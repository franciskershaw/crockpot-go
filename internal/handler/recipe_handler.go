package handler

import (
	"context"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
)

type RecipeRepository interface {
	Create(ctx context.Context, input models.CreateRecipeInput) (*models.Recipe, error)
	CountByCreator(ctx context.Context, userID string) (int, error)
}

type RecipeHandler struct {
	repo       RecipeRepository
	transactor Transactor
}

func NewRecipeHandler(repo RecipeRepository, transactor Transactor) *RecipeHandler {
	return &RecipeHandler{repo: repo, transactor: transactor}
}

type createRecipeRequest struct {
	Name          string                    `json:"name"`
	TimeInMinutes int                       `json:"timeInMinutes"`
	Serves        int                       `json:"serves"`
	Instructions  []string                  `json:"instructions"`
	Notes         []string                  `json:"notes"`
	CategoryIDs   []string                  `json:"categoryIds"`
	Ingredients   []createIngredientRequest `json:"ingredients"`
	Image         *recipeImageRequest       `json:"image"`
}

type createIngredientRequest struct {
	ItemID   string   `json:"itemId"`
	UnitID   *string  `json:"unitId"`
	Quantity *float64 `json:"quantity"`
}

type recipeImageRequest struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
}

func (h *RecipeHandler) Create(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"error": "STUB_RecipeHandler_Create_NOT_IMPLEMENTED"})
}
