package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

const (
	refreshTokenTTL         = 7 * 24 * time.Hour
	oauthStateCookieMaxAge  = 10 * time.Minute
	oauthExchangeTimeout    = 10 * time.Second
	confirmationCodeTTL     = 10 * time.Minute
	maxConfirmationAttempts = 5
	minPasswordLength       = 8
	maxPasswordBytes        = 72
)

type UserRepository interface {
	GetOrCreateUser(ctx context.Context, email, googleID, displayName, avatarURL string) (*models.User, error)
	CreateUnconfirmedUser(ctx context.Context, email, passwordHash, name string) (*models.User, error)
	UpdatePasswordAndClearConfirmation(ctx context.Context, email, passwordHash, name string) (*models.User, error)
	MarkEmailConfirmed(ctx context.Context, userID string) (*models.User, error)
	FindByEmail(ctx context.Context, email string) (*models.User, error)
}

type RefreshTokenRepository interface {
	CreateFamily(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) (*models.RefreshTokenFamily, error)
	DeleteStaleFamiliesForUser(ctx context.Context, userID string) error
}

type EmailVerificationTokenRepository interface {
	Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.EmailVerificationToken, error)
	FindActiveByUserID(ctx context.Context, userID string) (*models.EmailVerificationToken, error)
	IncrementAttempts(ctx context.Context, id string) (*models.EmailVerificationToken, error)
	MarkUsed(ctx context.Context, id string) error
	DeleteActiveForUser(ctx context.Context, userID string) error
}

type OAuthManager interface {
	GenerateState() (string, error)
	ValidateState(state string) bool
	GetAuthURL(state string) string
	ExchangeCodeForToken(ctx context.Context, code string) (*oauth2.Token, error)
	VerifyIDToken(ctx context.Context, token *oauth2.Token) (*auth.IDTokenClaims, error)
}

type EmailSender interface {
	SendConfirmationCode(ctx context.Context, toEmail, code string) error
}

type AuthHandler struct {
	userRepo                   UserRepository
	oauthManager               OAuthManager
	refreshTokenRepo           RefreshTokenRepository
	emailVerificationTokenRepo EmailVerificationTokenRepository
	emailSender                EmailSender
	cfg                        *config.Config
}

func NewAuthHandler(userRepo UserRepository, oauthManager OAuthManager, refreshTokenRepo RefreshTokenRepository, emailVerificationTokenRepo EmailVerificationTokenRepository, emailSender EmailSender, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		userRepo:                   userRepo,
		oauthManager:               oauthManager,
		refreshTokenRepo:           refreshTokenRepo,
		emailVerificationTokenRepo: emailVerificationTokenRepo,
		emailSender:                emailSender,
		cfg:                        cfg,
	}
}

func (h *AuthHandler) setRefreshCookie(c *gin.Context, value string, maxAge int) {
	sameSite := http.SameSiteLaxMode
	if h.cfg.Environment == config.EnvProduction {
		sameSite = http.SameSiteNoneMode
	}
	c.SetSameSite(sameSite)
	c.SetCookie("refreshToken", value, maxAge, "/", "", h.cfg.Environment == config.EnvProduction, true)
}

func (h *AuthHandler) setOAuthStateCookie(c *gin.Context, value string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("oauthState", value, int(oauthStateCookieMaxAge.Seconds()), "/", "", h.cfg.Environment == config.EnvProduction, true)
}

func (h *AuthHandler) clearOAuthStateCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("oauthState", "", -1, "/", "", h.cfg.Environment == config.EnvProduction, true)
}

// redirectWithError sends the browser back to the frontend's callback route with an error code, never a JSON body.
func (h *AuthHandler) redirectWithError(c *gin.Context, code string) {
	c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s/auth/callback?error=%s", h.cfg.FrontendURL, code))
}

func (h *AuthHandler) LoginWithGoogle(c *gin.Context) {
	state, err := h.oauthManager.GenerateState()
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to generate oauth state: %w", err))
		h.redirectWithError(c, "server_error")
		return
	}

	h.setOAuthStateCookie(c, state)
	c.Redirect(http.StatusTemporaryRedirect, h.oauthManager.GetAuthURL(state))
}

// validOAuthStateCookie is the double-submit check: cookie must match the query param.
func (h *AuthHandler) validOAuthStateCookie(c *gin.Context, queryState string) bool {
	cookieState, err := c.Cookie("oauthState")
	if err != nil {
		return false
	}
	return cookieState == queryState
}

