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
}

type ShoppingListHandler struct {
	repo ShoppingListRepository
}

func NewShoppingListHandler(repo ShoppingListRepository) *ShoppingListHandler {
	return &ShoppingListHandler{repo: repo}
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
