package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

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

	hasScoreSignal := len(filter.IngredientIDs) > 0 || len(filter.IncludeCategoryIDs) > 0
	isUnfiltered := !hasScoreSignal &&
		len(filter.ExcludeCategoryIDs) == 0 &&
		filter.Query == "" &&
		filter.MinTime == 0 &&
		filter.MaxTime == 0 &&
		!filter.Mine &&
		!filter.PendingOnly

	rows, err := q.ListRecipes(ctx, sqlc.ListRecipesParams{
		CallerIsAdmin:      filter.CallerIsAdmin,
		CallerID:           callerID,
		OnlyMine:           filter.Mine,
		OnlyPending:        filter.PendingOnly,
		NameQuery:          filter.Query,
		MinTime:            int32(filter.MinTime),
		MaxTime:            int32(filter.MaxTime),
		ExcludeCategoryIds: pgUUIDs(filter.ExcludeCategoryIDs),
		IncludeCategoryIds: pgUUIDs(filter.IncludeCategoryIDs),
		IngredientIds:      pgUUIDs(filter.IngredientIDs),
		HasScoreSignal:     hasScoreSignal,
		IsUnfiltered:       isUnfiltered,
		Seed:               filter.Seed,
		ResultLimit:        int32(filter.Limit),
		ResultOffset:       int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list recipes: %w", err)
	}

	total := 0
	if len(rows) > 0 {
		total = int(rows[0].Total)
	}

	cards := make([]*models.RecipeCard, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		card := &models.RecipeCard{
			ID:                     uuidValue(row.ID),
			Name:                   row.Name,
			ImageURL:               textPtr(row.ImageUrl),
			ImageFilename:          textPtr(row.ImageFilename),
			TimeInMinutes:          int(row.TimeInMinutes),
			Serves:                 int(row.Serves),
			Approved:               row.Approved,
			Categories:             []models.CategoryRef{},
			CreatedAt:              row.CreatedAt.Time,
			TotalIngredientCount:   int(row.TotalIngredientCount),
			MatchedIngredientCount: int(row.MatchedIngredientCount),
			MatchedCategoryCount:   int(row.MatchedCategoryCount),
			Score:                  row.Score,
			Tier:                   coverageTier(int(row.MatchedIngredientCount), int(row.TotalIngredientCount), len(filter.IngredientIDs)),
		}
		cards[i] = card
		ids[i] = row.ID
	}

	if err := hydrateCardCategories(ctx, q, cards, ids); err != nil {
		return nil, 0, err
	}

	if err := markFavourites(ctx, q, callerID, cards, ids); err != nil {
		return nil, 0, err
	}

	return cards, int(total), nil
}

