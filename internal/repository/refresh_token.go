package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresRefreshTokenRepository struct {
	db sqlc.DBTX
}

func NewPostgresRefreshTokenRepository(db sqlc.DBTX) *PostgresRefreshTokenRepository {
	return &PostgresRefreshTokenRepository{db: db}
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

	created, err := queriesFor(ctx, r.db).CreateRefreshTokenFamily(ctx, sqlc.CreateRefreshTokenFamilyParams{
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
	if err := queriesFor(ctx, r.db).DeleteStaleRefreshTokenFamiliesForUser(ctx, userUUID); err != nil {
		return fmt.Errorf("failed to delete stale refresh token families: %w", err)
	}
	return nil
}

func (r *PostgresRefreshTokenRepository) RevokeAllFamiliesForUser(ctx context.Context, userID string) error {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	if err := queriesFor(ctx, r.db).RevokeAllRefreshTokenFamiliesForUser(ctx, userUUID); err != nil {
		return fmt.Errorf("failed to revoke refresh token families: %w", err)
	}
	return nil
}

func (r *PostgresRefreshTokenRepository) FindFamilyByID(ctx context.Context, id, userID string) (*models.RefreshTokenFamily, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid family id: %w", err)
	}
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	found, err := queriesFor(ctx, r.db).GetRefreshTokenFamilyByID(ctx, sqlc.GetRefreshTokenFamilyByIDParams{
		ID:     idUUID,
		UserID: userUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrRefreshTokenFamilyNotFound
		}
		return nil, fmt.Errorf("failed to find refresh token family: %w", err)
	}
	return toModelRefreshTokenFamily(found), nil
}

// RotateFamily mutates only if presentedHash still matches the live row at write time, re-validated
// atomically in the UPDATE's WHERE clause. A false return means nothing was mutated — treat as reuse.
func (r *PostgresRefreshTokenRepository) RotateFamily(ctx context.Context, familyID, presentedHash, newTokenHash string, newExpiresAt, graceWindowCutoff time.Time) (bool, error) {
	idUUID, err := uuidParam(familyID)
	if err != nil {
		return false, fmt.Errorf("invalid family id: %w", err)
	}
	rows, err := queriesFor(ctx, r.db).RotateRefreshTokenFamily(ctx, sqlc.RotateRefreshTokenFamilyParams{
		ID:                idUUID,
		TokenHash:         newTokenHash,
		ExpiresAt:         pgtype.Timestamptz{Time: newExpiresAt, Valid: true},
		PresentedHash:     presentedHash,
		GraceWindowCutoff: pgtype.Timestamptz{Time: graceWindowCutoff, Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("failed to rotate refresh token family: %w", err)
	}
	return rows > 0, nil
}

func (r *PostgresRefreshTokenRepository) RevokeFamily(ctx context.Context, familyID string) error {
	idUUID, err := uuidParam(familyID)
	if err != nil {
		return fmt.Errorf("invalid family id: %w", err)
	}
	if err := queriesFor(ctx, r.db).RevokeRefreshTokenFamily(ctx, idUUID); err != nil {
		return fmt.Errorf("failed to revoke refresh token family: %w", err)
	}
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
