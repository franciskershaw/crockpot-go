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
	resendCooldown          = 60 * time.Second
	passwordResetTokenTTL   = time.Hour
	minPasswordLength       = 8
	maxPasswordBytes        = 72
	refreshGraceWindow      = 10 * time.Second
)

type UserRepository interface {
	GetOrCreateUser(ctx context.Context, email, googleID, displayName, avatarURL string) (*models.User, error)
	CreateUnconfirmedUser(ctx context.Context, email, passwordHash, name string) (*models.User, error)
	MarkEmailConfirmed(ctx context.Context, userID string) (*models.User, error)
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByID(ctx context.Context, userID string) (*models.User, error)
	UpdateLastLogin(ctx context.Context, userID string) (*models.User, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) (*models.User, error)
}

type RefreshTokenRepository interface {
	CreateFamily(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) (*models.RefreshTokenFamily, error)
	DeleteStaleFamiliesForUser(ctx context.Context, userID string) error
	RevokeAllFamiliesForUser(ctx context.Context, userID string) error
	FindFamilyByID(ctx context.Context, id, userID string) (*models.RefreshTokenFamily, error)
	RotateFamily(ctx context.Context, familyID, presentedHash, newTokenHash string, newExpiresAt, graceWindowCutoff time.Time) (bool, error)
	RevokeFamily(ctx context.Context, familyID string) error
}

type EmailVerificationTokenRepository interface {
	Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.EmailVerificationToken, error)
	FindActiveByUserID(ctx context.Context, userID string) (*models.EmailVerificationToken, error)
	IncrementAttempts(ctx context.Context, id string) (*models.EmailVerificationToken, error)
	MarkUsed(ctx context.Context, id string) error
	DeleteActiveForUser(ctx context.Context, userID string) error
}

type PasswordResetTokenRepository interface {
	Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.PasswordResetToken, error)
	FindActiveByUserID(ctx context.Context, userID string) (*models.PasswordResetToken, error)
	FindActiveByTokenHash(ctx context.Context, tokenHash string) (*models.PasswordResetToken, error)
	MarkUsed(ctx context.Context, id string) (bool, error)
	DeleteActiveForUser(ctx context.Context, userID string) error
	AcquireUserLock(ctx context.Context, userID string) error
}

// Transactor runs fn inside a single database transaction — every repository call made
// with the ctx passed to fn joins that transaction, regardless of which repo interface it's called through.
type Transactor interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
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
	SendPasswordResetLink(ctx context.Context, toEmail, resetURL string) error
}

type AuthHandler struct {
	userRepo                   UserRepository
	oauthManager               OAuthManager
	refreshTokenRepo           RefreshTokenRepository
	emailVerificationTokenRepo EmailVerificationTokenRepository
	passwordResetTokenRepo     PasswordResetTokenRepository
	emailSender                EmailSender
	transactor                 Transactor
	cfg                        *config.Config
}

