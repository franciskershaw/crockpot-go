package handler

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/franciskershaw/crockpot-go/internal/cloudinary"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ulule/limiter/v3"
)

// recipeLimits maps a role to its max owned-recipe count; a role absent from the map is uncapped.
var recipeLimits = map[string]int{"FREE": 5}

type RecipeRepository interface {
	Create(ctx context.Context, input models.CreateRecipeInput) (*models.RecipeDetail, error)
	Update(ctx context.Context, id string, input models.CreateRecipeInput, callerID string, callerIsAdmin bool) (detail *models.RecipeDetail, orphanedImage *string, err error)
	Delete(ctx context.Context, id string, callerID string, callerIsAdmin bool) (menuUserIDs []string, orphanedImage *string, err error)
	CheckWritable(ctx context.Context, id string, callerID string, callerIsAdmin bool) error
	MenuUserIDs(ctx context.Context, id string) ([]string, error)
	CountByCreator(ctx context.Context, userID string) (int, error)
	List(ctx context.Context, filter models.RecipeListFilter) ([]*models.RecipeCard, int, error)
	GetByID(ctx context.Context, id string, callerID *string, callerIsAdmin bool) (*models.RecipeDetail, error)
	GetTimeRange(ctx context.Context) (*models.RecipeTimeRange, error)

	AddFavourite(ctx context.Context, userID, recipeID string, callerIsAdmin bool) error
	RemoveFavourite(ctx context.Context, userID, recipeID string) error
	ListFavourites(ctx context.Context, userID string, page, limit int) ([]*models.RecipeCard, int, error)
}

type ImageStore interface {
	Upload(ctx context.Context, file io.Reader, publicID string) (cloudinary.UploadedImage, error)
	Destroy(ctx context.Context, publicID string) error
}

// RecipeImages is what the recipe handler needs for photos: PhotoLimiter is keyed per user and only counts saves that carry a photo.
type RecipeImages struct {
	Store        ImageStore
	Scope        ImageScope
	PhotoLimiter *limiter.Limiter
	NewID        func() string
}

type RecipeHandler struct {
	repo          RecipeRepository
	shoppingLists ShoppingListRegenerator
	transactor    Transactor
	images        RecipeImages
}

func NewRecipeHandler(repo RecipeRepository, shoppingLists ShoppingListRegenerator, transactor Transactor, images RecipeImages) *RecipeHandler {
	return &RecipeHandler{repo: repo, shoppingLists: shoppingLists, transactor: transactor, images: images}
}

func (h *RecipeHandler) Create(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	role := c.GetString("role")

	write, ok := parseRecipeWrite(c, false)
	if !ok {
		return
	}
	input := write.input

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
	var uploaded *cloudinary.UploadedImage
	if write.photo != nil {
		if uploaded, ok = h.uploadPhoto(c, userID, write.photo); !ok {
			return
		}
		input.ImageURL, input.ImageFilename = &uploaded.SecureURL, &uploaded.PublicID
	}

	var recipe *models.RecipeDetail
	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		var err error
		recipe, err = h.repo.Create(ctx, input)
		return err
	})
	if txErr != nil {
		if uploaded != nil {
			h.destroyImage(c, uploaded.PublicID)
		}
		writeRecipeWriteError(c, txErr)
		return
	}
	c.JSON(http.StatusCreated, recipe)
}

func (h *RecipeHandler) Update(c *gin.Context) {
	userID, ok := userIDFromCtx(c)
	if !ok {
		unauthorized(c, "unauthorized")
		return
	}
	id := c.Param("id")
	if !parseID(c, id) {
		return
	}
	write, ok := parseRecipeWrite(c, true)
	if !ok {
		return
	}
	input := write.input
	isAdmin := c.GetString("role") == "ADMIN"

	var uploaded *cloudinary.UploadedImage
	switch {
	case write.photo != nil:
		if err := h.repo.CheckWritable(c.Request.Context(), id, userID, isAdmin); err != nil {
			writeRecipeWriteError(c, err)
			return
		}
		if uploaded, ok = h.uploadPhoto(c, userID, write.photo); !ok {
			return
		}
		input.Image = models.ImageReplace
		input.ImageURL, input.ImageFilename = &uploaded.SecureURL, &uploaded.PublicID
	case write.removeImage:
		input.Image = models.ImageRemove
	}

	var detail *models.RecipeDetail
	var orphan *string
	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		var err error
		detail, orphan, err = h.repo.Update(ctx, id, input, userID, isAdmin)
		if err != nil {
			return err
		}
		holders, err := h.repo.MenuUserIDs(ctx, id)
		if err != nil {
			return err
		}
		return h.regenerateShoppingLists(ctx, holders)
	})
	if txErr != nil {
		if uploaded != nil {
			h.destroyImage(c, uploaded.PublicID)
		}
		writeRecipeWriteError(c, txErr)
		return
	}
	h.destroyOrphan(c, orphan)
	c.JSON(http.StatusOK, detail)
}

func (h *RecipeHandler) Delete(c *gin.Context) {
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

	var orphan *string
	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		var affected []string
		var err error
		affected, orphan, err = h.repo.Delete(ctx, id, userID, isAdmin)
		if err != nil {
			return err
		}
		return h.regenerateShoppingLists(ctx, affected)
	})
	if txErr != nil {
		writeRecipeWriteError(c, txErr)
		return
	}
	h.destroyOrphan(c, orphan)
	c.Status(http.StatusNoContent)
}

func (h *RecipeHandler) regenerateShoppingLists(ctx context.Context, userIDs []string) error {
	for _, uid := range userIDs {
		if err := h.shoppingLists.Regenerate(ctx, uid); err != nil {
			return err
		}
	}
	return nil
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

func (h *RecipeHandler) GetTimeRange(c *gin.Context) {
	timeRange, err := h.repo.GetTimeRange(c.Request.Context())
	if err != nil {
		internalError(c, "failed to get recipe time range", err)
		return
	}
	c.JSON(http.StatusOK, timeRange)
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

	txErr := h.transactor.WithinTx(c.Request.Context(), func(ctx context.Context) error {
		return h.repo.AddFavourite(ctx, userID, id, isAdmin)
	})
	if txErr != nil {
		if errors.Is(txErr, models.ErrRecipeNotFound) {
			notFound(c, "not_found")
			return
		}
		internalError(c, "failed to add favourite", txErr)
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

func writeRecipeWriteError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, models.ErrRecipeInvalidItem):
		badRequest(c, "invalid_item_id")
	case errors.Is(err, models.ErrRecipeInvalidUnit):
		badRequest(c, "invalid_unit_id")
	case errors.Is(err, models.ErrRecipeInvalidCategory):
		badRequest(c, "invalid_category_id")
	case errors.Is(err, models.ErrIngredientUnitNotAllowed):
		badRequest(c, "unit_not_allowed_for_item")
	case errors.Is(err, models.ErrRecipeNonIngredientItem):
		badRequest(c, "item_not_ingredient")
	case errors.Is(err, models.ErrRecipeDuplicateIngredient):
		badRequest(c, "duplicate_ingredient")
	case errors.Is(err, models.ErrRecipeNotFound):
		notFound(c, "not_found")
	case errors.Is(err, models.ErrRecipeForbidden):
		forbidden(c, "forbidden")
	case errors.Is(err, models.ErrRecipeApprovedLocked):
		forbidden(c, "recipe_approved_locked")
	default:
		internalError(c, "failed to write recipe", err)
	}
}
