package handler

import (
	"context"
	"errors"
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
	categories, err := h.repo.List(c.Request.Context())
	if err != nil {
		internalError(c, "failed to list item categories", err)
		return
	}
	c.JSON(http.StatusOK, categories)
}

func (h *ItemCategoryHandler) Create(c *gin.Context) {
	var req createItemCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	name, ok := validateName(c, req.Name)
	if !ok {
		return
	}
	icon, ok := validateIconToken(c, req.Icon)
	if !ok {
		return
	}

	category, err := h.repo.Create(c.Request.Context(), name, icon)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrItemCategoryNameTaken):
			conflict(c, "name_taken")
		case errors.Is(err, models.ErrItemCategoryIconTaken):
			conflict(c, "icon_taken")
		default:
			internalError(c, "failed to create item category", err)
		}
		return
	}
	c.JSON(http.StatusCreated, category)
}

func (h *ItemCategoryHandler) Update(c *gin.Context) {
	var req updateItemCategoryRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == nil && req.Icon == nil {
		badRequest(c, "invalid_request")
		return
	}

	var name, icon string
	if req.Name != nil {
		validated, ok := validateName(c, *req.Name)
		if !ok {
			return
		}
		name = validated
	}
	if req.Icon != nil {
		validated, ok := validateIconToken(c, *req.Icon)
		if !ok {
			return
		}
		icon = validated
	}

	category, err := h.repo.Update(c.Request.Context(), c.Param("id"), name, icon)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrItemCategoryNotFound):
			notFound(c, "not_found")
		case errors.Is(err, models.ErrItemCategoryNameTaken):
			conflict(c, "name_taken")
		case errors.Is(err, models.ErrItemCategoryIconTaken):
			conflict(c, "icon_taken")
		default:
			internalError(c, "failed to update item category", err)
		}
		return
	}
	c.JSON(http.StatusOK, category)
}

func (h *ItemCategoryHandler) Delete(c *gin.Context) {
	err := h.repo.Delete(c.Request.Context(), c.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, models.ErrItemCategoryNotFound):
			notFound(c, "not_found")
		case errors.Is(err, models.ErrItemCategoryInUse):
			conflict(c, "category_in_use")
		default:
			internalError(c, "failed to delete item category", err)
		}
		return
	}
	c.Status(http.StatusNoContent)
}
