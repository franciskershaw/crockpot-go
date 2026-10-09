package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// normaliseEmail folds A–Z only, matching users.email's CHECK; full Unicode folding would let lookalikes (Kelvin sign → k) match real accounts.
func normaliseEmail(email string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, email)
}

type PostgresUserRepository struct {
	db sqlc.DBTX
}

func NewPostgresUserRepository(db sqlc.DBTX) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

// GetOrCreateUser updates only last_login_at on a google_id match (the stored name is the user's to edit), creates one otherwise, or returns models.ErrEmailRegisteredWithPassword on an email conflict.
func (r *PostgresUserRepository) GetOrCreateUser(ctx context.Context, email, googleID, displayName string) (*models.User, error) {
	email = normaliseEmail(email)
	existing, err := queriesFor(ctx, r.db).GetUserByGoogleID(ctx, textParam(googleID))
	switch {
	case err == nil:
		return r.markLogin(ctx, existing.ID)
	case errors.Is(err, pgx.ErrNoRows):
		// fall through to create
	default:
		return nil, fmt.Errorf("failed to look up user by google id: %w", err)
	}

	created, err := queriesFor(ctx, r.db).CreateGoogleUser(ctx, sqlc.CreateGoogleUserParams{
		Email:    email,
		GoogleID: textParam(googleID),
		Name:     optionalTextParam(displayName),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Re-check by google_id regardless of which constraint fired — a concurrent first login for the same account can trip either.
			if existing, findErr := queriesFor(ctx, r.db).GetUserByGoogleID(ctx, textParam(googleID)); findErr == nil {
				return r.markLogin(ctx, existing.ID)
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
	email = normaliseEmail(email)
	created, err := queriesFor(ctx, r.db).CreateUnconfirmedUser(ctx, sqlc.CreateUnconfirmedUserParams{
		Email:        email,
		PasswordHash: textParam(passwordHash),
		Name:         textParam(name),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
			existing, findErr := queriesFor(ctx, r.db).GetUserByEmail(ctx, email)
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
	found, err := queriesFor(ctx, r.db).GetUserByEmail(ctx, normaliseEmail(email))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user by email: %w", err)
	}
	return toModelUser(found), nil
}

func (r *PostgresUserRepository) FindByID(ctx context.Context, userID string) (*models.User, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	found, err := queriesFor(ctx, r.db).GetUserByID(ctx, userUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user by id: %w", err)
	}
	return toModelUser(found), nil
}

// FindByIDForUpdate is FindByID that also row-locks the user until the caller's transaction ends.
func (r *PostgresUserRepository) FindByIDForUpdate(ctx context.Context, userID string) (*models.User, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	found, err := queriesFor(ctx, r.db).GetUserByIDForUpdate(ctx, userUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to find user by id for update: %w", err)
	}
	return toModelUser(found), nil
}

func (r *PostgresUserRepository) MarkEmailConfirmed(ctx context.Context, userID string) (*models.User, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	updated, err := queriesFor(ctx, r.db).MarkUserEmailConfirmed(ctx, userUUID)
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
	return r.markLogin(ctx, userUUID)
}

func (r *PostgresUserRepository) UpdateName(ctx context.Context, userID, name string) (*models.User, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	updated, err := queriesFor(ctx, r.db).UpdateUserName(ctx, sqlc.UpdateUserNameParams{
		ID:   userUUID,
		Name: textParam(name),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, models.ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to update name: %w", err)
	}
	return toModelUser(updated), nil
}

// Delete removes the user; every table that references them cascades, and recipes they created keep a null creator.
func (r *PostgresUserRepository) Delete(ctx context.Context, userID string) error {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	n, err := queriesFor(ctx, r.db).DeleteUser(ctx, userUUID)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}
	if n == 0 {
		return models.ErrUserNotFound
	}
	return nil
}

func (r *PostgresUserRepository) markLogin(ctx context.Context, id pgtype.UUID) (*models.User, error) {
	updated, err := queriesFor(ctx, r.db).UpdateUserLastLogin(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update last login: %w", err)
	}
	return toModelUser(updated), nil
}

func (r *PostgresUserRepository) UpdatePassword(ctx context.Context, userID, passwordHash string) (*models.User, error) {
	userUUID, err := uuidParam(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	updated, err := queriesFor(ctx, r.db).UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
		ID:           userUUID,
		PasswordHash: textParam(passwordHash),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update password: %w", err)
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
		Role:            u.Role,
		EmailVerifiedAt: timePtr(u.EmailVerifiedAt),
		LastLoginAt:     timePtr(u.LastLoginAt),
		CreatedAt:       u.CreatedAt.Time,
		UpdatedAt:       u.UpdatedAt.Time,
	}
}
