package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type PostgresUserRepository struct {
	q *sqlc.Queries
}

func NewPostgresUserRepository(db sqlc.DBTX) *PostgresUserRepository {
	return &PostgresUserRepository{q: sqlc.New(db)}
}

// GetOrCreateUser refreshes name/image/last_login_at on a google_id match, creates one otherwise, or returns models.ErrEmailRegisteredWithPassword on an email conflict.
func (r *PostgresUserRepository) GetOrCreateUser(ctx context.Context, email, googleID, displayName, avatarURL string) (*models.User, error) {
	existing, err := r.q.GetUserByGoogleID(ctx, textParam(googleID))
	switch {
	case err == nil:
		updated, err := r.q.UpdateUserLoginProfile(ctx, sqlc.UpdateUserLoginProfileParams{
			ID:    existing.ID,
			Name:  textParam(displayName),
			Image: textParam(avatarURL),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update login profile: %w", err)
		}
		return toModelUser(updated), nil
	case errors.Is(err, pgx.ErrNoRows):
		// fall through to create
	default:
		return nil, fmt.Errorf("failed to look up user by google id: %w", err)
	}

	created, err := r.q.CreateGoogleUser(ctx, sqlc.CreateGoogleUserParams{
		Email:    email,
		GoogleID: textParam(googleID),
		Name:     textParam(displayName),
		Image:    textParam(avatarURL),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			return nil, models.ErrEmailRegisteredWithPassword
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return toModelUser(created), nil
}

func toModelUser(u sqlc.User) *models.User {
	return &models.User{
		ID:              uuidValue(u.ID),
		GoogleID:        textPtr(u.GoogleID),
		PasswordHash:    textPtr(u.PasswordHash),
		Email:           u.Email,
		Name:            textPtr(u.Name),
		Image:           textPtr(u.Image),
		Role:            u.Role,
		EmailVerifiedAt: timePtr(u.EmailVerifiedAt),
		LastLoginAt:     timePtr(u.LastLoginAt),
		CreatedAt:       u.CreatedAt.Time,
		UpdatedAt:       u.UpdatedAt.Time,
	}
}
