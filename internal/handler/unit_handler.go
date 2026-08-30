package handler

import (
	"context"
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
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *UnitHandler) Create(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *UnitHandler) Update(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}

func (h *UnitHandler) Delete(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_NOT_IMPLEMENTED"})
}
