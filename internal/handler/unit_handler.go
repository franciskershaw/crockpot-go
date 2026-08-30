package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type UnitRepository interface {
	List(ctx context.Context) ([]*models.Unit, error)
	Create(ctx context.Context, name, abbreviation string) (*models.Unit, error)
	Update(ctx context.Context, id, name, abbreviation string) (*models.Unit, error)
	Delete(ctx context.Context, id string) error
}

type UnitHandler struct {
	repo UnitRepository
}

func NewUnitHandler(repo UnitRepository) *UnitHandler {
	return &UnitHandler{repo: repo}
}

type createUnitRequest struct {
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
}

type updateUnitRequest struct {
	Name         *string `json:"name"`
	Abbreviation *string `json:"abbreviation"`
}

func (h *UnitHandler) List(c *gin.Context) {
	units, err := h.repo.List(c.Request.Context())
	if err != nil {
		internalError(c, "failed to list units", err)
		return
	}
	c.JSON(http.StatusOK, units)
}

func (h *UnitHandler) Create(c *gin.Context) {
	var req createUnitRequest
	if !bindJSON(c, &req) {
		return
	}
	name, ok := validateName(c, req.Name)
	if !ok {
		return
	}
	abbreviation, ok := validateAbbreviation(c, req.Abbreviation)
	if !ok {
		return
	}

	unit, err := h.repo.Create(c.Request.Context(), name, abbreviation)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrUnitNameTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "name_taken"})
		case errors.Is(err, models.ErrUnitAbbreviationTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "abbreviation_taken"})
		default:
			internalError(c, "failed to create unit", err)
		}
		return
	}
	c.JSON(http.StatusCreated, unit)
}

func (h *UnitHandler) Update(c *gin.Context) {
	var req updateUnitRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Name == nil && req.Abbreviation == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	var name, abbreviation string
	if req.Name != nil {
		validated, ok := validateName(c, *req.Name)
		if !ok {
			return
		}
		name = validated
	}
	if req.Abbreviation != nil {
		validated, ok := validateAbbreviation(c, *req.Abbreviation)
		if !ok {
			return
		}
		abbreviation = validated
	}

	unit, err := h.repo.Update(c.Request.Context(), c.Param("id"), name, abbreviation)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrUnitNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, models.ErrUnitNameTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "name_taken"})
		case errors.Is(err, models.ErrUnitAbbreviationTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "abbreviation_taken"})
		default:
			internalError(c, "failed to update unit", err)
		}
		return
	}
	c.JSON(http.StatusOK, unit)
}

func (h *UnitHandler) Delete(c *gin.Context) {
	err := h.repo.Delete(c.Request.Context(), c.Param("id"))
	if err != nil {
		switch {
		case errors.Is(err, models.ErrUnitNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, models.ErrUnitInUse):
			c.JSON(http.StatusConflict, gin.H{"error": "unit_in_use"})
		default:
			internalError(c, "failed to delete unit", err)
		}
		return
	}
	c.Status(http.StatusNoContent)
}