func (h *AuthHandler) GoogleCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if code == "" {
		h.clearOAuthStateCookie(c)
		h.redirectWithError(c, "missing_code")
		return
	}
	if state == "" {
		h.clearOAuthStateCookie(c)
		h.redirectWithError(c, "missing_state")
		return
	}

	if !h.validOAuthStateCookie(c, state) {
		h.clearOAuthStateCookie(c)
		h.redirectWithError(c, "invalid_state")
		return
	}
	h.clearOAuthStateCookie(c)

	if !h.oauthManager.ValidateState(state) {
		h.redirectWithError(c, "invalid_state")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), oauthExchangeTimeout)
	defer cancel()
	oauthToken, err := h.oauthManager.ExchangeCodeForToken(ctx, code)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to exchange oauth code: %w", err))
		h.redirectWithError(c, "exchange_failed")
		return
	}

	idTokenClaims, err := h.oauthManager.VerifyIDToken(ctx, oauthToken)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to verify id token: %w", err))
		h.redirectWithError(c, "verify_failed")
		return
	}
	if !idTokenClaims.EmailVerified {
		h.redirectWithError(c, "email_not_verified")
		return
	}

	user, err := h.userRepo.GetOrCreateUser(ctx, idTokenClaims.Email, idTokenClaims.GoogleID, idTokenClaims.DisplayName, idTokenClaims.AvatarURL)
	if err != nil {
		if errors.Is(err, models.ErrEmailRegisteredWithPassword) {
			h.redirectWithError(c, "email_registered_with_password")
			return
		}
		_ = c.Error(fmt.Errorf("failed to process user: %w", err))
		h.redirectWithError(c, "server_error")
		return
	}

	familyID := uuid.NewString()
	refreshToken, err := auth.GenerateRefreshToken(user.ID.String(), familyID, h.cfg.JWTSecretRefresh)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to generate refresh token: %w", err))
		h.redirectWithError(c, "server_error")
		return
	}

	if err := h.refreshTokenRepo.DeleteStaleFamiliesForUser(ctx, user.ID.String()); err != nil {
		_ = c.Error(fmt.Errorf("failed to clean up refresh tokens: %w", err))
		h.redirectWithError(c, "server_error")
		return
	}
	if _, err := h.refreshTokenRepo.CreateFamily(ctx, familyID, user.ID.String(), auth.HashToken(refreshToken), time.Now().Add(refreshTokenTTL)); err != nil {
		_ = c.Error(fmt.Errorf("failed to persist refresh token: %w", err))
		h.redirectWithError(c, "server_error")
		return
	}

	h.setRefreshCookie(c, refreshToken, int(refreshTokenTTL.Seconds()))

	// No access token or user data in the redirect — the frontend mints its own via the refresh cookie just set.
	c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s/auth/callback", h.cfg.FrontendURL))
}

type registerRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
	Name     string `json:"name" binding:"required"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	if len(req.Password) < minPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password_too_short"})
		return
	}
	if len(req.Password) > maxPasswordBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password_too_long"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to hash password: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.CreateUnconfirmedUser(ctx, req.Email, string(hash), req.Name)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrEmailRegisteredWithGoogle):
			c.JSON(http.StatusConflict, gin.H{"error": "email_registered_with_google"})
			return
		case errors.Is(err, models.ErrEmailRegisteredWithPassword):
			c.JSON(http.StatusConflict, gin.H{"error": "email_already_registered"})
			return
		case errors.Is(err, models.ErrEmailUnconfirmed):
			user, err = h.userRepo.UpdatePasswordAndClearConfirmation(ctx, req.Email, string(hash), req.Name)
			if err != nil {
				_ = c.Error(fmt.Errorf("failed to update unconfirmed user: %w", err))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
				return
			}
		default:
			_ = c.Error(fmt.Errorf("failed to create user: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
	}

	if err := h.issueConfirmationCode(ctx, user); err != nil {
		_ = c.Error(fmt.Errorf("failed to issue confirmation code: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "check your email for a confirmation code"})
}

// issueConfirmationCode clears any prior unconsumed code before issuing and emailing a fresh one — shared by fresh registration and the abandoned-signup retry path.
func (h *AuthHandler) issueConfirmationCode(ctx context.Context, user *models.User) error {
	if err := h.emailVerificationTokenRepo.DeleteActiveForUser(ctx, user.ID.String()); err != nil {
		return fmt.Errorf("failed to clear prior confirmation code: %w", err)
	}

	code, err := auth.GenerateConfirmationCode()
	if err != nil {
		return fmt.Errorf("failed to generate confirmation code: %w", err)
	}

	if _, err := h.emailVerificationTokenRepo.Create(ctx, user.ID.String(), auth.HashToken(code), time.Now().Add(confirmationCodeTTL)); err != nil {
		return fmt.Errorf("failed to persist confirmation code: %w", err)
	}

	if err := h.emailSender.SendConfirmationCode(ctx, user.Email, code); err != nil {
		return fmt.Errorf("failed to send confirmation email: %w", err)
	}
	return nil
}

type confirmRequest struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

func (h *AuthHandler) ConfirmEmail(c *gin.Context) {
	var req confirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		// Unknown email collapses into the same code_invalid as a wrong code — no second enumeration channel next to register's own.
		c.JSON(http.StatusBadRequest, gin.H{"error": "code_invalid"})
		return
	}

	token, err := h.emailVerificationTokenRepo.FindActiveByUserID(ctx, user.ID.String())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code_invalid"})
		return
	}

	if token.Attempts >= maxConfirmationAttempts {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too_many_attempts"})
		return
	}

	if time.Now().After(token.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "code_expired"})
		return
	}

	if auth.HashToken(req.Code) != token.TokenHash {
		if _, err := h.emailVerificationTokenRepo.IncrementAttempts(ctx, token.ID.String()); err != nil {
			_ = c.Error(fmt.Errorf("failed to record failed confirmation attempt: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "code_invalid"})
		return
	}

	if _, err := h.userRepo.MarkEmailConfirmed(ctx, user.ID.String()); err != nil {
		_ = c.Error(fmt.Errorf("failed to mark email confirmed: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if err := h.emailVerificationTokenRepo.MarkUsed(ctx, token.ID.String()); err != nil {
		_ = c.Error(fmt.Errorf("failed to mark confirmation code used: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "email confirmed"})
}
