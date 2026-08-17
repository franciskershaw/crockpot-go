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

type PostgresPasswordResetTokenRepository struct {
	db sqlc.DBTX
}

func NewPostgresPasswordResetTokenRepository(db sqlc.DBTX) *PostgresPasswordResetTokenRepository {
	return &PostgresPasswordResetTokenRepository{db: db}
}

// AcquireUserLock takes a tx-scoped advisory lock on userID, serializing
// concurrent callers; must run inside a transaction (auto-releases on commit/rollback).
func (r *PostgresPasswordResetTokenRepository) AcquireUserLock(ctx context.Context, userID string) error {
	if err := queriesFor(ctx, r.db).AcquireUserPasswordResetLock(ctx, userID); err != nil {
		return fmt.Errorf("failed to acquire password reset lock: %w", err)
	}
	return nil
}

func (r *PostgresPasswordResetTokenRepository) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.PasswordResetToken, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	created, err := queriesFor(ctx, r.db).CreatePasswordResetToken(ctx, sqlc.CreatePasswordResetTokenParams{
		UserID:    userUUID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create password reset token: %w", err)
	}
	return toModelPasswordResetToken(created), nil
}

func (r *PostgresPasswordResetTokenRepository) FindActiveByUserID(ctx context.Context, userID string) (*models.PasswordResetToken, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	found, err := queriesFor(ctx, r.db).FindActivePasswordResetTokenByUserID(ctx, userUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrNoActivePasswordResetToken
		}
		return nil, fmt.Errorf("failed to find active password reset token: %w", err)
	}
	return toModelPasswordResetToken(found), nil
}

func (r *PostgresPasswordResetTokenRepository) FindActiveByTokenHash(ctx context.Context, tokenHash string) (*models.PasswordResetToken, error) {
	found, err := queriesFor(ctx, r.db).FindActivePasswordResetTokenByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrNoActivePasswordResetToken
		}
		return nil, fmt.Errorf("failed to find active password reset token: %w", err)
	}
	return toModelPasswordResetToken(found), nil
}

// MarkUsed atomically claims the token: claimed is true only if it was still unused and unexpired.
func (r *PostgresPasswordResetTokenRepository) MarkUsed(ctx context.Context, id string) (bool, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return false, fmt.Errorf("invalid token id: %w", err)
	}

	rowsAffected, err := queriesFor(ctx, r.db).MarkPasswordResetTokenUsed(ctx, idUUID)
	if err != nil {
		return false, fmt.Errorf("failed to mark password reset token used: %w", err)
	}
	return rowsAffected == 1, nil
}

func (r *PostgresPasswordResetTokenRepository) DeleteActiveForUser(ctx context.Context, userID string) error {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}

	if err := queriesFor(ctx, r.db).DeleteActivePasswordResetTokensForUser(ctx, userUUID); err != nil {
		return fmt.Errorf("failed to delete active password reset tokens: %w", err)
	}
	return nil
}

func toModelPasswordResetToken(t sqlc.PasswordResetToken) *models.PasswordResetToken {
	return &models.PasswordResetToken{
		ID:        uuidValue(t.ID),
		UserID:    uuidValue(t.UserID),
		TokenHash: t.TokenHash,
		ExpiresAt: t.ExpiresAt.Time,
		UsedAt:    timePtr(t.UsedAt),
		CreatedAt: t.CreatedAt.Time,
	}
}
