package repository

import (
	"context"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
)

type PostgresUnitRepository struct {
	db sqlc.DBTX
}

func NewPostgresUnitRepository(db sqlc.DBTX) *PostgresUnitRepository {
	return &PostgresUnitRepository{db: db}
}

var stubUnitSentinelID = uuid.MustParse("feedface-feed-face-feed-facefeedface")

func stubUnit() *models.Unit {
	return &models.Unit{
		ID:           stubUnitSentinelID,
		Name:         "STUB_NOT_IMPLEMENTED",
		Abbreviation: "STUB_NOT_IMPLEMENTED",
		CreatedAt:    time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func (r *PostgresUnitRepository) List(ctx context.Context) ([]*models.Unit, error) {
	return nil, nil
}

func (r *PostgresUnitRepository) Create(ctx context.Context, name, abbreviation string) (*models.Unit, error) {
	return stubUnit(), nil
}

func (r *PostgresUnitRepository) Update(ctx context.Context, id, name, abbreviation string) (*models.Unit, error) {
	return stubUnit(), nil
}

func (r *PostgresUnitRepository) Delete(ctx context.Context, id string) error {
	return nil
}
