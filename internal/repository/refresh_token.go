package repository

import (
	"context"
	"errors"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
)

type PostgresRefreshTokenRepository struct {
	q *sqlc.Queries
}

func NewPostgresRefreshTokenRepository(db sqlc.DBTX) *PostgresRefreshTokenRepository {
	return &PostgresRefreshTokenRepository{q: sqlc.New(db)}
}

func (r *PostgresRefreshTokenRepository) CreateFamily(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) (*models.RefreshTokenFamily, error) {
	return nil, errors.New("not implemented")
}

func (r *PostgresRefreshTokenRepository) DeleteStaleFamiliesForUser(ctx context.Context, userID string) error {
	return errors.New("not implemented")
}
