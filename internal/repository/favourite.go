package repository

import (
	"context"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (r *PostgresRecipeRepository) AddFavourite(ctx context.Context, userID, recipeID string, callerIsAdmin bool) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(recipeID)
	if err != nil {
		return fmt.Errorf("invalid recipe id: %w", err)
	}

	visible, err := q.RecipeVisibleToCaller(ctx, sqlc.RecipeVisibleToCallerParams{
		ID:            rid,
		CallerIsAdmin: callerIsAdmin,
		CallerID:      uid,
	})
	if err != nil {
		return fmt.Errorf("failed to check recipe visibility: %w", err)
	}
	if !visible {
		return models.ErrRecipeNotFound
	}

	if err := q.AddFavourite(ctx, sqlc.AddFavouriteParams{
		UserID:   uid,
		RecipeID: rid,
	}); err != nil {
		return fmt.Errorf("failed to add favourite: %w", err)
	}
	return nil
}

func (r *PostgresRecipeRepository) RemoveFavourite(ctx context.Context, userID, recipeID string) error {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	rid, err := uuidParam(recipeID)
	if err != nil {
		return fmt.Errorf("invalid recipe id: %w", err)
	}

	if err := q.RemoveFavourite(ctx, sqlc.RemoveFavouriteParams{
		UserID:   uid,
		RecipeID: rid,
	}); err != nil {
		return fmt.Errorf("failed to remove favourite: %w", err)
	}
	return nil
}

func (r *PostgresRecipeRepository) ListFavourites(ctx context.Context, userID string, page, limit int) ([]*models.RecipeCard, int, error) {
	q := queriesFor(ctx, r.db)

	uid, err := uuidParam(userID)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid user id: %w", err)
	}

	if page < 1 {
		page = 1
	}
	offset := (page - 1) * limit

	rows, err := q.ListFavouriteRecipes(ctx, sqlc.ListFavouriteRecipesParams{
		UserID: uid,
		Limit:  int32(limit),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list favourite recipes: %w", err)
	}

	total, err := q.CountFavouriteRecipes(ctx, uid)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count favourite recipes: %w", err)
	}

	cards := make([]*models.RecipeCard, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		card := toRecipeCard(row)
		card.IsFavourite = true
		cards[i] = card
		ids[i] = row.ID
	}

	if len(ids) > 0 {
		catRows, err := q.ListRecipeCardCategories(ctx, ids)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to load recipe categories: %w", err)
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
	}

	return cards, int(total), nil
}