func NewAuthHandler(userRepo UserRepository, oauthManager OAuthManager, refreshTokenRepo RefreshTokenRepository, emailVerificationTokenRepo EmailVerificationTokenRepository, passwordResetTokenRepo PasswordResetTokenRepository, emailSender EmailSender, transactor Transactor, cfg *config.Config) *AuthHandler {
	return &AuthHandler{
		userRepo:                   userRepo,
		oauthManager:               oauthManager,
		refreshTokenRepo:           refreshTokenRepo,
		emailVerificationTokenRepo: emailVerificationTokenRepo,
		passwordResetTokenRepo:     passwordResetTokenRepo,
		emailSender:                emailSender,
		transactor:                 transactor,
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

// issueRefreshSession clears any stale families for the user, creates a fresh one, and sets the refresh cookie — shared by every path that starts a new session (GoogleCallback, Login).
func (h *AuthHandler) issueRefreshSession(ctx context.Context, c *gin.Context, userID string) error {
	familyID := uuid.NewString()
	refreshToken, err := auth.GenerateRefreshToken(userID, familyID, h.cfg.JWTSecretRefresh)
	if err != nil {
		return fmt.Errorf("failed to generate refresh token: %w", err)
	}

	if err := h.refreshTokenRepo.DeleteStaleFamiliesForUser(ctx, userID); err != nil {
		return fmt.Errorf("failed to clean up refresh tokens: %w", err)
	}
	if _, err := h.refreshTokenRepo.CreateFamily(ctx, familyID, userID, auth.HashToken(refreshToken), time.Now().Add(refreshTokenTTL)); err != nil {
		return fmt.Errorf("failed to persist refresh token: %w", err)
	}

	h.setRefreshCookie(c, refreshToken, int(refreshTokenTTL.Seconds()))
	return nil
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

	if err := h.issueRefreshSession(ctx, c, user.ID.String()); err != nil {
		_ = c.Error(err)
		h.redirectWithError(c, "server_error")
		return
	}

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
			// Never overwrite the password here — the legitimate owner, not whoever last called
			// register, is the one who'll complete confirmation with their already-delivered code.
			user, err = h.userRepo.FindByEmail(ctx, req.Email)
			if err != nil {
				_ = c.Error(fmt.Errorf("failed to look up unconfirmed user: %w", err))
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
		var cooldown *errResendCooldown
		if errors.As(err, &cooldown) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":             "resend_too_soon",
				"retryAfterSeconds": int(cooldown.retryAfter.Seconds()),
			})
			return
		}
		_ = c.Error(fmt.Errorf("failed to issue confirmation code: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "check your email for a confirmation code"})
}

// errResendCooldown signals the 60s per-email cooldown is still active — every caller of issueConfirmationCode gets this protection, not just ResendConfirmation.
type errResendCooldown struct {
	retryAfter time.Duration
}

func (e *errResendCooldown) Error() string {
	return fmt.Sprintf("resend cooldown active, retry after %s", e.retryAfter)
}

// issueConfirmationCode clears any prior unconsumed code before issuing and emailing a fresh one — shared by fresh registration, the abandoned-signup retry path, and ResendConfirmation.
func (h *AuthHandler) issueConfirmationCode(ctx context.Context, user *models.User) error {
	if existing, err := h.emailVerificationTokenRepo.FindActiveByUserID(ctx, user.ID.String()); err == nil {
		if elapsed := time.Since(existing.CreatedAt); elapsed < resendCooldown {
			return &errResendCooldown{retryAfter: resendCooldown - elapsed}
		}
	} else if !errors.Is(err, models.ErrNoActiveEmailVerificationToken) {
		return fmt.Errorf("failed to look up active confirmation code: %w", err)
	}

	if err := h.emailVerificationTokenRepo.DeleteActiveForUser(ctx, user.ID.String()); err != nil {
		return fmt.Errorf("failed to clear prior confirmation code: %w", err)
	}

	code, err := auth.GenerateConfirmationCode()
	if err != nil {
		return fmt.Errorf("failed to generate confirmation code: %w", err)
	}

	token, err := h.emailVerificationTokenRepo.Create(ctx, user.ID.String(), auth.HashToken(code), time.Now().Add(confirmationCodeTTL))
	if err != nil {
		return fmt.Errorf("failed to persist confirmation code: %w", err)
	}

	if err := h.emailSender.SendConfirmationCode(ctx, user.Email, code); err != nil {
		// Never delivered — mark it used so it doesn't block the next attempt behind a cooldown for a code the user never received.
		if markErr := h.emailVerificationTokenRepo.MarkUsed(ctx, token.ID.String()); markErr != nil {
			return fmt.Errorf("failed to send confirmation email (%w) and failed to clean up undelivered code: %w", err, markErr)
		}
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
		if !errors.Is(err, models.ErrUserNotFound) {
			_ = c.Error(fmt.Errorf("failed to look up user by email: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
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
	// Confirmation itself already succeeded — a failure here just leaves an inert token that
	// self-expires within confirmationCodeTTL, not worth failing a successful confirmation over.
	if err := h.emailVerificationTokenRepo.MarkUsed(ctx, token.ID.String()); err != nil {
		_ = c.Error(fmt.Errorf("email confirmed but failed to mark code used: %w", err))
	}

	c.JSON(http.StatusOK, gin.H{"message": "email confirmed"})
}

type resendRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func (h *AuthHandler) ResendConfirmation(c *gin.Context) {
	var req resendRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		if !errors.Is(err, models.ErrUserNotFound) {
			_ = c.Error(fmt.Errorf("failed to look up user by email: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "email_not_found"})
		return
	}

	if user.EmailVerifiedAt != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "already_confirmed"})
		return
	}

	if err := h.issueConfirmationCode(ctx, user); err != nil {
		var cooldown *errResendCooldown
		if errors.As(err, &cooldown) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":             "resend_too_soon",
				"retryAfterSeconds": int(cooldown.retryAfter.Seconds()),
			})
			return
		}
		_ = c.Error(fmt.Errorf("failed to issue confirmation code: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "confirmation code resent"})
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		if !errors.Is(err, models.ErrUserNotFound) {
			_ = c.Error(fmt.Errorf("failed to look up user by email: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}

	if user.PasswordHash == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "google_account_no_password"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}

	if user.EmailVerifiedAt == nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "email_not_confirmed"})
		return
	}

	accessToken, err := auth.GenerateAccessToken(user.Email, user.ID.String(), user.Role, h.cfg.JWTSecretAccess)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to generate access token: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	if _, err := h.userRepo.UpdateLastLogin(ctx, user.ID.String()); err != nil {
		_ = c.Error(fmt.Errorf("failed to update last login: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Session issuance stays last — it's the one step that sets a live cookie.
	if err := h.issueRefreshSession(ctx, c, user.ID.String()); err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"accessToken": accessToken})
}

type forgotPasswordRequest struct {
	Email string `json:"email" binding:"required,email"`
}

func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req forgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	ctx := c.Request.Context()

	user, err := h.userRepo.FindByEmail(ctx, req.Email)
	if err != nil {
		if !errors.Is(err, models.ErrUserNotFound) {
			_ = c.Error(fmt.Errorf("failed to look up user by email: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "email_not_found"})
		return
	}

	if user.PasswordHash == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "google_account_no_password"})
		return
	}

	if err := h.issuePasswordResetToken(ctx, user); err != nil {
		var cooldown *errResendCooldown
		if errors.As(err, &cooldown) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":             "resend_too_soon",
				"retryAfterSeconds": int(cooldown.retryAfter.Seconds()),
			})
			return
		}
		_ = c.Error(fmt.Errorf("failed to issue password reset token: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "check your email for a password reset link"})
}

// The DB sequence runs behind a per-user advisory lock so concurrent calls don't race the active-token
// unique index; the email send stays outside the transaction, never holding a DB lock across a network call.
func (h *AuthHandler) issuePasswordResetToken(ctx context.Context, user *models.User) error {
	var token string
	var created *models.PasswordResetToken

	err := h.transactor.WithinTx(ctx, func(ctx context.Context) error {
		if err := h.passwordResetTokenRepo.AcquireUserLock(ctx, user.ID.String()); err != nil {
			return err
		}

		if existing, err := h.passwordResetTokenRepo.FindActiveByUserID(ctx, user.ID.String()); err == nil {
			if elapsed := time.Since(existing.CreatedAt); elapsed < resendCooldown {
				return &errResendCooldown{retryAfter: resendCooldown - elapsed}
			}
		} else if !errors.Is(err, models.ErrNoActivePasswordResetToken) {
			return fmt.Errorf("failed to look up active password reset token: %w", err)
		}

		if err := h.passwordResetTokenRepo.DeleteActiveForUser(ctx, user.ID.String()); err != nil {
			return fmt.Errorf("failed to clear prior password reset token: %w", err)
		}

		var err error
		token, err = auth.GenerateResetToken()
		if err != nil {
			return fmt.Errorf("failed to generate password reset token: %w", err)
		}

		created, err = h.passwordResetTokenRepo.Create(ctx, user.ID.String(), auth.HashToken(token), time.Now().Add(passwordResetTokenTTL))
		if err != nil {
			return fmt.Errorf("failed to persist password reset token: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	resetURL := fmt.Sprintf("%s/reset-password?token=%s", h.cfg.FrontendURL, token)
	if err := h.emailSender.SendPasswordResetLink(ctx, user.Email, resetURL); err != nil {
		// Never delivered — mark it used so it doesn't block the next attempt behind a cooldown for a link the user never received.
		if _, markErr := h.passwordResetTokenRepo.MarkUsed(ctx, created.ID.String()); markErr != nil {
			return fmt.Errorf("failed to send password reset email (%w) and failed to clean up undelivered token: %w", err, markErr)
		}
		return fmt.Errorf("failed to send password reset email: %w", err)
	}
	return nil
}

type resetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"newPassword" binding:"required"`
}

func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	ctx := c.Request.Context()

	token, err := h.passwordResetTokenRepo.FindActiveByTokenHash(ctx, auth.HashToken(req.Token))
	if err != nil {
		if !errors.Is(err, models.ErrNoActivePasswordResetToken) {
			_ = c.Error(fmt.Errorf("failed to look up password reset token: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "token_invalid"})
		return
	}

	if time.Now().After(token.ExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "token_expired"})
		return
	}

	if len(req.NewPassword) < minPasswordLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password_too_short"})
		return
	}
	if len(req.NewPassword) > maxPasswordBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password_too_long"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to hash password: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	userID := token.UserID.String()
	familyID := uuid.NewString()
	refreshToken, err := auth.GenerateRefreshToken(userID, familyID, h.cfg.JWTSecretRefresh)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to generate refresh token: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	var user *models.User
	txErr := h.transactor.WithinTx(ctx, func(ctx context.Context) error {
		claimed, err := h.passwordResetTokenRepo.MarkUsed(ctx, token.ID.String())
		if err != nil {
			return fmt.Errorf("failed to mark password reset token used: %w", err)
		}
		if !claimed {
			return errPasswordResetTokenAlreadyClaimed
		}

		user, err = h.userRepo.UpdatePassword(ctx, userID, string(hash))
		if err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}

		if _, err := h.userRepo.MarkEmailConfirmed(ctx, userID); err != nil {
			return fmt.Errorf("failed to mark email confirmed: %w", err)
		}

		if err := h.refreshTokenRepo.RevokeAllFamiliesForUser(ctx, userID); err != nil {
			return fmt.Errorf("failed to revoke existing sessions: %w", err)
		}

		if err := h.refreshTokenRepo.DeleteStaleFamiliesForUser(ctx, userID); err != nil {
			return fmt.Errorf("failed to clean up refresh tokens: %w", err)
		}
		if _, err := h.refreshTokenRepo.CreateFamily(ctx, familyID, userID, auth.HashToken(refreshToken), time.Now().Add(refreshTokenTTL)); err != nil {
			return fmt.Errorf("failed to persist refresh token: %w", err)
		}
		return nil
	})
	if txErr != nil {
		if errors.Is(txErr, errPasswordResetTokenAlreadyClaimed) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "token_invalid"})
			return
		}
		_ = c.Error(txErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	accessToken, err := auth.GenerateAccessToken(user.Email, user.ID.String(), user.Role, h.cfg.JWTSecretAccess)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to generate access token: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// Cookie is set only after the transaction above commits — never on a response that says failure.
	h.setRefreshCookie(c, refreshToken, int(refreshTokenTTL.Seconds()))

	c.JSON(http.StatusOK, gin.H{"accessToken": accessToken})
}

// errPasswordResetTokenAlreadyClaimed signals MarkUsed's atomic claim lost a race to a concurrent ResetPassword call.
var errPasswordResetTokenAlreadyClaimed = errors.New("password reset token already claimed")

func (h *AuthHandler) invalidRefreshToken(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_refresh_token"})
}

func (h *AuthHandler) RefreshToken(c *gin.Context) {
	ctx := c.Request.Context()

	cookie, err := c.Cookie("refreshToken")
	if err != nil {
		h.invalidRefreshToken(c)
		return
	}

	claims, err := auth.ValidateRefreshToken(cookie, h.cfg.JWTSecretRefresh)
	if err != nil {
		h.invalidRefreshToken(c)
		return
	}

	family, err := h.refreshTokenRepo.FindFamilyByID(ctx, claims.FamilyID, claims.Subject)
	if err != nil {
		if !errors.Is(err, models.ErrRefreshTokenFamilyNotFound) {
			_ = c.Error(fmt.Errorf("failed to look up refresh token family: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		h.invalidRefreshToken(c)
		return
	}

	if family.RevokedAt != nil {
		h.invalidRefreshToken(c)
		return
	}

	// User lookup happens before rotation — rotation is what grants the continued session (new cookie),
	// so a failure here must never leave a rotated-but-unusable family behind.
	user, err := h.userRepo.FindByID(ctx, claims.Subject)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to look up user: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	accessToken, err := auth.GenerateAccessToken(user.Email, user.ID.String(), user.Role, h.cfg.JWTSecretAccess)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to generate access token: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	newRefreshToken, err := auth.GenerateRefreshToken(claims.Subject, claims.FamilyID, h.cfg.JWTSecretRefresh)
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to generate refresh token: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	// RotateFamily re-validates the presented hash atomically against the live row at write time,
	// not the possibly-stale read above. rotated=false means it no longer qualifies: revoke, don't retry.
	presentedHash := auth.HashToken(cookie)
	rotated, err := h.refreshTokenRepo.RotateFamily(ctx, claims.FamilyID, presentedHash, auth.HashToken(newRefreshToken), time.Now().Add(refreshTokenTTL), time.Now().Add(-refreshGraceWindow))
	if err != nil {
		_ = c.Error(fmt.Errorf("failed to rotate refresh token family: %w", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !rotated {
		if err := h.refreshTokenRepo.RevokeFamily(ctx, claims.FamilyID); err != nil {
			_ = c.Error(fmt.Errorf("failed to revoke reused refresh token family: %w", err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		h.invalidRefreshToken(c)
		return
	}

	h.setRefreshCookie(c, newRefreshToken, int(refreshTokenTTL.Seconds()))
	c.JSON(http.StatusOK, gin.H{"accessToken": accessToken})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	if cookie, err := c.Cookie("refreshToken"); err == nil {
		if claims, err := auth.ValidateRefreshToken(cookie, h.cfg.JWTSecretRefresh); err == nil {
			if err := h.refreshTokenRepo.RevokeFamily(c.Request.Context(), claims.FamilyID); err != nil {
				// Cookie is still cleared below — but a presented, valid token that fails to revoke
				// must not report success, or the family stays live while the client thinks it's safe.
				_ = c.Error(fmt.Errorf("failed to revoke refresh token family: %w", err))
				h.setRefreshCookie(c, "", -1)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
				return
			}
		}
	}

	// Cookie is always cleared, even if the token above was missing or invalid.
	h.setRefreshCookie(c, "", -1)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (h *AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "me_not_implemented_stub"})
}
