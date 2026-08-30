package handler

import (
	"context"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type RecipeCategoryRepository interface {
	List(ctx context.Context) ([]*models.RecipeCategory, error)
	Create(ctx context.Context, name string) (*models.RecipeCategory, error)
	Update(ctx context.Context, id, name string) (*models.RecipeCategory, error)
	Delete(ctx context.Context, id string) error
}

type RecipeCategoryHandler struct {
	repo RecipeCategoryRepository
}

func NewRecipeCategoryHandler(repo RecipeCategoryRepository) *RecipeCategoryHandler {
	return &RecipeCategoryHandler{repo: repo}
}

type createRecipeCategoryRequest struct {
	Name string `json:"name"`
}

type updateRecipeCategoryRequest struct {
	Name string `json:"name"`
}

func (h *RecipeCategoryHandler) List(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *RecipeCategoryHandler) Create(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *RecipeCategoryHandler) Update(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *RecipeCategoryHandler) Delete(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}
