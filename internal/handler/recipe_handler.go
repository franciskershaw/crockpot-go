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
	List(ctx context.Context, filter models.RecipeListFilter) ([]*models.RecipeCard, int, error)
	GetByID(ctx context.Context, id string, callerID *string, callerIsAdmin bool) (*models.RecipeDetail, error)

	AddFavourite(ctx context.Context, userID, recipeID string, callerIsAdmin bool) error
	RemoveFavourite(ctx context.Context, userID, recipeID string) error
	ListFavourites(ctx context.Context, userID string, page, limit int) ([]*models.RecipeCard, int, error)
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

func (h *RecipeHandler) List(c *gin.Context) {
	filter, ok := parseRecipeListFilter(c)
	if !ok {
		return
	}
	if userID, ok := userIDFromCtx(c); ok {
		filter.CallerID = &userID
	}
	filter.CallerIsAdmin = c.GetString("role") == "ADMIN"

	cards, total, err := h.repo.List(c.Request.Context(), filter)
	if err != nil {
		internalError(c, "failed to list recipes", err)
		return
	}
	if cards == nil {
		cards = []*models.RecipeCard{}
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + filter.Limit - 1) / filter.Limit
	}

	c.JSON(http.StatusOK, gin.H{
		"recipes":    cards,
		"page":       filter.Page,
		"limit":      filter.Limit,
		"total":      total,
		"totalPages": totalPages,
	})
}

func (h *RecipeHandler) Get(c *gin.Context) {
	id := c.Param("id")
	if !parseID(c, id) {
		return
	}

	var callerID *string
	if userID, ok := userIDFromCtx(c); ok {
		callerID = &userID
	}
	isAdmin := c.GetString("role") == "ADMIN"

	detail, err := h.repo.GetByID(c.Request.Context(), id, callerID, isAdmin)
	if err != nil {
		if errors.Is(err, models.ErrRecipeNotFound) {
			notFound(c, "not_found")
			return
		}
		internalError(c, "failed to get recipe", err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

func (h *RecipeHandler) AddFavourite(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	id := c.Param("id")
	if !parseID(c, id) {
		return
	}
	isAdmin := c.GetString("role") == "ADMIN"

	if err := h.repo.AddFavourite(c.Request.Context(), userID, id, isAdmin); err != nil {
		if errors.Is(err, models.ErrRecipeNotFound) {
			notFound(c, "not_found")
			return
		}
		internalError(c, "failed to add favourite", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "recipe favourited"})
}

func (h *RecipeHandler) RemoveFavourite(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	id := c.Param("id")
	if !parseID(c, id) {
		return
	}

	if err := h.repo.RemoveFavourite(c.Request.Context(), userID, id); err != nil {
		internalError(c, "failed to remove favourite", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "recipe unfavourited"})
}

func (h *RecipeHandler) ListFavourites(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}

	page, ok := parseIntQuery(c, "page", 1)
	if !ok {
		return
	}
	limit, ok := parseIntQuery(c, "limit", 20)
	if !ok {
		return
	}
	page = clampInt(page, 1, 1_000_000)
	limit = clampInt(limit, 1, 50)

	cards, total, err := h.repo.ListFavourites(c.Request.Context(), userID, page, limit)
	if err != nil {
		internalError(c, "failed to list favourites", err)
		return
	}
	if cards == nil {
		cards = []*models.RecipeCard{}
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + limit - 1) / limit
	}

	c.JSON(http.StatusOK, gin.H{
		"recipes":    cards,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": totalPages,
	})
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
