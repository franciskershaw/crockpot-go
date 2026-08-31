package repository

import (
	"context"
	"errors"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
)

type PostgresRecipeRepository struct {
	db sqlc.DBTX
}

func NewPostgresRecipeRepository(db sqlc.DBTX) *PostgresRecipeRepository {
	return &PostgresRecipeRepository{db: db}
}

func (r *PostgresRecipeRepository) Create(ctx context.Context, input models.CreateRecipeInput) (*models.Recipe, error) {
	return nil, errors.New("STUB_PostgresRecipeRepository_Create_NOT_IMPLEMENTED")
}

func (r *PostgresRecipeRepository) CountByCreator(ctx context.Context, userID string) (int, error) {
	return -999, errors.New("STUB_PostgresRecipeRepository_CountByCreator_NOT_IMPLEMENTED")
}