// markFavourites sets IsFavourite on each card the caller has favourited; a null callerID leaves every card false.
func markFavourites(ctx context.Context, q *sqlc.Queries, callerID pgtype.UUID, cards []*models.RecipeCard, ids []pgtype.UUID) error {
	if !callerID.Valid || len(ids) == 0 {
		return nil
	}
	favIDs, err := q.ListFavouritedRecipeIDs(ctx, sqlc.ListFavouritedRecipeIDsParams{
		UserID:    callerID,
		RecipeIds: ids,
	})
	if err != nil {
		return fmt.Errorf("failed to load favourited recipe ids: %w", err)
	}
	favourited := make(map[uuid.UUID]bool, len(favIDs))
	for _, fid := range favIDs {
		favourited[uuidValue(fid)] = true
	}
	for _, card := range cards {
		card.IsFavourite = favourited[card.ID]
	}
	return nil
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

const (
	bestCoverage = 0.55
	goodCoverage = 0.25
)

// coverageTier rates how much of the recipe the caller already has; categories never earn a tier on their own.
func coverageTier(matched, total, selected int) *string {
	if selected == 0 || total == 0 {
		return nil
	}
	coverage := float64(matched) / float64(total)
	var tier string
	switch {
	case coverage >= bestCoverage:
		tier = "best"
	case coverage >= goodCoverage:
		tier = "good"
	default:
		return nil
	}
	return &tier
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

	return buildRecipeDetail(ctx, q, row, cid)
}

// buildRecipeDetail hydrates a recipe row (categories, ingredients, favourite status) into the response shape shared by GetByID, Create, and Update.
func buildRecipeDetail(ctx context.Context, q *sqlc.Queries, row sqlc.Recipe, cid pgtype.UUID) (*models.RecipeDetail, error) {
	catRows, err := q.ListRecipeDetailCategories(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipe categories: %w", err)
	}
	categories := make([]models.CategoryRef, len(catRows))
	for i, cr := range catRows {
		categories[i] = models.CategoryRef{ID: uuidValue(cr.ID), Name: cr.Name}
	}

	ingRows, err := q.ListRecipeIngredientsHydrated(ctx, row.ID)
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
			RecipeID: row.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to check favourite status: %w", err)
		}
	}

	var creatorID *uuid.UUID
	var createdByName *string
	if row.CreatedByID.Valid {
		id := uuidValue(row.CreatedByID)
		creatorID = &id
		name, err := q.GetUserName(ctx, row.CreatedByID)
		switch {
		case err == nil:
			createdByName = textPtr(name)
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("failed to load recipe creator name: %w", err)
		}
	}

	return &models.RecipeDetail{
		RecipeCard: models.RecipeCard{
			ID:                   uuidValue(row.ID),
			Name:                 row.Name,
			ImageURL:             textPtr(row.ImageUrl),
			ImageFilename:        textPtr(row.ImageFilename),
			TimeInMinutes:        int(row.TimeInMinutes),
			Serves:               int(row.Serves),
			Approved:             row.Approved,
			Categories:           categories,
			CreatedAt:            row.CreatedAt.Time,
			IsFavourite:          isFavourite,
			TotalIngredientCount: len(ingredients),
		},
		Description:   textPtr(row.Description),
		Instructions:  instructions,
		Notes:         notes,
		Ingredients:   ingredients,
		CreatedByID:   creatorID,
		CreatedByName: createdByName,
		UpdatedAt:     row.UpdatedAt.Time,
	}, nil
}

