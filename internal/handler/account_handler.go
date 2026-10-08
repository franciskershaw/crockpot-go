package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/franciskershaw/crockpot-go/config"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type AccountUserRepository interface {
	FindByID(ctx context.Context, userID string) (*models.User, error)
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
