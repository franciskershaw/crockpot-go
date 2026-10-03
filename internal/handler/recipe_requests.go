package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type createRecipeRequest struct {
	Name          string                    `json:"name"`
	Description   *string                   `json:"description"`
	TimeInMinutes int                       `json:"timeInMinutes"`
	Serves        int                       `json:"serves"`
	Instructions  []string                  `json:"instructions"`
	Notes         []string                  `json:"notes"`
	CategoryIDs   []string                  `json:"categoryIds"`
	Ingredients   []createIngredientRequest `json:"ingredients"`
	RemoveImage   bool                      `json:"removeImage"`
}

type createIngredientRequest struct {
	ItemID   string   `json:"itemId"`
	UnitID   *string  `json:"unitId"`
	Quantity *float64 `json:"quantity"`
}

const maxPhotoBytes = 5 << 20

// multipartMemoryBytes keeps a whole capped recipe write in memory rather than spilling the photo to a temp file.
const multipartMemoryBytes = 8 << 20

var allowedPhotoTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// recipeWrite is a parsed recipe create/update: photo is nil when none was sent.
type recipeWrite struct {
	input       models.CreateRecipeInput
	photo       []byte
	removeImage bool
}

// parseRecipeWrite reads the multipart body (a "recipe" JSON part plus an optional "photo"); writes the error response and returns false on failure.
func parseRecipeWrite(c *gin.Context, isUpdate bool) (recipeWrite, bool) {
	if c.ContentType() != "multipart/form-data" {
		badRequest(c, "invalid_request")
		return recipeWrite{}, false
	}
	if err := c.Request.ParseMultipartForm(multipartMemoryBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request_too_large"})
		} else {
			badRequest(c, "invalid_request")
		}
		return recipeWrite{}, false
	}
	raw, ok := c.GetPostForm("recipe")
	if !ok {
		badRequest(c, "invalid_request")
		return recipeWrite{}, false
	}
	var req createRecipeRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		badRequest(c, "invalid_request")
		return recipeWrite{}, false
	}
	input, ok := validateRecipeRequest(c, req)
	if !ok {
		return recipeWrite{}, false
	}
	if req.RemoveImage && !isUpdate {
		badRequest(c, "invalid_image")
		return recipeWrite{}, false
	}
	photo, ok := readRecipePhoto(c)
	if !ok {
		return recipeWrite{}, false
	}
	if photo != nil && req.RemoveImage {
		badRequest(c, "invalid_image")
		return recipeWrite{}, false
	}
	return recipeWrite{input: input, photo: photo, removeImage: req.RemoveImage}, true
}

// readRecipePhoto returns the "photo" part's bytes, nil if absent; the type is sniffed from content, never the declared header.
func readRecipePhoto(c *gin.Context) ([]byte, bool) {
	fh, err := c.FormFile("photo")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, true
	}
	if err != nil {
		badRequest(c, "invalid_request")
		return nil, false
	}
	if fh.Size > maxPhotoBytes {
		badRequest(c, "image_too_large")
		return nil, false
	}
	f, err := fh.Open()
	if err != nil {
		badRequest(c, "invalid_request")
		return nil, false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxPhotoBytes+1))
	if err != nil {
		badRequest(c, "invalid_request")
		return nil, false
	}
	if len(data) > maxPhotoBytes {
		badRequest(c, "image_too_large")
		return nil, false
	}
	if !allowedPhotoTypes[http.DetectContentType(data)] {
		badRequest(c, "invalid_image")
		return nil, false
	}
	return data, true
}

// validateRecipeRequest validates the decoded body into a CreateRecipeInput (CreatedByID/Approved left for the handler).
func validateRecipeRequest(c *gin.Context, req createRecipeRequest) (models.CreateRecipeInput, bool) {

	name, ok := validateRecipeName(c, req.Name)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	description, ok := validateRecipeDescription(c, req.Description)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	if req.TimeInMinutes < 1 || req.TimeInMinutes > 1440 {
		badRequest(c, "invalid_time")
		return models.CreateRecipeInput{}, false
	}
	serves, ok := validateServes(c, req.Serves)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	instructions, ok := validateInstructions(c, req.Instructions)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	notes, ok := validateNotes(c, req.Notes)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	categoryIDs, ok := validateCategoryIDs(c, req.CategoryIDs)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	ingredients, ok := validateIngredients(c, req.Ingredients)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	return models.CreateRecipeInput{
		Name:          name,
		Description:   description,
		TimeInMinutes: req.TimeInMinutes,
		Serves:        serves,
		Instructions:  instructions,
		Notes:         notes,
		CategoryIDs:   categoryIDs,
		Ingredients:   ingredients,
	}, true
}

func validateRecipeName(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "":
		badRequest(c, "name_required")
		return "", false
	case len(trimmed) < 3:
		badRequest(c, "name_too_short")
		return "", false
	case len(trimmed) > 100:
		badRequest(c, "name_too_long")
		return "", false
	}
	return trimmed, true
}

