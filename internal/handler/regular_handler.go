package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

const maxRegularsPerUser = 50

type RegularRepository interface {
	List(ctx context.Context, userID string) ([]models.Regular, error)
	Create(ctx context.Context, userID, itemID string, unitID *string, quantity float64, limit int) (*models.Regular, error)
	Update(ctx context.Context, userID, regularID string, unitID *string, quantity float64) (*models.Regular, error)
	Delete(ctx context.Context, userID, regularID string) error
}

type RegularHandler struct {
	repo RegularRepository
}

func NewRegularHandler(repo RegularRepository) *RegularHandler {
	return &RegularHandler{repo: repo}
}

func (h *RegularHandler) List(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	regulars, err := h.repo.List(c.Request.Context(), userID)
	if err != nil {
		internalError(c, "failed to list regulars", err)
		return
	}
	c.JSON(http.StatusOK, regulars)
}

func (h *RegularHandler) Create(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	itemID, unitID, quantity, ok := parseItemQuantityRequest(c)
	if !ok {
		return
	}

	regular, err := h.repo.Create(c.Request.Context(), userID, itemID, unitID, quantity, maxRegularsPerUser)
	if err != nil {
		writeRegularWriteError(c, err, "failed to create regular")
		return
	}
	c.JSON(http.StatusCreated, regular)
}

func (h *RegularHandler) Update(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	regularID := c.Param("id")
	if !parseID(c, regularID) {
		return
	}
	unitID, quantity, ok := parseUpdateRegularRequest(c)
	if !ok {
		return
	}

	regular, err := h.repo.Update(c.Request.Context(), userID, regularID, unitID, quantity)
	if err != nil {
		writeRegularWriteError(c, err, "failed to update regular")
		return
	}
	c.JSON(http.StatusOK, regular)
}

func (h *RegularHandler) Delete(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	regularID := c.Param("id")
	if !parseID(c, regularID) {
		return
	}

	err := h.repo.Delete(c.Request.Context(), userID, regularID)
	switch {
	case err == nil:
		c.Status(http.StatusNoContent)
	case errors.Is(err, models.ErrRegularNotFound):
		notFound(c, "regular_not_found")
	default:
		internalError(c, "failed to delete regular", err)
	}
}

func writeRegularWriteError(c *gin.Context, err error, logMsg string) {
	switch {
	case errors.Is(err, models.ErrRegularNotFound):
		notFound(c, "regular_not_found")
	case errors.Is(err, models.ErrRegularInvalidItem):
		badRequest(c, "invalid_item_id")
	case errors.Is(err, models.ErrRegularInvalidUnit):
		badRequest(c, "invalid_unit_id")
	case errors.Is(err, models.ErrIngredientUnitNotAllowed):
		badRequest(c, "unit_not_allowed_for_item")
	case errors.Is(err, models.ErrRegularExists):
		conflict(c, "regular_exists")
	case errors.Is(err, models.ErrRegularsLimitReached):
		conflict(c, "regulars_limit_reached")
	default:
		internalError(c, logMsg, err)
	}
}
