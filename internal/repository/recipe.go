package repository

import (
	"context"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

var recipeConstraintErrors = map[string]error{
	"recipe_ingredients_item_id_fkey":            models.ErrRecipeInvalidItem,
	"recipe_ingredients_unit_id_fkey":            models.ErrRecipeInvalidUnit,
	"recipe_ingredients_recipe_id_item_id_key":   models.ErrRecipeDuplicateIngredient,
	"recipe_categories_recipes_category_id_fkey": models.ErrRecipeInvalidCategory,
}

type PostgresRecipeRepository struct {
	db sqlc.DBTX
}

func NewPostgresRecipeRepository(db sqlc.DBTX) *PostgresRecipeRepository {
	return &PostgresRecipeRepository{db: db}
}

func (r *PostgresRecipeRepository) CountByCreator(ctx context.Context, userID string) (int, error) {
	uid, err := uuidParam(userID)
	if err != nil {
		return 0, fmt.Errorf("invalid user id: %w", err)
	}
	count, err := queriesFor(ctx, r.db).CountRecipesByCreator(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("failed to count recipes by creator: %w", err)
	}
	return int(count), nil
}

func (r *PostgresRecipeRepository) Create(ctx context.Context, input models.CreateRecipeInput) (*models.Recipe, error) {
	q := queriesFor(ctx, r.db)

	if err := checkAllowedUnits(ctx, q, input.Ingredients); err != nil {
		return nil, err
	}

	created, err := q.CreateRecipe(ctx, sqlc.CreateRecipeParams{
		Name:          input.Name,
		TimeInMinutes: int32(input.TimeInMinutes),
		Serves:        int32(input.Serves),
		Instructions:  input.Instructions,
		Notes:         input.Notes,
		ImageUrl:      textPtrParam(input.ImageURL),
		ImageFilename: textPtrParam(input.ImageFilename),
		Approved:      input.Approved,
		CreatedByID:   pgUUID(input.CreatedByID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create recipe: %w", err)
	}

	for i, ing := range input.Ingredients {
		qty, err := numericParam(ing.Quantity)
		if err != nil {
			return nil, fmt.Errorf("invalid quantity: %w", err)
		}
		params := sqlc.CreateRecipeIngredientParams{
			RecipeID: created.ID,
			ItemID:   pgUUID(ing.ItemID),
			Quantity: qty,
			Position: int16(i),
		}
		if ing.UnitID != nil {
			params.UnitID = pgUUID(*ing.UnitID)
		}
		if err := q.CreateRecipeIngredient(ctx, params); err != nil {
			if mapped := pgConstraintError(err, recipeConstraintErrors); mapped != nil {
				return nil, mapped
			}
			return nil, fmt.Errorf("failed to add recipe ingredient: %w", err)
		}
	}

	for _, catID := range input.CategoryIDs {
		if err := q.CreateRecipeCategoryLink(ctx, sqlc.CreateRecipeCategoryLinkParams{
			RecipeID:   created.ID,
			CategoryID: pgUUID(catID),
		}); err != nil {
			if mapped := pgConstraintError(err, recipeConstraintErrors); mapped != nil {
				return nil, mapped
			}
			return nil, fmt.Errorf("failed to link recipe category: %w", err)
		}
	}

	return hydrateRecipe(ctx, q, created)
}

// checkAllowedUnits rejects an ingredient unit absent from its item's allowed set; an empty set means unconstrained, a nil unit always passes.
func checkAllowedUnits(ctx context.Context, q *sqlc.Queries, ingredients []models.Ingredient) error {
	itemIDs := make([]pgtype.UUID, 0, len(ingredients))
	for _, ing := range ingredients {
		itemIDs = append(itemIDs, pgUUID(ing.ItemID))
	}
	rows, err := q.ListItemAllowedUnitIDsForItems(ctx, itemIDs)
	if err != nil {
		return fmt.Errorf("failed to load item allowed units: %w", err)
	}

	allowed := make(map[uuid.UUID]map[uuid.UUID]bool)
	for _, row := range rows {
		itemID := uuidValue(row.ItemID)
		if allowed[itemID] == nil {
			allowed[itemID] = make(map[uuid.UUID]bool)
		}
		allowed[itemID][uuidValue(row.UnitID)] = true
	}

	for _, ing := range ingredients {
		if ing.UnitID == nil {
			continue
		}
		set := allowed[ing.ItemID]
		if len(set) == 0 {
			continue
		}
		if !set[*ing.UnitID] {
			return models.ErrIngredientUnitNotAllowed
		}
	}
	return nil
}

func hydrateRecipe(ctx context.Context, q *sqlc.Queries, row sqlc.Recipe) (*models.Recipe, error) {
	ingRows, err := q.ListRecipeIngredients(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list recipe ingredients: %w", err)
	}
	ingredients := make([]models.Ingredient, len(ingRows))
	for i, ir := range ingRows {
		qty, err := numericValue(ir.Quantity)
		if err != nil {
			return nil, fmt.Errorf("failed to read ingredient quantity: %w", err)
		}
		ing := models.Ingredient{ItemID: uuidValue(ir.ItemID), Quantity: qty}
		if ir.UnitID.Valid {
			u := uuidValue(ir.UnitID)
			ing.UnitID = &u
		}
		ingredients[i] = ing
	}

	catRows, err := q.ListRecipeCategoryIDsForRecipe(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list recipe categories: %w", err)
	}
	categoryIDs := make([]uuid.UUID, len(catRows))
	for i, c := range catRows {
		categoryIDs[i] = uuidValue(c)
	}

	return toModelRecipe(row, categoryIDs, ingredients), nil
}

func toModelRecipe(row sqlc.Recipe, categoryIDs []uuid.UUID, ingredients []models.Ingredient) *models.Recipe {
	instructions := row.Instructions
	if instructions == nil {
		instructions = []string{}
	}
	notes := row.Notes
	if notes == nil {
		notes = []string{}
	}
	return &models.Recipe{
		ID:            uuidValue(row.ID),
		Name:          row.Name,
		TimeInMinutes: int(row.TimeInMinutes),
		Serves:        int(row.Serves),
		Instructions:  instructions,
		Notes:         notes,
		ImageURL:      textPtr(row.ImageUrl),
		ImageFilename: textPtr(row.ImageFilename),
		Approved:      row.Approved,
		CategoryIDs:   categoryIDs,
		Ingredients:   ingredients,
		CreatedByID:   uuidValue(row.CreatedByID),
		CreatedByName: textPtr(row.CreatedByName),
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
}
