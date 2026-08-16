package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

type UserRepository interface {
	GetOrCreateUser(ctx context.Context, email, googleID, displayName, avatarURL string) (*models.User, error)
}

type RefreshTokenRepository interface {
	CreateFamily(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) (*models.RefreshTokenFamily, error)
	DeleteStaleFamiliesForUser(ctx context.Context, userID string) error
}

type OAuthManager interface {
	GenerateState() (string, error)
	ValidateState(state string) bool
	GetAuthURL(state string) string
	ExchangeCodeForToken(ctx context.Context, code string) (*oauth2.Token, error)
	VerifyIDToken(ctx context.Context, token *oauth2.Token) (*auth.IDTokenClaims, error)
}

type AuthHandler struct {
	userRepo         UserRepository
	oauthManager     OAuthManager
	refreshTokenRepo RefreshTokenRepository
	cfg              *config.Config
}

func NewAuthHandler(userRepo UserRepository, oauthManager OAuthManager, refreshTokenRepo RefreshTokenRepository, cfg *config.Config) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, oauthManager: oauthManager, refreshTokenRepo: refreshTokenRepo, cfg: cfg}
}

func (h *AuthHandler) LoginWithGoogle(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

func (h *AuthHandler) GoogleCallback(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}
