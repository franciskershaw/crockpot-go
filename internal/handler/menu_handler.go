package handler

import (
	"context"
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
	c.Status(http.StatusNotImplemented)
}

func (h *MenuHandler) UpsertEntry(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

func (h *MenuHandler) UpdateEntryServes(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

func (h *MenuHandler) RemoveEntry(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}
