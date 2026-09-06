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

type PostgresEmailVerificationTokenRepository struct {
	db sqlc.DBTX
}

func NewPostgresEmailVerificationTokenRepository(db sqlc.DBTX) *PostgresEmailVerificationTokenRepository {
	return &PostgresEmailVerificationTokenRepository{db: db}
}

func (r *PostgresEmailVerificationTokenRepository) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.EmailVerificationToken, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	created, err := queriesFor(ctx, r.db).CreateEmailVerificationToken(ctx, sqlc.CreateEmailVerificationTokenParams{
		UserID:    userUUID,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create email verification token: %w", err)
	}
	return toModelEmailVerificationToken(created), nil
}

func (r *PostgresEmailVerificationTokenRepository) FindActiveByUserID(ctx context.Context, userID string) (*models.EmailVerificationToken, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}

	found, err := queriesFor(ctx, r.db).FindActiveEmailVerificationTokenByUserID(ctx, userUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrNoActiveEmailVerificationToken
		}
		return nil, fmt.Errorf("failed to find active email verification token: %w", err)
	}
	return toModelEmailVerificationToken(found), nil
}

func (r *PostgresEmailVerificationTokenRepository) IncrementAttempts(ctx context.Context, id string) (*models.EmailVerificationToken, error) {
	idUUID, err := uuidParam(id)
	if err != nil {
		return nil, fmt.Errorf("invalid token id: %w", err)
	}

	updated, err := queriesFor(ctx, r.db).IncrementEmailVerificationTokenAttempts(ctx, idUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to increment email verification token attempts: %w", err)
	}
	return toModelEmailVerificationToken(updated), nil
}

func (r *PostgresEmailVerificationTokenRepository) MarkUsed(ctx context.Context, id string) error {
	idUUID, err := uuidParam(id)
	if err != nil {
		return fmt.Errorf("invalid token id: %w", err)
	}

	if err := queriesFor(ctx, r.db).MarkEmailVerificationTokenUsed(ctx, idUUID); err != nil {
		return fmt.Errorf("failed to mark email verification token used: %w", err)
	}
	return nil
}

func (r *PostgresEmailVerificationTokenRepository) DeleteActiveForUser(ctx context.Context, userID string) error {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}

	if err := queriesFor(ctx, r.db).DeleteActiveEmailVerificationTokensForUser(ctx, userUUID); err != nil {
		return fmt.Errorf("failed to delete active email verification tokens: %w", err)
	}
	return nil
}

func (r *PostgresEmailVerificationTokenRepository) DeleteAllStale(ctx context.Context) error {
	if err := queriesFor(ctx, r.db).DeleteAllStaleEmailVerificationTokens(ctx); err != nil {
		return fmt.Errorf("failed to delete all stale email verification tokens: %w", err)
	}
	return nil
}

func toModelEmailVerificationToken(t sqlc.EmailVerificationToken) *models.EmailVerificationToken {
	return &models.EmailVerificationToken{
		ID:        uuidValue(t.ID),
		UserID:    uuidValue(t.UserID),
		TokenHash: t.TokenHash,
		Attempts:  int(t.Attempts),
		ExpiresAt: t.ExpiresAt.Time,
		UsedAt:    timePtr(t.UsedAt),
		CreatedAt: t.CreatedAt.Time,
	}
}
