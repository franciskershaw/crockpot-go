package handler

import (
	"net/url"
	"strings"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

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

// parseCreateRecipeInput validates the body into a CreateRecipeInput (CreatedByID/Approved left for the handler); writes the error response and returns false on failure.
func parseCreateRecipeInput(c *gin.Context) (models.CreateRecipeInput, bool) {
	var req createRecipeRequest
	if !bindJSON(c, &req) {
		return models.CreateRecipeInput{}, false
	}

	name, ok := validateRecipeName(c, req.Name)
	if !ok {
		return models.CreateRecipeInput{}, false
	}
	if req.TimeInMinutes < 1 || req.TimeInMinutes > 1440 {
		badRequest(c, "invalid_time")
		return models.CreateRecipeInput{}, false
	}
	if req.Serves < 1 || req.Serves > 50 {
		badRequest(c, "invalid_serves")
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
	imageURL, imageFilename, ok := validateRecipeImage(c, req.Image)
	if !ok {
		return models.CreateRecipeInput{}, false
	}

	return models.CreateRecipeInput{
		Name:          name,
		TimeInMinutes: req.TimeInMinutes,
		Serves:        req.Serves,
		Instructions:  instructions,
		Notes:         notes,
		CategoryIDs:   categoryIDs,
		Ingredients:   ingredients,
		ImageURL:      imageURL,
		ImageFilename: imageFilename,
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

		if ing.Quantity == nil || *ing.Quantity <= 0 {
			badRequest(c, "invalid_quantity")
			return nil, false
		}

		parsed := models.Ingredient{ItemID: itemID, Quantity: *ing.Quantity}
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

func validateRecipeImage(c *gin.Context, img *recipeImageRequest) (*string, *string, bool) {
	if img == nil {
		return nil, nil, true
	}
	imageURL := strings.TrimSpace(img.URL)
	filename := strings.TrimSpace(img.Filename)
	if imageURL == "" || filename == "" {
		badRequest(c, "invalid_image")
		return nil, nil, false
	}
	parsed, err := url.Parse(imageURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "res.cloudinary.com" {
		badRequest(c, "invalid_image")
		return nil, nil, false
	}
	return &imageURL, &filename, true
}
