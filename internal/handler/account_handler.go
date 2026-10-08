package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type AccountUserRepository interface {
	FindByIDForUpdate(ctx context.Context, userID string) (*models.User, error)
	Delete(ctx context.Context, userID string) error
}

type AccountRecipeRepository interface {
	DeleteUnapprovedByCreator(ctx context.Context, creatorID string) (menuUserIDs []string, orphanedImages []string, err error)
}

type AccountHandler struct {
	users         AccountUserRepository
	recipes       AccountRecipeRepository
	shoppingLists ShoppingListRegenerator
	transactor    Transactor
	imageStore    ImageStore
	imageScope    ImageScope
	cfg           *config.Config
}

func NewAccountHandler(users AccountUserRepository, recipes AccountRecipeRepository, shoppingLists ShoppingListRegenerator, transactor Transactor, imageStore ImageStore, imageScope ImageScope, cfg *config.Config) *AccountHandler {
	return &AccountHandler{users: users, recipes: recipes, shoppingLists: shoppingLists, transactor: transactor, imageStore: imageStore, imageScope: imageScope, cfg: cfg}
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

	// Google accounts send no body.
	var req deleteMeRequest
	if c.Request.ContentLength != 0 && !bindJSON(c, &req) {
		return
	}

	// Locking the user first makes any insert that references them wait, then fail its foreign key once this commits.
	var orphans []string
	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		user, err := h.users.FindByIDForUpdate(ctx, userID)
		if err != nil {
			return fmt.Errorf("failed to look up user: %w", err)
		}
		if user.Role == "ADMIN" {
			return errAdminCannotSelfDelete
		}
		if user.PasswordHash != nil {
			if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
				return errInvalidPassword
			}
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
