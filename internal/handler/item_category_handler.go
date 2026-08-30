package handler

import (
	"context"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type ItemCategoryRepository interface {
	List(ctx context.Context) ([]*models.ItemCategory, error)
	Create(ctx context.Context, name, icon string) (*models.ItemCategory, error)
	Update(ctx context.Context, id, name, icon string) (*models.ItemCategory, error)
	Delete(ctx context.Context, id string) error
}

type ItemCategoryHandler struct {
	repo ItemCategoryRepository
}

func NewItemCategoryHandler(repo ItemCategoryRepository) *ItemCategoryHandler {
	return &ItemCategoryHandler{repo: repo}
}

type createItemCategoryRequest struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

type updateItemCategoryRequest struct {
	Name *string `json:"name"`
	Icon *string `json:"icon"`
}

func (h *ItemCategoryHandler) List(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *ItemCategoryHandler) Create(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *ItemCategoryHandler) Update(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *ItemCategoryHandler) Delete(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}
