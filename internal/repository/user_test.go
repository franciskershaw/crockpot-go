package repository_test

import (
	"context"
	"sync"
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

func TestGetOrCreateUser_ReturnsExistingAndPreservesProfileOnEmptyClaims(t *testing.T) {
	ctx := context.Background()
	googleID := "repo-test-google-" + uuid.NewString()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	created, err := userRepo.GetOrCreateUser(ctx, email, googleID, "Original Name", "http://example.com/original.png")
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, created.ID)

	fetched, err := userRepo.GetOrCreateUser(ctx, email, googleID, "", "")
	require.NoError(t, err)
	require.NotNil(t, fetched)

	require.NotNil(t, fetched.Name)
	assert.Equal(t, "Original Name", *fetched.Name, "empty claim should not blank out a previously stored name")
	require.NotNil(t, fetched.Image)
	assert.Equal(t, "http://example.com/original.png", *fetched.Image, "empty claim should not blank out a previously stored image")
}

func TestGetOrCreateUser_ConcurrentFirstLoginsForSameAccountBothSucceed(t *testing.T) {
	ctx := context.Background()
	googleID := "repo-test-google-" + uuid.NewString()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	var wg sync.WaitGroup
	results := make([]*models.User, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = userRepo.GetOrCreateUser(ctx, email, googleID, "Test User", "http://example.com/avatar.png")
		}(i)
	}
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.NotNil(t, results[0])
	require.NotNil(t, results[1])
	assert.Equal(t, results[0].ID, results[1].ID, "concurrent first logins for the same account should resolve to one user, not error")
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, results[0].ID)
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

func TestCreateUnconfirmedUser_CreatesNewUser(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	user, err := userRepo.CreateUnconfirmedUser(ctx, email, "bcrypt-hash-placeholder", "Test User")
	require.NoError(t, err)
	require.NotNil(t, user)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, user.ID)

	assert.Equal(t, email, user.Email)
	require.NotNil(t, user.PasswordHash)
	assert.Equal(t, "bcrypt-hash-placeholder", *user.PasswordHash)
	require.NotNil(t, user.Name)
	assert.Equal(t, "Test User", *user.Name)
	assert.Nil(t, user.GoogleID)
	assert.Nil(t, user.EmailVerifiedAt, "should not be confirmed until the OTP is verified")
	assert.Equal(t, "FREE", user.Role)
}

func TestCreateUnconfirmedUser_EmailAlreadyRegisteredWithGoogle(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	googleUserID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, google_id, email, email_verified_at) VALUES ($1, $2, $3, CURRENT_TIMESTAMP)`,
		googleUserID, "repo-test-google-"+uuid.NewString(), email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, googleUserID)

	user, err := userRepo.CreateUnconfirmedUser(ctx, email, "bcrypt-hash-placeholder", "Test User")
	assert.Nil(t, user)
	assert.ErrorIs(t, err, models.ErrEmailRegisteredWithGoogle)
}

func TestCreateUnconfirmedUser_EmailAlreadyRegisteredWithConfirmedPassword(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email, email_verified_at) VALUES ($1, $2, $3, CURRENT_TIMESTAMP)`,
		existingID, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	user, err := userRepo.CreateUnconfirmedUser(ctx, email, "bcrypt-hash-placeholder", "Test User")
	assert.Nil(t, user)
	assert.ErrorIs(t, err, models.ErrEmailRegisteredWithPassword)
}

func TestCreateUnconfirmedUser_EmailHasUnconfirmedPasswordAccount(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		existingID, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	user, err := userRepo.CreateUnconfirmedUser(ctx, email, "bcrypt-hash-placeholder", "Test User")
	assert.Nil(t, user)
	assert.ErrorIs(t, err, models.ErrEmailUnconfirmed)
}

func TestUpdatePasswordAndClearConfirmation_OverwritesUnconfirmedAccount(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, name, email) VALUES ($1, $2, $3, $4)`,
		existingID, "old-hash", "Old Name", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	updated, err := userRepo.UpdatePasswordAndClearConfirmation(ctx, email, "new-hash", "New Name")
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, existingID, updated.ID, "should update the existing row, not create a new one")
	require.NotNil(t, updated.PasswordHash)
	assert.Equal(t, "new-hash", *updated.PasswordHash)
	require.NotNil(t, updated.Name)
	assert.Equal(t, "New Name", *updated.Name)
	assert.Nil(t, updated.EmailVerifiedAt)
}

func TestFindByEmail_ReturnsUser(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		existingID, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	user, err := userRepo.FindByEmail(ctx, email)
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, existingID, user.ID)
	assert.Equal(t, email, user.Email)
}

func TestFindByEmail_ReturnsErrUserNotFound(t *testing.T) {
	ctx := context.Background()

	user, err := userRepo.FindByEmail(ctx, "repo-test-nonexistent-"+uuid.NewString()+"@example.com")
	assert.Nil(t, user)
	assert.ErrorIs(t, err, models.ErrUserNotFound)
}

func TestMarkEmailConfirmed_SetsEmailVerifiedAt(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		existingID, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	updated, err := userRepo.MarkEmailConfirmed(ctx, existingID.String())
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.NotNil(t, updated.EmailVerifiedAt)
}
