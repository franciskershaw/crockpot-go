package repository

import (
	"context"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
)

type PostgresPasswordResetTokenRepository struct {
	q *sqlc.Queries
}

func NewPostgresPasswordResetTokenRepository(db sqlc.DBTX) *PostgresPasswordResetTokenRepository {
	return &PostgresPasswordResetTokenRepository{q: sqlc.New(db)}
}

func (r *PostgresPasswordResetTokenRepository) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.PasswordResetToken, error) {
	return &models.PasswordResetToken{
		ID:        uuid.New(),
		UserID:    uuid.Nil,
		TokenHash: "STUBTOKENHASH",
		ExpiresAt: time.Now(),
		CreatedAt: time.Now(),
	}, nil
}

func (r *PostgresPasswordResetTokenRepository) FindActiveByUserID(ctx context.Context, userID string) (*models.PasswordResetToken, error) {
	return nil, models.ErrNoActivePasswordResetToken
}

func (r *PostgresPasswordResetTokenRepository) FindActiveByTokenHash(ctx context.Context, tokenHash string) (*models.PasswordResetToken, error) {
	return nil, models.ErrNoActivePasswordResetToken
}

func (r *PostgresPasswordResetTokenRepository) MarkUsed(ctx context.Context, id string) error {
	return nil
}

func (r *PostgresPasswordResetTokenRepository) DeleteActiveForUser(ctx context.Context, userID string) error {
	return nil
}
