package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresUserRepository struct {
	q *sqlc.Queries
}

func NewPostgresUserRepository(db sqlc.DBTX) *PostgresUserRepository {
	return &PostgresUserRepository{q: sqlc.New(db)}
}

// GetOrCreateUser refreshes name/image/last_login_at on a google_id match (empty claims leave the stored value untouched), creates one otherwise, or returns models.ErrEmailRegisteredWithPassword on an email conflict.
func (r *PostgresUserRepository) GetOrCreateUser(ctx context.Context, email, googleID, displayName, avatarURL string) (*models.User, error) {
	existing, err := r.q.GetUserByGoogleID(ctx, textParam(googleID))
	switch {
	case err == nil:
		return r.refreshLoginProfile(ctx, existing.ID, displayName, avatarURL)
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
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Re-check by google_id regardless of which constraint fired — a concurrent first login for the same account can trip either.
			if existing, findErr := r.q.GetUserByGoogleID(ctx, textParam(googleID)); findErr == nil {
				return r.refreshLoginProfile(ctx, existing.ID, displayName, avatarURL)
			}
			if pgErr.ConstraintName == "users_email_key" {
				return nil, models.ErrEmailRegisteredWithPassword
			}
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return toModelUser(created), nil
}

// On an email collision, distinguishes a Google account, a confirmed password account, and an abandoned unconfirmed signup rather than one generic conflict error.
func (r *PostgresUserRepository) CreateUnconfirmedUser(ctx context.Context, email, passwordHash, name string) (*models.User, error) {
	created, err := r.q.CreateUnconfirmedUser(ctx, sqlc.CreateUnconfirmedUserParams{
		Email:        email,
		PasswordHash: textParam(passwordHash),
		Name:         textParam(name),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			existing, findErr := r.q.GetUserByEmail(ctx, email)
			if findErr != nil {
				return nil, fmt.Errorf("failed to look up existing user by email: %w", findErr)
			}
			switch {
			case existing.GoogleID.Valid:
				return nil, models.ErrEmailRegisteredWithGoogle
			case existing.EmailVerifiedAt.Valid:
				return nil, models.ErrEmailRegisteredWithPassword
			default:
				return nil, models.ErrEmailUnconfirmed
			}
		}
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return toModelUser(created), nil
}

func (r *PostgresUserRepository) FindByEmail(ctx context.Context, email string) (*models.User, error) {
	found, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}
	return toModelUser(found), nil
}

func (r *PostgresUserRepository) MarkEmailConfirmed(ctx context.Context, userID string) (*models.User, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	updated, err := r.q.MarkUserEmailConfirmed(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to mark email confirmed: %w", err)
	}
	return toModelUser(updated), nil
}

func (r *PostgresUserRepository) UpdateLastLogin(ctx context.Context, userID string) (*models.User, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	updated, err := r.q.UpdateUserLastLogin(ctx, userUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to update last login: %w", err)
	}
	return toModelUser(updated), nil
}

func (r *PostgresUserRepository) UpdatePassword(ctx context.Context, userID, passwordHash string) (*models.User, error) {
	return nil, errors.New("stub not implemented")
}

func (r *PostgresUserRepository) refreshLoginProfile(ctx context.Context, id pgtype.UUID, displayName, avatarURL string) (*models.User, error) {
	updated, err := r.q.UpdateUserLoginProfile(ctx, sqlc.UpdateUserLoginProfileParams{
		ID:          id,
		DisplayName: displayName,
		AvatarUrl:   avatarURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update login profile: %w", err)
	}
	return toModelUser(updated), nil
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
