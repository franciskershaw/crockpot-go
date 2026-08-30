package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
)

var recipeCategoryConstraintErrors = map[string]error{
	"recipe_categories_name_key": models.ErrRecipeCategoryNameTaken,
}

type PostgresRecipeCategoryRepository struct {
	db sqlc.DBTX
}

func NewPostgresRecipeCategoryRepository(db sqlc.DBTX) *PostgresRecipeCategoryRepository {
	return &PostgresRecipeCategoryRepository{db: db}
}

func (r *PostgresRecipeCategoryRepository) List(ctx context.Context) ([]*models.RecipeCategory, error) {
	rows, err := queriesFor(ctx, r.db).ListRecipeCategories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list recipe categories: %w", err)
	}
	categories := make([]*models.RecipeCategory, len(rows))
	for i, row := range rows {
		categories[i] = toModelRecipeCategory(row)
	}
	return categories, nil
}

func (r *PostgresRecipeCategoryRepository) Create(ctx context.Context, name string) (*models.RecipeCategory, error) {
	created, err := queriesFor(ctx, r.db).CreateRecipeCategory(ctx, name)
	if err != nil {
		if constraintErr := pgConstraintError(err, recipeCategoryConstraintErrors); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to create recipe category: %w", err)
	}
	return toModelRecipeCategory(created), nil
}

func (r *PostgresRecipeCategoryRepository) Update(ctx context.Context, id, name string) (*models.RecipeCategory, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid recipe category id: %w", err)
	}
	updated, err := queriesFor(ctx, r.db).UpdateRecipeCategory(ctx, sqlc.UpdateRecipeCategoryParams{
		Name: name,
		ID:   idUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrRecipeCategoryNotFound
		}
		if constraintErr := pgConstraintError(err, recipeCategoryConstraintErrors); constraintErr != nil {
			return nil, constraintErr
		}
		return nil, fmt.Errorf("failed to update recipe category: %w", err)
	}
	return toModelRecipeCategory(updated), nil
}

func (r *PostgresRecipeCategoryRepository) Delete(ctx context.Context, id string) error {
	idUUID, err := uuidParam(id)
	if err != nil {
		return fmt.Errorf("invalid recipe category id: %w", err)
	}
	_, err = queriesFor(ctx, r.db).DeleteRecipeCategory(ctx, idUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.ErrRecipeCategoryNotFound
		}
		// recipe_categories_recipes.category_id is ON DELETE RESTRICT, so this is RestrictViolation, not ForeignKeyViolation.
		if pgErrorCode(err, pgerrcode.RestrictViolation) {
			return models.ErrRecipeCategoryInUse
		}
		return fmt.Errorf("failed to delete recipe category: %w", err)
	}
	return nil
}

func toModelRecipeCategory(c sqlc.RecipeCategory) *models.RecipeCategory {
	return &models.RecipeCategory{
		ID:        uuidValue(c.ID),
		Name:      c.Name,
		CreatedAt: c.CreatedAt.Time,
		UpdatedAt: c.UpdatedAt.Time,
	}
}
