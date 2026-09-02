package repository

import (
	"context"
	"errors"

	"github.com/franciskershaw/crockpot-go/internal/models"
)

// Red-stage stubs — replace bodies per the handoff doc's Data layer shape section.

func (r *PostgresRecipeRepository) AddFavourite(ctx context.Context, userID, recipeID string, callerIsAdmin bool) error {
	return errors.New("STUB: AddFavourite not implemented")
}

func (r *PostgresRecipeRepository) RemoveFavourite(ctx context.Context, userID, recipeID string) error {
	return errors.New("STUB: RemoveFavourite not implemented")
}

func (r *PostgresRecipeRepository) ListFavourites(ctx context.Context, userID string, page, limit int) ([]*models.RecipeCard, int, error) {
	return []*models.RecipeCard{}, -999, errors.New("STUB: ListFavourites not implemented")
}
