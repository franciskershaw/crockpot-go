package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func (r *PostgresRecipeRepository) GetTimeRange(ctx context.Context) (*models.RecipeTimeRange, error) {
	row, err := queriesFor(ctx, r.db).GetRecipeTimeRange(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get recipe time range: %w", err)
	}
	return &models.RecipeTimeRange{MinTime: int(row.MinTime), MaxTime: int(row.MaxTime)}, nil
}

func (r *PostgresRecipeRepository) List(ctx context.Context, filter models.RecipeListFilter) ([]*models.RecipeCard, int, error) {
	q := queriesFor(ctx, r.db)

	callerID, err := nullableUUIDParam(filter.CallerID)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid caller id: %w", err)
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * filter.Limit

	rows, err := q.ListRecipes(ctx, sqlc.ListRecipesParams{
		CallerIsAdmin:      filter.CallerIsAdmin,
		CallerID:           callerID,
		OnlyMine:           filter.Mine,
		NameQuery:          filter.Query,
		MinTime:            int32(filter.MinTime),
		MaxTime:            int32(filter.MaxTime),
		ExcludeCategoryIds: pgUUIDs(filter.ExcludeCategoryIDs),
		IncludeCategoryIds: pgUUIDs(filter.IncludeCategoryIDs),
		IngredientIds:      pgUUIDs(filter.IngredientIDs),
		ResultLimit:        int32(filter.Limit),
		ResultOffset:       int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list recipes: %w", err)
	}

	total, err := q.CountRecipes(ctx, sqlc.CountRecipesParams{
		CallerIsAdmin:      filter.CallerIsAdmin,
		CallerID:           callerID,
		OnlyMine:           filter.Mine,
		NameQuery:          filter.Query,
		MinTime:            int32(filter.MinTime),
		MaxTime:            int32(filter.MaxTime),
		ExcludeCategoryIds: pgUUIDs(filter.ExcludeCategoryIDs),
		IncludeCategoryIds: pgUUIDs(filter.IncludeCategoryIDs),
		IngredientIds:      pgUUIDs(filter.IngredientIDs),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count recipes: %w", err)
	}

	cards := make([]*models.RecipeCard, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		cards[i] = toRecipeCard(row)
		ids[i] = row.ID
	}

	if err := hydrateCardCategories(ctx, q, cards, ids); err != nil {
		return nil, 0, err
	}

	if callerID.Valid && len(ids) > 0 {
		favIDs, err := q.ListFavouritedRecipeIDs(ctx, sqlc.ListFavouritedRecipeIDsParams{
			UserID:    callerID,
			RecipeIds: ids,
		})
		if err != nil {
			return nil, 0, fmt.Errorf("failed to load favourited recipe ids: %w", err)
		}
		favourited := make(map[uuid.UUID]bool, len(favIDs))
		for _, fid := range favIDs {
			favourited[uuidValue(fid)] = true
		}
		for _, card := range cards {
			card.IsFavourite = favourited[card.ID]
		}
	}

	return cards, int(total), nil
}

// hydrateCardCategories batch-loads categories for ids and assigns them onto the matching cards in place.
func hydrateCardCategories(ctx context.Context, q *sqlc.Queries, cards []*models.RecipeCard, ids []pgtype.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	catRows, err := q.ListRecipeCardCategories(ctx, ids)
	if err != nil {
		return fmt.Errorf("failed to load recipe categories: %w", err)
	}
	byRecipe := make(map[uuid.UUID][]models.CategoryRef, len(ids))
	for _, cr := range catRows {
		rid := uuidValue(cr.RecipeID)
		byRecipe[rid] = append(byRecipe[rid], models.CategoryRef{ID: uuidValue(cr.ID), Name: cr.Name})
	}
	for _, card := range cards {
		if cats := byRecipe[card.ID]; cats != nil {
			card.Categories = cats
		}
	}
	return nil
}

func toRecipeCard(row sqlc.Recipe) *models.RecipeCard {
	return &models.RecipeCard{
		ID:            uuidValue(row.ID),
		Name:          row.Name,
		ImageURL:      textPtr(row.ImageUrl),
		ImageFilename: textPtr(row.ImageFilename),
		TimeInMinutes: int(row.TimeInMinutes),
		Serves:        int(row.Serves),
		Approved:      row.Approved,
		Categories:    []models.CategoryRef{},
		CreatedAt:     row.CreatedAt.Time,
	}
}

func (r *PostgresRecipeRepository) GetByID(ctx context.Context, id string, callerID *string, callerIsAdmin bool) (*models.RecipeDetail, error) {
	q := queriesFor(ctx, r.db)

	recipeID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid recipe id: %w", err)
	}
	cid, err := nullableUUIDParam(callerID)
	if err != nil {
		return nil, fmt.Errorf("invalid caller id: %w", err)
	}

	row, err := q.GetRecipeForReader(ctx, sqlc.GetRecipeForReaderParams{
		ID:            recipeID,
		CallerIsAdmin: callerIsAdmin,
		CallerID:      cid,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrRecipeNotFound
		}
		return nil, fmt.Errorf("failed to get recipe: %w", err)
	}

	catRows, err := q.ListRecipeDetailCategories(ctx, recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipe categories: %w", err)
	}
	categories := make([]models.CategoryRef, len(catRows))
	for i, cr := range catRows {
		categories[i] = models.CategoryRef{ID: uuidValue(cr.ID), Name: cr.Name}
	}

	ingRows, err := q.ListRecipeIngredientsHydrated(ctx, recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipe ingredients: %w", err)
	}
	ingredients := make([]models.HydratedIngredient, len(ingRows))
	for i, ir := range ingRows {
		qty, err := numericValue(ir.Quantity)
		if err != nil {
			return nil, fmt.Errorf("failed to read ingredient quantity: %w", err)
		}
		ing := models.HydratedIngredient{
			ItemID:           uuidValue(ir.ItemID),
			ItemName:         ir.ItemName,
			ItemCategoryID:   uuidValue(ir.ItemCategoryID),
			ItemCategoryName: ir.ItemCategoryName,
			Quantity:         qty,
		}
		if ir.UnitID.Valid {
			u := uuidValue(ir.UnitID)
			ing.UnitID = &u
		}
		if ir.UnitAbbreviation.Valid {
			abbr := ir.UnitAbbreviation.String
			ing.UnitAbbreviation = &abbr
		}
		ingredients[i] = ing
	}

	instructions := row.Instructions
	if instructions == nil {
		instructions = []string{}
	}
	notes := row.Notes
	if notes == nil {
		notes = []string{}
	}

	var isFavourite bool
	if cid.Valid {
		isFavourite, err = q.IsRecipeFavourited(ctx, sqlc.IsRecipeFavouritedParams{
			UserID:   cid,
			RecipeID: recipeID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to check favourite status: %w", err)
		}
	}

	return &models.RecipeDetail{
		RecipeCard: models.RecipeCard{
			ID:            uuidValue(row.ID),
			Name:          row.Name,
			ImageURL:      textPtr(row.ImageUrl),
			ImageFilename: textPtr(row.ImageFilename),
			TimeInMinutes: int(row.TimeInMinutes),
			Serves:        int(row.Serves),
			Approved:      row.Approved,
			Categories:    categories,
			CreatedAt:     row.CreatedAt.Time,
			IsFavourite:   isFavourite,
		},
		Description:   textPtr(row.Description),
		Instructions:  instructions,
		Notes:         notes,
		Ingredients:   ingredients,
		CreatedByID:   uuidValue(row.CreatedByID),
		CreatedByName: textPtr(row.CreatedByName),
		UpdatedAt:     row.UpdatedAt.Time,
	}, nil
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

	return hydrateRecipe(ctx, q, created, input.CategoryIDs)
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

// hydrateRecipe re-reads ingredients for their DB-rounded quantities; categoryIDs are passed through as inserted.
func hydrateRecipe(ctx context.Context, q *sqlc.Queries, row sqlc.Recipe, categoryIDs []uuid.UUID) (*models.Recipe, error) {
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
