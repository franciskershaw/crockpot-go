package handler

import (
	"context"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type ItemRepository interface {
	List(ctx context.Context) ([]*models.Item, error)
	Create(ctx context.Context, name, categoryID string, unitIDs []string) (*models.Item, error)
	Update(ctx context.Context, id, name, categoryID string, unitIDs []string) (*models.Item, error)
	Delete(ctx context.Context, id string) error
}

type ItemHandler struct {
	repo       ItemRepository
	transactor Transactor
}

func NewItemHandler(repo ItemRepository, transactor Transactor) *ItemHandler {
	return &ItemHandler{repo: repo, transactor: transactor}
}

type createItemRequest struct {
	Name           string   `json:"name"`
	CategoryID     string   `json:"categoryId"`
	AllowedUnitIDs []string `json:"allowedUnitIds"`
}

type updateItemRequest struct {
	Name           *string   `json:"name"`
	CategoryID     *string   `json:"categoryId"`
	AllowedUnitIDs *[]string `json:"allowedUnitIds"`
}

func (h *ItemHandler) List(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *ItemHandler) Create(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *ItemHandler) Update(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *ItemHandler) Delete(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}