func (r *PostgresRecipeRepository) Create(ctx context.Context, input models.CreateRecipeInput) (*models.RecipeDetail, error) {
	q := queriesFor(ctx, r.db)

	if err := checkAllowedUnits(ctx, q, input.Ingredients); err != nil {
		return nil, err
	}
	if err := checkIngredientItems(ctx, q, input.Ingredients); err != nil {
		return nil, err
	}

	created, err := q.CreateRecipe(ctx, sqlc.CreateRecipeParams{
		Name:          input.Name,
		Description:   textPtrParam(input.Description),
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

	if err := insertRecipeIngredients(ctx, q, created.ID, input.Ingredients); err != nil {
		return nil, err
	}
	if err := linkRecipeCategories(ctx, q, created.ID, input.CategoryIDs); err != nil {
		return nil, err
	}

	return buildRecipeDetail(ctx, q, created, pgUUID(input.CreatedByID))
}

// Update returns the old image's public id when the update dropped it and no other recipe uses it, for the caller to destroy after commit.
func (r *PostgresRecipeRepository) Update(ctx context.Context, id string, input models.CreateRecipeInput, callerID string, callerIsAdmin bool) (*models.RecipeDetail, *string, error) {
	q := queriesFor(ctx, r.db)

	existing, cid, err := lockRecipeForWrite(ctx, q, id, callerID)
	if err != nil {
		return nil, nil, err
	}
	recipeID := existing.ID
	if err := recipeWriteError(existing, cid, callerIsAdmin); err != nil {
		return nil, nil, err
	}

	if err := checkAllowedUnits(ctx, q, input.Ingredients); err != nil {
		return nil, nil, err
	}
	if err := checkIngredientItems(ctx, q, input.Ingredients); err != nil {
		return nil, nil, err
	}

	imageURL, imageFilename := existing.ImageUrl, existing.ImageFilename
	if input.Image != models.ImageKeep {
		imageURL, imageFilename = textPtrParam(input.ImageURL), textPtrParam(input.ImageFilename)
	}

	updated, err := q.UpdateRecipe(ctx, sqlc.UpdateRecipeParams{
		ID:            recipeID,
		Name:          input.Name,
		Description:   textPtrParam(input.Description),
		TimeInMinutes: int32(input.TimeInMinutes),
		Serves:        int32(input.Serves),
		Instructions:  input.Instructions,
		Notes:         input.Notes,
		ImageUrl:      imageURL,
		ImageFilename: imageFilename,
		Approved:      existing.Approved,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to update recipe: %w", err)
	}

	if err := q.DeleteRecipeIngredients(ctx, recipeID); err != nil {
		return nil, nil, fmt.Errorf("failed to clear recipe ingredients: %w", err)
	}
	if err := q.DeleteRecipeCategoryLinks(ctx, recipeID); err != nil {
		return nil, nil, fmt.Errorf("failed to clear recipe categories: %w", err)
	}

	if err := insertRecipeIngredients(ctx, q, recipeID, input.Ingredients); err != nil {
		return nil, nil, err
	}
	if err := linkRecipeCategories(ctx, q, recipeID, input.CategoryIDs); err != nil {
		return nil, nil, err
	}

	detail, err := buildRecipeDetail(ctx, q, updated, cid)
	if err != nil {
		return nil, nil, err
	}
	if !existing.ImageFilename.Valid || existing.ImageFilename == imageFilename {
		return detail, nil, nil
	}
	orphan, err := orphanedImage(ctx, q, existing.ImageFilename, recipeID)
	if err != nil {
		return nil, nil, err
	}
	return detail, orphan, nil
}

// insertRecipeIngredients creates one recipe_ingredients row per ingredient, preserving submit order via Position.
func insertRecipeIngredients(ctx context.Context, q *sqlc.Queries, recipeID pgtype.UUID, ingredients []models.Ingredient) error {
	for i, ing := range ingredients {
		qty, err := numericParam(ing.Quantity)
		if err != nil {
			return fmt.Errorf("invalid quantity: %w", err)
		}
		params := sqlc.CreateRecipeIngredientParams{
			RecipeID: recipeID,
			ItemID:   pgUUID(ing.ItemID),
			Quantity: qty,
			Position: int16(i),
		}
		if ing.UnitID != nil {
			params.UnitID = pgUUID(*ing.UnitID)
		}
		if err := q.CreateRecipeIngredient(ctx, params); err != nil {
			if mapped := pgConstraintError(err, recipeConstraintErrors); mapped != nil {
				return mapped
			}
			return fmt.Errorf("failed to add recipe ingredient: %w", err)
		}
	}
	return nil
}

// linkRecipeCategories creates one recipe_categories_recipes row per category ID.
func linkRecipeCategories(ctx context.Context, q *sqlc.Queries, recipeID pgtype.UUID, categoryIDs []uuid.UUID) error {
	for _, catID := range categoryIDs {
		if err := q.CreateRecipeCategoryLink(ctx, sqlc.CreateRecipeCategoryLinkParams{
			RecipeID:   recipeID,
			CategoryID: pgUUID(catID),
		}); err != nil {
			if mapped := pgConstraintError(err, recipeConstraintErrors); mapped != nil {
				return mapped
			}
			return fmt.Errorf("failed to link recipe category: %w", err)
		}
	}
	return nil
}

// Delete returns the users whose menus held the recipe, and its image's public id when no other recipe uses it.
func (r *PostgresRecipeRepository) Delete(ctx context.Context, id string, callerID string, callerIsAdmin bool) ([]string, *string, error) {
	q := queriesFor(ctx, r.db)

	existing, cid, err := lockRecipeForWrite(ctx, q, id, callerID)
	if err != nil {
		return nil, nil, err
	}
	recipeID := existing.ID
	if err := recipeWriteError(existing, cid, callerIsAdmin); err != nil {
		return nil, nil, err
	}

	return deleteRecipeRow(ctx, q, recipeID, existing.ImageFilename)
}

// Approve approves a pending recipe if it hasn't changed since seenUpdatedAt; an approved one is returned unchanged.
func (r *PostgresRecipeRepository) Approve(ctx context.Context, id string, callerID string, seenUpdatedAt time.Time) (*models.RecipeDetail, error) {
	q := queriesFor(ctx, r.db)

	existing, cid, err := lockRecipeForWrite(ctx, q, id, callerID)
	if err != nil {
		return nil, err
	}
	recipeID := existing.ID

	if existing.Approved {
		row, err := q.GetRecipeForReader(ctx, sqlc.GetRecipeForReaderParams{
			ID:            recipeID,
			CallerIsAdmin: true,
			CallerID:      cid,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get recipe: %w", err)
		}
		return buildRecipeDetail(ctx, q, row, cid)
	}
	if !existing.UpdatedAt.Time.Equal(seenUpdatedAt) {
		return nil, models.ErrRecipeChanged
	}

	approved, err := q.ApproveRecipe(ctx, recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to approve recipe: %w", err)
	}
	return buildRecipeDetail(ctx, q, approved, cid)
}

// deleteRecipeRow deletes a recipe the caller has already locked, returning the users whose menus held it and its photo if no other recipe uses it.
func deleteRecipeRow(ctx context.Context, q *sqlc.Queries, recipeID pgtype.UUID, imageFilename pgtype.Text) ([]string, *string, error) {
	// Read before the delete: the cascade removes the menu entries that say who held it.
	menuUserIDs, err := q.ListMenuUserIDsForRecipe(ctx, recipeID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list menus holding recipe: %w", err)
	}

	if err := q.DeleteRecipe(ctx, recipeID); err != nil {
		return nil, nil, fmt.Errorf("failed to delete recipe: %w", err)
	}
	var orphan *string
	if imageFilename.Valid {
		if orphan, err = orphanedImage(ctx, q, imageFilename, recipeID); err != nil {
			return nil, nil, err
		}
	}
	return uuidStrings(menuUserIDs), orphan, nil
}

// DeleteUnapprovedByCreator deletes every unapproved recipe creatorID made, returning the users whose menus held one and the photos no recipe uses any more.
func (r *PostgresRecipeRepository) DeleteUnapprovedByCreator(ctx context.Context, creatorID string) (menuUserIDs []string, orphanedImages []string, err error) {
	q := queriesFor(ctx, r.db)

	cid, err := uuidParam(creatorID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid creator id: %w", err)
	}
	drafts, err := q.ListUnapprovedRecipesForWriteByCreator(ctx, cid)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list unapproved recipes: %w", err)
	}

	holders := map[string]struct{}{}
	for _, d := range drafts {
		ids, orphan, err := deleteRecipeRow(ctx, q, d.ID, d.ImageFilename)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range ids {
			holders[id] = struct{}{}
		}
		if orphan != nil {
			orphanedImages = append(orphanedImages, *orphan)
		}
	}

	for id := range holders {
		menuUserIDs = append(menuUserIDs, id)
	}
	sort.Strings(menuUserIDs)
	return menuUserIDs, orphanedImages, nil
}

// orphanedImage returns publicID when no recipe other than recipeID still uses it.
func orphanedImage(ctx context.Context, q *sqlc.Queries, publicID pgtype.Text, recipeID pgtype.UUID) (*string, error) {
	if err := q.LockImage(ctx, publicID.String); err != nil {
		return nil, fmt.Errorf("failed to lock image: %w", err)
	}
	n, err := q.CountRecipesUsingImage(ctx, sqlc.CountRecipesUsingImageParams{ImageFilename: publicID, ExcludeID: recipeID})
	if err != nil {
		return nil, fmt.Errorf("failed to count recipes using image: %w", err)
	}
	if n > 0 {
		return nil, nil
	}
	return textPtr(publicID), nil
}

// ImageInUse reports whether any recipe still points at publicID.
func (r *PostgresRecipeRepository) ImageInUse(ctx context.Context, publicID string) (bool, error) {
	n, err := queriesFor(ctx, r.db).CountRecipesWithImage(ctx, pgtype.Text{String: publicID, Valid: true})
	if err != nil {
		return false, fmt.Errorf("failed to count recipes with image: %w", err)
	}
	return n > 0, nil
}

// CheckWritable applies the write rule outside a transaction, so a refused photo save never uploads; Update and Delete re-check under the row lock.
func (r *PostgresRecipeRepository) CheckWritable(ctx context.Context, id string, callerID string, callerIsAdmin bool) error {
	q := queriesFor(ctx, r.db)

	existing, cid, err := lockRecipeForWrite(ctx, q, id, callerID)
	if err != nil {
		return err
	}
	return recipeWriteError(existing, cid, callerIsAdmin)
}

func (r *PostgresRecipeRepository) MenuUserIDs(ctx context.Context, id string) ([]string, error) {
	recipeID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid recipe id: %w", err)
	}
	ids, err := queriesFor(ctx, r.db).ListMenuUserIDsForRecipe(ctx, recipeID)
	if err != nil {
		return nil, fmt.Errorf("failed to list menus holding recipe: %w", err)
	}
	return uuidStrings(ids), nil
}

// lockRecipeForWrite parses the ids and row-locks the recipe until the transaction ends, mapping a missing one to ErrRecipeNotFound.
func lockRecipeForWrite(ctx context.Context, q *sqlc.Queries, id, callerID string) (sqlc.GetRecipeForWriteRow, pgtype.UUID, error) {
	recipeID, err := uuidParam(id)
	if err != nil {
		return sqlc.GetRecipeForWriteRow{}, pgtype.UUID{}, fmt.Errorf("invalid recipe id: %w", err)
	}
	cid, err := uuidParam(callerID)
	if err != nil {
		return sqlc.GetRecipeForWriteRow{}, pgtype.UUID{}, fmt.Errorf("invalid caller id: %w", err)
	}
	existing, err := q.GetRecipeForWrite(ctx, recipeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.GetRecipeForWriteRow{}, pgtype.UUID{}, models.ErrRecipeNotFound
		}
		return sqlc.GetRecipeForWriteRow{}, pgtype.UUID{}, fmt.Errorf("failed to get recipe: %w", err)
	}
	return existing, cid, nil
}

// recipeWriteError reports why callerID may not update/delete a recipe, or nil: admins may write any recipe, owners only while it's pending.
func recipeWriteError(existing sqlc.GetRecipeForWriteRow, callerID pgtype.UUID, callerIsAdmin bool) error {
	switch {
	case callerIsAdmin:
		return nil
	case existing.CreatedByID != callerID && !existing.Approved:
		return models.ErrRecipeNotFound
	case existing.CreatedByID != callerID:
		return models.ErrRecipeForbidden
	case existing.Approved:
		return models.ErrRecipeApprovedLocked
	}
	return nil
}

// checkIngredientItems rejects an ingredient whose item's category isn't a recipe ingredient (e.g. House). Recipe-only: the shopping list shares checkAllowedUnits, not this.
func checkIngredientItems(ctx context.Context, q *sqlc.Queries, ingredients []models.Ingredient) error {
	itemIDs := make([]pgtype.UUID, 0, len(ingredients))
	for _, ing := range ingredients {
		itemIDs = append(itemIDs, pgUUID(ing.ItemID))
	}
	rejected, err := q.ListNonIngredientItemIDs(ctx, itemIDs)
	if err != nil {
		return fmt.Errorf("failed to check ingredient items: %w", err)
	}
	if len(rejected) > 0 {
		return models.ErrRecipeNonIngredientItem
	}
	return nil
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
