package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/email"
	"github.com/franciskershaw/crockpot-go/internal/handler"
	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/franciskershaw/crockpot-go/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"

	_ "github.com/joho/godotenv/autoload"
)

// Must exceed newHTTPServer's WriteTimeout (15s) so in-flight requests within their own allowed timeout aren't cut off by shutdown first.
const shutdownGracePeriod = 20 * time.Second

var globalRateLimit = limiter.Rate{Period: time.Minute, Limit: 120}
var authRateLimit = limiter.Rate{Period: time.Minute, Limit: 10}
var authRefreshRateLimit = limiter.Rate{Period: time.Minute, Limit: 30}

func main() {
	// Match Gin's own default writer (os.Stdout) so log output interleaves in order.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Config load failed: %v\n", err)
		os.Exit(1)
	}

	// Initialise the DB
	err = db.InitDB(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Database init failed: %v\n", err)
		os.Exit(1)
	}
	defer db.CloseDB()

	// Initialise Google OAuth manager once at startup (makes a network call)
	oauthManager, err := auth.NewGoogleOAuthManager(
		cfg.GoogleClientID,
		cfg.GoogleClientSecret,
		cfg.GoogleRedirectURL,
		cfg.JWTSecretOAuthState,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Google OAuth init failed: %v\n", err)
		os.Exit(1)
	}

	userRepo := repository.NewPostgresUserRepository(db.DB)
	refreshTokenRepo := repository.NewPostgresRefreshTokenRepository(db.DB)
	emailVerificationTokenRepo := repository.NewPostgresEmailVerificationTokenRepository(db.DB)
	passwordResetTokenRepo := repository.NewPostgresPasswordResetTokenRepository(db.DB)
	transactor := repository.NewPostgresTransactor(db.DB)
	itemCategoryRepo := repository.NewPostgresItemCategoryRepository(db.DB)
	unitRepo := repository.NewPostgresUnitRepository(db.DB)
	itemRepo := repository.NewPostgresItemRepository(db.DB)
	emailSender := email.NewResendClient(cfg.ResendAPIKey, cfg.EmailFrom)
	authHandler := handler.NewAuthHandler(userRepo, oauthManager, refreshTokenRepo, emailVerificationTokenRepo, passwordResetTokenRepo, emailSender, transactor, cfg)
	itemCategoryHandler := handler.NewItemCategoryHandler(itemCategoryRepo)
	unitHandler := handler.NewUnitHandler(unitRepo)
	itemHandler := handler.NewItemHandler(itemRepo, transactor)

	// Initialize Gin server
	gin.SetMode(configureGinMode(string(cfg.Environment)))
	server := gin.Default()
	if err := server.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		fmt.Fprintf(os.Stderr, "SetTrustedProxies failed: %v\n", err)
		os.Exit(1)
	}
	// CORS before the rate limiter so preflight OPTIONS aren't charged against the global bucket.
	server.Use(middleware.CORS(cfg.FrontendURL))
	server.Use(middleware.NewRateLimitMiddleware(memory.NewStore(), globalRateLimit).Handler())

	server.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "Welcome to the Crockpot API",
		})
	})

	server.GET("/auth/google/login", authHandler.LoginWithGoogle)
	server.GET("/auth/google/callback", authHandler.GoogleCallback)

	authTight := server.Group("/auth")
	authTight.Use(middleware.NewRateLimitMiddleware(memory.NewStore(), authRateLimit).Handler())
	{
		authTight.POST("/register", authHandler.Register)
		authTight.POST("/confirm", authHandler.ConfirmEmail)
		authTight.POST("/resend-confirmation", authHandler.ResendConfirmation)
		authTight.POST("/login", authHandler.Login)
		authTight.POST("/forgot-password", authHandler.ForgotPassword)
		authTight.POST("/reset-password", authHandler.ResetPassword)
		authTight.POST("/logout", authHandler.Logout)
	}

	authRefresh := server.Group("/auth")
	authRefresh.Use(middleware.NewRateLimitMiddleware(memory.NewStore(), authRefreshRateLimit).Handler())
	{
		authRefresh.POST("/refresh", authHandler.RefreshToken)
	}

	authed := server.Group("/")
	authed.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess))
	{
		authed.GET("/me", authHandler.Me)
	}

	// Public read: reference data, visible to anonymous browse/filter.
	server.GET("/item-categories", itemCategoryHandler.List)

	itemCategoriesAdmin := server.Group("/item-categories")
	itemCategoriesAdmin.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess), middleware.RequireRole("ADMIN"))
	{
		itemCategoriesAdmin.POST("", itemCategoryHandler.Create)
		itemCategoriesAdmin.PATCH("/:id", itemCategoryHandler.Update)
		itemCategoriesAdmin.DELETE("/:id", itemCategoryHandler.Delete)
	}

	server.GET("/units", unitHandler.List)

	unitsAdmin := server.Group("/units")
	unitsAdmin.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess), middleware.RequireRole("ADMIN"))
	{
		unitsAdmin.POST("", unitHandler.Create)
		unitsAdmin.PATCH("/:id", unitHandler.Update)
		unitsAdmin.DELETE("/:id", unitHandler.Delete)
	}

	server.GET("/items", itemHandler.List)

	itemsAdmin := server.Group("/items")
	itemsAdmin.Use(middleware.AuthMiddleware(cfg.JWTSecretAccess), middleware.RequireRole("ADMIN"))
	{
		itemsAdmin.POST("", itemHandler.Create)
		itemsAdmin.PATCH("/:id", itemHandler.Update)
		itemsAdmin.DELETE("/:id", itemHandler.Delete)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpServer := newHTTPServer(":"+cfg.Port, server)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGracePeriod)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "graceful shutdown failed: %v\n", err)
	}
}
