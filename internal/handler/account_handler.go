package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AccountUserRepository interface {
	FindByID(ctx context.Context, userID string) (*models.User, error)
	FindByIDForUpdate(ctx context.Context, userID string) (*models.User, error)
	UpdateName(ctx context.Context, userID, name string) (*models.User, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string) (*models.User, error)
	Delete(ctx context.Context, userID string) error
}

type AccountRecipeRepository interface {
	DeleteUnapprovedByCreator(ctx context.Context, creatorID string) (menuUserIDs []string, orphanedImages []string, err error)
}

// AccountHandler serves /me: the signed-in user's profile, password and account.
type AccountHandler struct {
	users         AccountUserRepository
	refreshTokens RefreshTokenRepository
	emailSender   EmailSender
	recipes       AccountRecipeRepository
	shoppingLists ShoppingListRegenerator
	transactor    Transactor
	imageStore    ImageStore
	imageScope    ImageScope
	cfg           *config.Config
}

func NewAccountHandler(users AccountUserRepository, refreshTokens RefreshTokenRepository, emailSender EmailSender, recipes AccountRecipeRepository, shoppingLists ShoppingListRegenerator, transactor Transactor, imageStore ImageStore, imageScope ImageScope, cfg *config.Config) *AccountHandler {
	return &AccountHandler{
		users: users, refreshTokens: refreshTokens, emailSender: emailSender, recipes: recipes,
		shoppingLists: shoppingLists, transactor: transactor, imageStore: imageStore, imageScope: imageScope, cfg: cfg,
	}
}

func (h *AccountHandler) Me(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	user, err := h.users.FindByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			unauthorized(c, "unauthorized")
			return
		}
		internalError(c, "failed to look up user", err)
		return
	}

	c.JSON(http.StatusOK, meResponse(user))
}

type updateMeRequest struct {
	Name string `json:"name"`
}

func (h *AccountHandler) UpdateMe(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	var req updateMeRequest
	if !bindJSON(c, &req) {
		return
	}
	name, ok := validateUserName(c, req.Name)
	if !ok {
		return
	}

	user, err := h.users.UpdateName(c.Request.Context(), userID, name)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			unauthorized(c, "unauthorized")
			return
		}
		internalError(c, "failed to update name", err)
		return
	}

	c.JSON(http.StatusOK, meResponse(user))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" binding:"required"`
	NewPassword     string `json:"newPassword" binding:"required"`
}

func (h *AccountHandler) ChangePassword(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	var req changePasswordRequest
	if !bindJSON(c, &req) {
		return
	}
	if !validateNewPassword(c, req.NewPassword) {
		return
	}

	ctx := c.Request.Context()
	user, err := h.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			unauthorized(c, "unauthorized")
			return
		}
		internalError(c, "failed to look up user", err)
		return
	}
	if user.PasswordHash == nil {
		conflict(c, "no_password")
		return
	}
	// bcrypt runs before the transaction so a wrong guess never holds the user row lock.
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		// 403, not 401: the frontend treats a 401 on a signed-in request as an expired session.
		forbidden(c, "invalid_password")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		internalError(c, "failed to hash password", err)
		return
	}

	familyID := uuid.NewString()
	refreshToken, err := auth.GenerateRefreshToken(userID, familyID, h.cfg.JWTSecretRefresh)
	if err != nil {
		internalError(c, "failed to generate refresh token", err)
		return
	}

	txErr := h.transactor.WithinTx(ctx, func(ctx context.Context) error {
		locked, err := h.users.FindByIDForUpdate(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to look up user: %w", err)
		}
		// A reset or change that landed after the check above makes the checked password stale.
		if !samePasswordHash(locked.PasswordHash, user.PasswordHash) {
			return errInvalidPassword
		}
		if _, err := h.users.UpdatePassword(ctx, userID, string(hash)); err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}
		return replaceAllSessions(ctx, h.refreshTokens, userID, familyID, refreshToken)
	})
	switch {
	case txErr == nil:
	case errors.Is(txErr, models.ErrUserNotFound):
		unauthorized(c, "unauthorized")
		return
	case errors.Is(txErr, errInvalidPassword):
		forbidden(c, "invalid_password")
		return
	default:
		_ = c.Error(txErr)
		serverError(c)
		return
	}

	accessToken, err := auth.GenerateAccessToken(user.Email, user.ID.String(), user.Role, h.cfg.JWTSecretAccess)
	if err != nil {
		internalError(c, "failed to generate access token", err)
		return
	}

	setRefreshCookie(c, h.cfg, refreshToken, int(refreshTokenTTL.Seconds()))

	forgotURL := fmt.Sprintf("%s/forgot-password", h.cfg.FrontendURL)
	if err := h.emailSender.SendPasswordChanged(context.WithoutCancel(ctx), user.Email, forgotURL); err != nil {
		_ = c.Error(fmt.Errorf("failed to send password changed email: %w", err))
	}

	c.JSON(http.StatusOK, gin.H{"accessToken": accessToken})
}

