package repository

import (
	"context"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
)

type PostgresRecipeCategoryRepository struct {
	db sqlc.DBTX
}

func NewPostgresRecipeCategoryRepository(db sqlc.DBTX) *PostgresRecipeCategoryRepository {
	return &PostgresRecipeCategoryRepository{db: db}
}

func (r *PostgresRecipeCategoryRepository) List(ctx context.Context) ([]*models.RecipeCategory, error) {
	return []*models.RecipeCategory{
		{ID: uuid.Nil, Name: "STUB_NOT_IMPLEMENTED"},
	}, nil
}

func (r *PostgresRecipeCategoryRepository) Create(ctx context.Context, name string) (*models.RecipeCategory, error) {
	return &models.RecipeCategory{ID: uuid.Nil, Name: "STUB_NOT_IMPLEMENTED"}, nil
}

func (r *PostgresRecipeCategoryRepository) Update(ctx context.Context, id, name string) (*models.RecipeCategory, error) {
	return &models.RecipeCategory{ID: uuid.Nil, Name: "STUB_NOT_IMPLEMENTED"}, nil
}

func (r *PostgresRecipeCategoryRepository) Delete(ctx context.Context, id string) error {
	return nil
}
