package repository

import (
	"context"
	"errors"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
)

type PostgresUserRepository struct {
	q *sqlc.Queries
}

func NewPostgresUserRepository(db sqlc.DBTX) *PostgresUserRepository {
	return &PostgresUserRepository{q: sqlc.New(db)}
}

func (r *PostgresUserRepository) GetOrCreateUser(ctx context.Context, email, googleID, displayName, avatarURL string) (*models.User, error) {
	return nil, errors.New("not implemented")
}