func validateRecipeDescription(c *gin.Context, raw *string) (*string, bool) {
	if raw == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, true
	}
	if len(trimmed) > 500 {
		badRequest(c, "description_too_long")
		return nil, false
	}
	return &trimmed, true
}

func validateInstructions(c *gin.Context, raw []string) ([]string, bool) {
	if len(raw) == 0 {
		badRequest(c, "instructions_required")
		return nil, false
	}
	if len(raw) > 50 {
		badRequest(c, "too_many_instructions")
		return nil, false
	}
	out := make([]string, len(raw))
	for i, s := range raw {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			badRequest(c, "invalid_instruction")
			return nil, false
		}
		out[i] = trimmed
	}
	return out, true
}

func validateNotes(c *gin.Context, raw []string) ([]string, bool) {
	if len(raw) > 10 {
		badRequest(c, "too_many_notes")
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, true
}

func validateCategoryIDs(c *gin.Context, raw []string) ([]uuid.UUID, bool) {
	if len(raw) == 0 {
		badRequest(c, "categories_required")
		return nil, false
	}
	if len(raw) > 3 {
		badRequest(c, "too_many_categories")
		return nil, false
	}
	seen := make(map[uuid.UUID]bool, len(raw))
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			badRequest(c, "invalid_request")
			return nil, false
		}
		if seen[id] {
			badRequest(c, "duplicate_category")
			return nil, false
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, true
}

func validateIngredients(c *gin.Context, raw []createIngredientRequest) ([]models.Ingredient, bool) {
	if len(raw) == 0 {
		badRequest(c, "ingredients_required")
		return nil, false
	}
	if len(raw) > 50 {
		badRequest(c, "too_many_ingredients")
		return nil, false
	}
	seen := make(map[uuid.UUID]bool, len(raw))
	out := make([]models.Ingredient, 0, len(raw))
	for _, ing := range raw {
		itemID, err := uuid.Parse(strings.TrimSpace(ing.ItemID))
		if err != nil {
			badRequest(c, "invalid_request")
			return nil, false
		}
		if seen[itemID] {
			badRequest(c, "duplicate_ingredient")
			return nil, false
		}
		seen[itemID] = true

		quantity, ok := validateQuantity(c, ing.Quantity)
		if !ok {
			return nil, false
		}

		parsed := models.Ingredient{ItemID: itemID, Quantity: quantity}
		if ing.UnitID != nil {
			if trimmed := strings.TrimSpace(*ing.UnitID); trimmed != "" {
				unitID, err := uuid.Parse(trimmed)
				if err != nil {
					badRequest(c, "invalid_request")
					return nil, false
				}
				parsed.UnitID = &unitID
			}
		}
		out = append(out, parsed)
	}
	return out, true
}

// parseRecipeListFilter reads the GET /recipes query into a filter (CallerID/CallerIsAdmin left for the handler); writes the 400 and returns false on bad input.
func parseRecipeListFilter(c *gin.Context) (models.RecipeListFilter, bool) {
	var f models.RecipeListFilter
	f.Query = strings.TrimSpace(c.Query("q"))
	f.Mine = c.Query("mine") == "true"
	f.Seed = strings.TrimSpace(c.Query("seed"))

	categoryIDs, ok := parseUUIDQuery(c, "categoryId")
	if !ok {
		return f, false
	}
	switch c.DefaultQuery("categoryMode", "include") {
	case "include":
		f.IncludeCategoryIDs = categoryIDs
	case "exclude":
		f.ExcludeCategoryIDs = categoryIDs
	default:
		badRequest(c, "invalid_request")
		return f, false
	}

	if f.IngredientIDs, ok = parseUUIDQuery(c, "ingredientId"); !ok {
		return f, false
	}

	minTime, ok := parseIntQuery(c, "minTime", 0)
	if !ok {
		return f, false
	}
	maxTime, ok := parseIntQuery(c, "maxTime", 0)
	if !ok {
		return f, false
	}
	f.MinTime = clampInt(minTime, 0, 1_000_000)
	f.MaxTime = clampInt(maxTime, 0, 1_000_000)

	page, ok := parseIntQuery(c, "page", 1)
	if !ok {
		return f, false
	}
	limit, ok := parseIntQuery(c, "limit", 20)
	if !ok {
		return f, false
	}
	f.Page = clampInt(page, 1, 1_000_000)
	f.Limit = clampInt(limit, 1, 50)

	return f, true
}

func parseUUIDQuery(c *gin.Context, key string) ([]uuid.UUID, bool) {
	raw := c.QueryArray(key)
	out := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		id, err := uuid.Parse(strings.TrimSpace(s))
		if err != nil {
			badRequest(c, "invalid_request")
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
}

func parseIntQuery(c *gin.Context, key string, def int) (int, bool) {
	s := c.Query(key)
	if s == "" {
		return def, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		badRequest(c, "invalid_request")
		return 0, false
	}
	return n, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
