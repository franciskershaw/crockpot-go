package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// recipeLimits maps a role to its max owned-recipe count; a role absent from the map is uncapped.
var recipeLimits = map[string]int{"FREE": 5}

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

func (h *RecipeHandler) Create(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	role := c.GetString("role")

	input, ok := parseCreateRecipeInput(c)
	if !ok {
		return
	}

	creatorID, err := uuid.Parse(userID)
	if err != nil {
		internalError(c, "invalid user id in token", err)
		return
	}
	input.CreatedByID = creatorID
	input.Approved = role == "ADMIN"

	if !h.withinRecipeCap(c, role, userID) {
		return
	}

	var recipe *models.Recipe
	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		var err error
		recipe, err = h.repo.Create(ctx, input)
		return err
	})
	if txErr != nil {
		writeRecipeCreateError(c, txErr)
		return
	}
	c.JSON(http.StatusCreated, recipe)
}

// withinRecipeCap returns false (and writes the 409/500) when a capped role is at its limit or the count lookup fails.
func (h *RecipeHandler) withinRecipeCap(c *gin.Context, role, userID string) bool {
	limit, capped := recipeLimits[role]
	if !capped {
		return true
	}
	count, err := h.repo.CountByCreator(c.Request.Context(), userID)
	if err != nil {
		internalError(c, "failed to count recipes", err)
		return false
	}
	if count >= limit {
		conflict(c, "recipe_limit_reached")
		return false
	}
	return true
}

func writeRecipeCreateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, models.ErrRecipeInvalidItem):
		badRequest(c, "invalid_item_id")
	case errors.Is(err, models.ErrRecipeInvalidUnit):
		badRequest(c, "invalid_unit_id")
	case errors.Is(err, models.ErrRecipeInvalidCategory):
		badRequest(c, "invalid_category_id")
	case errors.Is(err, models.ErrIngredientUnitNotAllowed):
		badRequest(c, "unit_not_allowed_for_item")
	case errors.Is(err, models.ErrRecipeDuplicateIngredient):
		badRequest(c, "duplicate_ingredient")
	default:
		internalError(c, "failed to create recipe", err)
	}
}
