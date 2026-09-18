package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type ShoppingListRepository interface {
	Get(ctx context.Context, userID string) (*models.ShoppingList, error)
	AddManualItem(ctx context.Context, userID, itemID string, unitID *string, quantity float64) error
	UpdateItem(ctx context.Context, userID, itemRowID string, obtained *bool, quantity *float64) error
	DeleteItem(ctx context.Context, userID, itemRowID string) error
}

type ShoppingListHandler struct {
	repo       ShoppingListRepository
	transactor Transactor
}

func NewShoppingListHandler(repo ShoppingListRepository, transactor Transactor) *ShoppingListHandler {
	return &ShoppingListHandler{repo: repo, transactor: transactor}
}

func (h *ShoppingListHandler) Get(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	list, err := h.repo.Get(c.Request.Context(), userID)
	if err != nil {
		internalError(c, "failed to get shopping list", err)
		return
	}
	c.JSON(http.StatusOK, list)
}

func (h *ShoppingListHandler) AddItem(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	itemID, unitID, quantity, ok := parseAddManualShoppingListItemRequest(c)
	if !ok {
		return
	}

	err := h.repo.AddManualItem(c.Request.Context(), userID, itemID, unitID, quantity)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"message": "item added to shopping list"})
	case errors.Is(err, models.ErrShoppingListInvalidItem):
		badRequest(c, "invalid_item")
	case errors.Is(err, models.ErrShoppingListInvalidUnit):
		badRequest(c, "invalid_unit")
	case errors.Is(err, models.ErrIngredientUnitNotAllowed):
		badRequest(c, "unit_not_allowed")
	default:
		internalError(c, "failed to add manual shopping list item", err)
	}
}

func (h *ShoppingListHandler) UpdateItem(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	itemRowID := c.Param("id")
	if !parseID(c, itemRowID) {
		return
	}
	obtained, quantity, ok := parseUpdateShoppingListItemRequest(c)
	if !ok {
		return
	}

	err := h.repo.UpdateItem(c.Request.Context(), userID, itemRowID, obtained, quantity)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"message": "shopping list item updated"})
	case errors.Is(err, models.ErrShoppingListItemNotFound):
		notFound(c, "shopping_list_item_not_found")
	default:
		internalError(c, "failed to update shopping list item", err)
	}
}

func (h *ShoppingListHandler) DeleteItem(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	itemRowID := c.Param("id")
	if !parseID(c, itemRowID) {
		return
	}

	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		return h.repo.DeleteItem(ctx, userID, itemRowID)
	})
	switch {
	case txErr == nil:
		c.JSON(http.StatusOK, gin.H{"message": "item removed from shopping list"})
	case errors.Is(txErr, models.ErrShoppingListItemNotFound):
		notFound(c, "shopping_list_item_not_found")
	default:
		internalError(c, "failed to delete shopping list item", txErr)
	}
}
