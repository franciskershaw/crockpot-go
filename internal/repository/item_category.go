package repository

import (
	"context"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
)

type PostgresItemCategoryRepository struct {
	db sqlc.DBTX
}

func NewPostgresItemCategoryRepository(db sqlc.DBTX) *PostgresItemCategoryRepository {
	return &PostgresItemCategoryRepository{db: db}
}

var stubItemCategorySentinelID = uuid.MustParse("deadbeef-dead-beef-dead-beefdeadbeef")

func stubItemCategory() *models.ItemCategory {
	return &models.ItemCategory{
		ID:        stubItemCategorySentinelID,
		Name:      "STUB_NOT_IMPLEMENTED",
		Icon:      "STUB_NOT_IMPLEMENTED",
		CreatedAt: time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (r *PostgresItemCategoryRepository) List(ctx context.Context) ([]*models.ItemCategory, error) {
	return nil, nil
}

func (r *PostgresItemCategoryRepository) Create(ctx context.Context, name, icon string) (*models.ItemCategory, error) {
	return stubItemCategory(), nil
}

func (r *PostgresItemCategoryRepository) Update(ctx context.Context, id, name, icon string) (*models.ItemCategory, error) {
	return stubItemCategory(), nil
}

func (r *PostgresItemCategoryRepository) Delete(ctx context.Context, id string) error {
	return nil
}
