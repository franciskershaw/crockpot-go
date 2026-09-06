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

type MenuHandler struct {
	repo MenuRepository
}

func NewMenuHandler(repo MenuRepository) *MenuHandler {
	return &MenuHandler{repo: repo}
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

	if err := h.repo.UpsertEntry(c.Request.Context(), userID, recipeID, serves, isAdmin); err != nil {
		if errors.Is(err, models.ErrRecipeNotFound) {
			notFound(c, "not_found")
			return
		}
		internalError(c, "failed to upsert menu entry", err)
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

	if err := h.repo.UpdateEntryServes(c.Request.Context(), userID, recipeID, serves); err != nil {
		if errors.Is(err, models.ErrMenuEntryNotFound) {
			notFound(c, "menu_entry_not_found")
			return
		}
		internalError(c, "failed to update menu entry", err)
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

	if err := h.repo.RemoveEntry(c.Request.Context(), userID, recipeID); err != nil {
		internalError(c, "failed to remove menu entry", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "recipe removed from menu"})
}
