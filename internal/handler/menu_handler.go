package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type MenuRepository interface {
	GetMenu(ctx context.Context, userID string) (*models.Menu, error)
	UpsertEntry(ctx context.Context, userID, recipeID string, serves int, callerIsAdmin bool) error
	UpdateEntryServes(ctx context.Context, userID, recipeID string, serves int) error
	RemoveEntry(ctx context.Context, userID, recipeID string) error
}

// ShoppingListRegenerator is the shopping-list side effect every menu write triggers,
// in the same transaction — MenuHandler only needs Regenerate.
type ShoppingListRegenerator interface {
	Regenerate(ctx context.Context, userID string) error
}

type MenuHandler struct {
	repo          MenuRepository
	shoppingLists ShoppingListRegenerator
	transactor    Transactor
}

func NewMenuHandler(repo MenuRepository, shoppingLists ShoppingListRegenerator, transactor Transactor) *MenuHandler {
	return &MenuHandler{repo: repo, shoppingLists: shoppingLists, transactor: transactor}
}

func (h *MenuHandler) Get(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	menu, err := h.repo.GetMenu(c.Request.Context(), userID)
	if err != nil {
		internalError(c, "failed to get menu", err)
		return
	}
	c.JSON(http.StatusOK, menu)
}

func (h *MenuHandler) UpsertEntry(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	recipeID, serves, ok := parseUpsertMenuEntryRequest(c)
	if !ok {
		return
	}
	isAdmin := c.GetString("role") == "ADMIN"

	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		if err := h.repo.UpsertEntry(ctx, userID, recipeID, serves, isAdmin); err != nil {
			return err
		}
		return h.shoppingLists.Regenerate(ctx, userID)
	})
	if txErr != nil {
		if errors.Is(txErr, models.ErrRecipeNotFound) {
			notFound(c, "not_found")
			return
		}
		internalError(c, "failed to upsert menu entry", txErr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "recipe added to menu"})
}

func (h *MenuHandler) UpdateEntryServes(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	recipeID := c.Param("recipeId")
	if !parseID(c, recipeID) {
		return
	}
	serves, ok := parseMenuEntryServesRequest(c)
	if !ok {
		return
	}

	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		if err := h.repo.UpdateEntryServes(ctx, userID, recipeID, serves); err != nil {
			return err
		}
		return h.shoppingLists.Regenerate(ctx, userID)
	})
	if txErr != nil {
		if errors.Is(txErr, models.ErrMenuEntryNotFound) {
			notFound(c, "menu_entry_not_found")
			return
		}
		internalError(c, "failed to update menu entry", txErr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "menu entry updated"})
}

func (h *MenuHandler) RemoveEntry(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	recipeID := c.Param("recipeId")
	if !parseID(c, recipeID) {
		return
	}

	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		if err := h.repo.RemoveEntry(ctx, userID, recipeID); err != nil {
			return err
		}
		return h.shoppingLists.Regenerate(ctx, userID)
	})
	if txErr != nil {
		internalError(c, "failed to remove menu entry", txErr)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "recipe removed from menu"})
}
