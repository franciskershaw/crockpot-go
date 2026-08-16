package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOrCreateUser_CreatesNewUser(t *testing.T) {
	ctx := context.Background()
	googleID := "repo-test-google-" + uuid.NewString()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	user, err := userRepo.GetOrCreateUser(ctx, email, googleID, "Test User", "http://example.com/avatar.png")
	require.NoError(t, err)
	require.NotNil(t, user)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, user.ID)

	assert.NotEqual(t, uuid.Nil, user.ID)
	require.NotNil(t, user.GoogleID)
	assert.Equal(t, googleID, *user.GoogleID)
	assert.Equal(t, email, user.Email)
	require.NotNil(t, user.Name)
	assert.Equal(t, "Test User", *user.Name)
	require.NotNil(t, user.Image)
	assert.Equal(t, "http://example.com/avatar.png", *user.Image)
	assert.Equal(t, "FREE", user.Role)
	assert.NotNil(t, user.EmailVerifiedAt, "Google signups should be verified immediately")
	assert.NotNil(t, user.LastLoginAt, "creation counts as the first login")
}

func TestGetOrCreateUser_ReturnsExistingAndRefreshesProfile(t *testing.T) {
	ctx := context.Background()
	googleID := "repo-test-google-" + uuid.NewString()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	created, err := userRepo.GetOrCreateUser(ctx, email, googleID, "Original Name", "http://example.com/original.png")
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, created.ID)
	time.Sleep(10 * time.Millisecond)

	fetched, err := userRepo.GetOrCreateUser(ctx, email, googleID, "Updated Name", "http://example.com/updated.png")
	require.NoError(t, err)
	require.NotNil(t, fetched)

	assert.Equal(t, created.ID, fetched.ID, "expected the same user record, not a new one")
	require.NotNil(t, fetched.Name)
	assert.Equal(t, "Updated Name", *fetched.Name, "expected name to refresh from the latest Google claims")
	require.NotNil(t, fetched.Image)
	assert.Equal(t, "http://example.com/updated.png", *fetched.Image, "expected image to refresh from the latest Google claims")
	require.NotNil(t, created.LastLoginAt)
	require.NotNil(t, fetched.LastLoginAt)
	assert.True(t, fetched.LastLoginAt.After(*created.LastLoginAt), "expected last_login_at to advance on repeat login")
}

func TestGetOrCreateUser_EmailAlreadyRegisteredWithPassword(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	passwordUserID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		passwordUserID, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, passwordUserID)

	user, err := userRepo.GetOrCreateUser(ctx, email, "repo-test-google-"+uuid.NewString(), "Test User", "")
	assert.Nil(t, user)
	assert.ErrorIs(t, err, models.ErrEmailRegisteredWithPassword)
}
