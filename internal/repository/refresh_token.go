package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresRefreshTokenRepository struct {
	q *sqlc.Queries
}

func NewPostgresRefreshTokenRepository(db sqlc.DBTX) *PostgresRefreshTokenRepository {
	return &PostgresRefreshTokenRepository{q: sqlc.New(db)}
}

func (r *PostgresRefreshTokenRepository) CreateFamily(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) (*models.RefreshTokenFamily, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid family id: %w", err)
	}
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	created, err := r.q.CreateRefreshTokenFamily(ctx, sqlc.CreateRefreshTokenFamilyParams{
		ID:        idUUID,
		UserID:    userUUID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh token family: %w", err)
	}
	return toModelRefreshTokenFamily(created), nil
}

func (r *PostgresRefreshTokenRepository) DeleteStaleFamiliesForUser(ctx context.Context, userID string) error {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	if err := r.q.DeleteStaleRefreshTokenFamiliesForUser(ctx, userUUID); err != nil {
		return fmt.Errorf("failed to delete stale refresh token families: %w", err)
	}
	return nil
}

func (r *PostgresRefreshTokenRepository) RevokeAllFamiliesForUser(ctx context.Context, userID string) error {
	return nil
}

func toModelRefreshTokenFamily(f sqlc.RefreshToken) *models.RefreshTokenFamily {
	return &models.RefreshTokenFamily{
		ID:                     uuidValue(f.ID),
		UserID:                 uuidValue(f.UserID),
		TokenHash:              f.TokenHash,
		PreviousTokenHash:      textPtr(f.PreviousTokenHash),
		PreviousTokenRotatedAt: timePtr(f.PreviousTokenRotatedAt),
		ExpiresAt:              f.ExpiresAt.Time,
		RevokedAt:              timePtr(f.RevokedAt),
		CreatedAt:              f.CreatedAt.Time,
	}
}
