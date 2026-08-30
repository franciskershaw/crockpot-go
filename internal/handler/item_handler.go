package handler

import (
	"context"
	"errors"
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
	items, err := h.repo.List(c.Request.Context())
	if err != nil {
		internalError(c, "failed to list items", err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *ItemHandler) Create(c *gin.Context) {
	var req createItemRequest
	if !bindJSON(c, &req) {
		return
	}
	name, ok := validateName(c, req.Name)
	if !ok {
		return
	}
	categoryID, ok := validateCategoryID(c, req.CategoryID)
	if !ok {
		return
	}
	for _, unitID := range req.AllowedUnitIDs {
		if !parseID(c, unitID) {
			return
		}
	}

	var item *models.Item
	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		var err error
		item, err = h.repo.Create(ctx, name, categoryID, req.AllowedUnitIDs)
		return err
	})
	if txErr != nil {
		writeItemWriteError(c, txErr)
		return
	}
	c.JSON(http.StatusCreated, item)
}

func (h *ItemHandler) Update(c *gin.Context) {
	var req updateItemRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == nil && req.CategoryID == nil && req.AllowedUnitIDs == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	var name, categoryID string
	if req.Name != nil {
		validated, ok := validateName(c, *req.Name)
		if !ok {
			return
		}
		name = validated
	}
	if req.CategoryID != nil {
		validated, ok := validateCategoryID(c, *req.CategoryID)
		if !ok {
			return
		}
		categoryID = validated
	}
	var unitIDs []string
	if req.AllowedUnitIDs != nil {
		for _, unitID := range *req.AllowedUnitIDs {
			if !parseID(c, unitID) {
				return
			}
		}
		unitIDs = *req.AllowedUnitIDs
	}

	id := c.Param("id")
	var item *models.Item
	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		var err error
		item, err = h.repo.Update(ctx, id, name, categoryID, unitIDs)
		return err
	})
	if txErr != nil {
		if errors.Is(txErr, models.ErrItemNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		writeItemWriteError(c, txErr)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *ItemHandler) Delete(c *gin.Context) {
	err := h.repo.Delete(c.Request.Context(), c.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, models.ErrItemNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, models.ErrItemInUse):
			c.JSON(http.StatusConflict, gin.H{"error": "item_in_use"})
		default:
			internalError(c, "failed to delete item", err)
		}
		return
	}
	c.Status(http.StatusNoContent)
}

// writeItemWriteError handles the error set shared by Create and Update — not ErrItemNotFound,
// which only Update can return and which its caller checks first.
func writeItemWriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, models.ErrItemNameTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "name_taken"})
	case errors.Is(err, models.ErrItemInvalidCategory):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_category_id"})
	case errors.Is(err, models.ErrItemInvalidUnit):
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_unit_id"})
	default:
		internalError(c, "failed to write item", err)
	}
}
