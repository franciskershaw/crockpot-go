package handler

import (
	"context"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type ShoppingListRepository interface {
	Get(ctx context.Context, userID string) (*models.ShoppingList, error)
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
