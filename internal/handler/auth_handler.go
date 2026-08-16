package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

const (
	refreshTokenTTL        = 7 * 24 * time.Hour
	oauthStateCookieMaxAge = 10 * time.Minute
)

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

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

	ctx := context.Background()
	oauthToken, err := h.oauthManager.ExchangeCodeForToken(ctx, code)
	if err != nil {
		h.redirectWithError(c, "exchange_failed")
		return
	}

	idTokenClaims, err := h.oauthManager.VerifyIDToken(ctx, oauthToken)
	if err != nil {
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
	if _, err := h.refreshTokenRepo.CreateFamily(ctx, familyID, user.ID.String(), hashRefreshToken(refreshToken), time.Now().Add(refreshTokenTTL)); err != nil {
		_ = c.Error(fmt.Errorf("failed to persist refresh token: %w", err))
		h.redirectWithError(c, "server_error")
		return
	}

	h.setRefreshCookie(c, refreshToken, int(refreshTokenTTL.Seconds()))

	// No access token or user data in the redirect — the frontend mints its own via the refresh cookie just set.
	c.Redirect(http.StatusTemporaryRedirect, fmt.Sprintf("%s/auth/callback", h.cfg.FrontendURL))
}
