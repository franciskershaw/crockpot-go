package handler

import (
	"context"
	"errors"
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
	categories, err := h.repo.List(c.Request.Context())
	if err != nil {
		internalError(c, "failed to list recipe categories", err)
		return
	}
	c.JSON(http.StatusOK, categories)
}

func (h *RecipeCategoryHandler) Create(c *gin.Context) {
	var req createRecipeCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	name, ok := validateName(c, req.Name)
	if !ok {
		return
	}

	category, err := h.repo.Create(c.Request.Context(), name)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrRecipeCategoryNameTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "name_taken"})
		default:
			internalError(c, "failed to create recipe category", err)
		}
		return
	}
	c.JSON(http.StatusCreated, category)
}

func (h *RecipeCategoryHandler) Update(c *gin.Context) {
	var req updateRecipeCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	name, ok := validateName(c, req.Name)
	if !ok {
		return
	}

	category, err := h.repo.Update(c.Request.Context(), c.Param("id"), name)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrRecipeCategoryNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, models.ErrRecipeCategoryNameTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "name_taken"})
		default:
			internalError(c, "failed to update recipe category", err)
		}
		return
	}
	c.JSON(http.StatusOK, category)
}

func (h *RecipeCategoryHandler) Delete(c *gin.Context) {
	err := h.repo.Delete(c.Request.Context(), c.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, models.ErrRecipeCategoryNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, models.ErrRecipeCategoryInUse):
			c.JSON(http.StatusConflict, gin.H{"error": "category_in_use"})
		default:
			internalError(c, "failed to delete recipe category", err)
		}
		return
	}
	c.Status(http.StatusNoContent)
}
