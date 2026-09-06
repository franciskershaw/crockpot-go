package repository

import (
	"context"
	"errors"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
)

type PostgresMenuRepository struct {
	db sqlc.DBTX
}

func NewPostgresMenuRepository(db sqlc.DBTX) *PostgresMenuRepository {
	return &PostgresMenuRepository{db: db}
}

func (r *PostgresMenuRepository) GetMenu(ctx context.Context, userID string) (*models.Menu, error) {
	return nil, errors.New("not implemented")
}

func (r *PostgresMenuRepository) UpsertEntry(ctx context.Context, userID, recipeID string, serves int, callerIsAdmin bool) error {
	return errors.New("not implemented")
}

func (r *PostgresMenuRepository) UpdateEntryServes(ctx context.Context, userID, recipeID string, serves int) error {
	return errors.New("not implemented")
}

func (r *PostgresMenuRepository) RemoveEntry(ctx context.Context, userID, recipeID string) error {
	return errors.New("not implemented")
}
