package repository

import (
	"context"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
)

type PostgresEmailVerificationTokenRepository struct {
	q *sqlc.Queries
}

func NewPostgresEmailVerificationTokenRepository(db sqlc.DBTX) *PostgresEmailVerificationTokenRepository {
	return &PostgresEmailVerificationTokenRepository{q: sqlc.New(db)}
}

func (r *PostgresEmailVerificationTokenRepository) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.EmailVerificationToken, error) {
	return &models.EmailVerificationToken{}, nil
}

func (r *PostgresEmailVerificationTokenRepository) FindActiveByUserID(ctx context.Context, userID string) (*models.EmailVerificationToken, error) {
	return &models.EmailVerificationToken{}, nil
}

func (r *PostgresEmailVerificationTokenRepository) IncrementAttempts(ctx context.Context, id string) (*models.EmailVerificationToken, error) {
	return &models.EmailVerificationToken{}, nil
}

func (r *PostgresEmailVerificationTokenRepository) MarkUsed(ctx context.Context, id string) error {
	return nil
}

func (r *PostgresEmailVerificationTokenRepository) DeleteActiveForUser(ctx context.Context, userID string) error {
	return nil
}
