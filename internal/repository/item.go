package repository

import (
	"context"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
)

type PostgresItemRepository struct {
	db sqlc.DBTX
}

func NewPostgresItemRepository(db sqlc.DBTX) *PostgresItemRepository {
	return &PostgresItemRepository{db: db}
}

var (
	stubItemSentinelID     = uuid.MustParse("baadf00d-baad-f00d-baad-f00dbaadf00d")
	stubItemSentinelCatID  = uuid.MustParse("cafebabe-cafe-babe-cafe-babecafebabe")
	stubItemSentinelUnitID = uuid.MustParse("0ddba11f-0ddb-a11f-0ddb-a11f0ddba11f")
)

func stubItem() *models.Item {
	return &models.Item{
		ID:             stubItemSentinelID,
		Name:           "STUB_NOT_IMPLEMENTED",
		CategoryID:     stubItemSentinelCatID,
		AllowedUnitIDs: []uuid.UUID{stubItemSentinelUnitID},
		CreatedAt:      time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (r *PostgresItemRepository) List(ctx context.Context) ([]*models.Item, error) {
	return nil, nil
}

func (r *PostgresItemRepository) Create(ctx context.Context, name, categoryID string, unitIDs []string) (*models.Item, error) {
	return stubItem(), nil
}

func (r *PostgresItemRepository) Update(ctx context.Context, id, name, categoryID string, unitIDs []string) (*models.Item, error) {
	return stubItem(), nil
}

func (r *PostgresItemRepository) Delete(ctx context.Context, id string) error {
	return nil
}