var errInvalidPassword = errors.New("current password does not match")

func samePasswordHash(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func meResponse(user *models.User) gin.H {
	return gin.H{
		"id":           user.ID.String(),
		"email":        user.Email,
		"name":         user.Name,
		"role":         user.Role,
		"authProvider": authProvider(user),
	}
}

// authProvider relies on the users CHECK constraint: exactly one of google_id and password_hash is set.
func authProvider(user *models.User) string {
	if user.GoogleID != nil {
		return "google"
	}
	return "password"
}

type deleteMeRequest struct {
	Password string `json:"password"`
}

var errAdminCannotSelfDelete = errors.New("admin cannot delete their own account")

// DeleteMe hard-deletes the caller: their drafts go, their approved recipes stay with no creator, everything else cascades.
func (h *AccountHandler) DeleteMe(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	// Google accounts send no body, which can arrive with an unknown length rather than zero.
	var req deleteMeRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		badRequest(c, "invalid_request")
		return
	}

	ctx := c.Request.Context()
	user, err := h.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, models.ErrUserNotFound) {
			unauthorized(c, "unauthorized")
			return
		}
		internalError(c, "failed to look up user", err)
		return
	}
	if user.Role == "ADMIN" {
		forbidden(c, "admin_cannot_self_delete")
		return
	}
	// bcrypt runs before the transaction so a wrong guess never holds the user row lock.
	if user.PasswordHash != nil {
		if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
			forbidden(c, "invalid_password")
			return
		}
	}

	// Locking the user makes any insert that references them wait, then fail its foreign key once this commits.
	var orphans []string
	txErr := h.transactor.WithinTx(ctx, func(ctx context.Context) error {
		locked, err := h.users.FindByIDForUpdate(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to look up user: %w", err)
		}
		if locked.Role == "ADMIN" {
			return errAdminCannotSelfDelete
		}
		if !samePasswordHash(locked.PasswordHash, user.PasswordHash) {
			return errInvalidPassword
		}

		var holders []string
		holders, orphans, err = h.recipes.DeleteUnapprovedByCreator(ctx, userID)
		if err != nil {
			return err
		}
		for _, holder := range holders {
			if holder == userID {
				continue
			}
			if err := h.shoppingLists.Regenerate(ctx, holder); err != nil {
				return err
			}
		}
		return h.users.Delete(ctx, userID)
	})
	switch {
	case txErr == nil:
	case errors.Is(txErr, models.ErrUserNotFound):
		unauthorized(c, "unauthorized")
		return
	case errors.Is(txErr, errAdminCannotSelfDelete):
		forbidden(c, "admin_cannot_self_delete")
		return
	case errors.Is(txErr, errInvalidPassword):
		forbidden(c, "invalid_password")
		return
	default:
		_ = c.Error(txErr)
		serverError(c)
		return
	}

	for _, publicID := range orphans {
		destroyImageInScope(c, h.imageStore, h.imageScope, publicID)
	}
	setRefreshCookie(c, h.cfg, "", -1)
	c.Status(http.StatusNoContent)
}
