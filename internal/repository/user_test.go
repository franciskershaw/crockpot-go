package repository_test

import (
	"context"
	"strings"
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

	user, err := userRepo.GetOrCreateUser(ctx, email, googleID, "Test User")
	require.NoError(t, err)
	require.NotNil(t, user)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, user.ID)

	assert.NotEqual(t, uuid.Nil, user.ID)
	require.NotNil(t, user.GoogleID)
	assert.Equal(t, googleID, *user.GoogleID)
	assert.Equal(t, email, user.Email)
	require.NotNil(t, user.Name)
	assert.Equal(t, "Test User", *user.Name)
	assert.Equal(t, "FREE", user.Role)
	assert.NotNil(t, user.EmailVerifiedAt, "Google signups should be verified immediately")
	assert.NotNil(t, user.LastLoginAt, "creation counts as the first login")
}

func TestGetOrCreateUser_ReturnsExistingAndKeepsStoredName(t *testing.T) {
	ctx := context.Background()
	googleID := "repo-test-google-" + uuid.NewString()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	created, err := userRepo.GetOrCreateUser(ctx, email, googleID, "Original Name")
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, created.ID)
	time.Sleep(10 * time.Millisecond)

	fetched, err := userRepo.GetOrCreateUser(ctx, email, googleID, "Name From Google")
	require.NoError(t, err)
	require.NotNil(t, fetched)

	assert.Equal(t, created.ID, fetched.ID, "expected the same user record, not a new one")
	require.NotNil(t, fetched.Name)
	assert.Equal(t, "Original Name", *fetched.Name, "a returning sign-in must not overwrite the stored name")
	require.NotNil(t, created.LastLoginAt)
	require.NotNil(t, fetched.LastLoginAt)
	assert.True(t, fetched.LastLoginAt.After(*created.LastLoginAt), "expected last_login_at to advance on repeat login")
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
			results[i], errs[i] = userRepo.GetOrCreateUser(ctx, email, googleID, "Test User")
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

	user, err := userRepo.GetOrCreateUser(ctx, email, "repo-test-google-"+uuid.NewString(), "Test User")
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

func TestFindByID_ReturnsUser(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		existingID, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	user, err := userRepo.FindByID(ctx, existingID.String())
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, existingID, user.ID)
	assert.Equal(t, email, user.Email)
}

func TestFindByID_ReturnsErrUserNotFound(t *testing.T) {
	ctx := context.Background()

	user, err := userRepo.FindByID(ctx, uuid.NewString())
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

func TestUpdateLastLogin_AdvancesLastLoginAt(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()
	staleLastLogin := time.Now().Add(-1 * time.Hour)

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email, last_login_at) VALUES ($1, $2, $3, $4)`,
		existingID, "bcrypt-hash-placeholder", email, staleLastLogin,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	updated, err := userRepo.UpdateLastLogin(ctx, existingID.String())
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.LastLoginAt)
	assert.True(t, updated.LastLoginAt.After(staleLastLogin), "expected last_login_at to advance past its stale value")
}

func TestUpdatePassword_SetsNewPasswordHash(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	existingID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		existingID, "old-bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, existingID)

	newHash := "new-bcrypt-hash-" + uuid.NewString()
	updated, err := userRepo.UpdatePassword(ctx, existingID.String(), newHash)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.PasswordHash)
	assert.Equal(t, newHash, *updated.PasswordHash)
}

func TestUpdateName_SetsName(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Name Before")

	updated, err := userRepo.UpdateName(ctx, userID.String(), "Name After")
	require.NoError(t, err)
	require.NotNil(t, updated.Name)
	assert.Equal(t, "Name After", *updated.Name)
	assert.Equal(t, userID, updated.ID)

	fetched, err := userRepo.FindByID(ctx, userID.String())
	require.NoError(t, err)
	require.NotNil(t, fetched.Name)
	assert.Equal(t, "Name After", *fetched.Name)
}

func TestUpdateName_ReturnsErrUserNotFound(t *testing.T) {
	updated, err := userRepo.UpdateName(context.Background(), uuid.NewString(), "Nobody")
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrUserNotFound)
}

func TestGetOrCreateUser_EmptyNameStoredAsNull(t *testing.T) {
	ctx := context.Background()
	user, err := userRepo.GetOrCreateUser(ctx, "repo-test-"+uuid.NewString()+"@example.com", "repo-test-google-"+uuid.NewString(), "")
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, user.ID)

	assert.Nil(t, user.Name, "an empty Google name is no name, not an empty string a byline would render")
}

func TestFindByIDForUpdate_ReturnsUser(t *testing.T) {
	userID := insertTestUser(t, "Locked Reader")

	err := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
		user, err := userRepo.FindByIDForUpdate(ctx, userID.String())
		require.NoError(t, err)
		assert.Equal(t, userID, user.ID)
		return nil
	})
	require.NoError(t, err)
}

func TestFindByIDForUpdate_ReturnsErrUserNotFound(t *testing.T) {
	err := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
		_, err := userRepo.FindByIDForUpdate(ctx, uuid.NewString())
		return err
	})
	assert.ErrorIs(t, err, models.ErrUserNotFound)
}

func TestFindByIDForUpdate_BlocksConcurrentWriteUntilCommit(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Locked User")

	locked := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	holdErr := make(chan error, 1)
	go func() {
		holdErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			if _, err := userRepo.FindByIDForUpdate(ctx, userID.String()); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-holdErr:
		t.Fatalf("lookup failed before holding its transaction open: %v", err)
	}

	written := make(chan error, 1)
	go func() {
		_, err := userRepo.UpdateName(ctx, userID.String(), "Written While Locked")
		written <- err
	}()

	waitForLockWait(t)
	releaseOnce(release)

	require.NoError(t, <-holdErr)
	require.NoError(t, <-written)
}

func TestCreateUnconfirmedUser_StoresEmailLowercase(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	user, err := userRepo.CreateUnconfirmedUser(ctx, strings.ToUpper(email), "bcrypt-hash-placeholder", "Test User")
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, user.ID)

	assert.Equal(t, email, user.Email)
}

func TestFindByEmail_MatchesAnyCase(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	id := uuid.New()
	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		id, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, id)

	found, err := userRepo.FindByEmail(ctx, strings.ToUpper(email))
	require.NoError(t, err)
	assert.Equal(t, id, found.ID)
}

func TestCreateUnconfirmedUser_CaseVariantOfExistingEmailConflicts(t *testing.T) {
	cases := []struct {
		name   string
		insert string
		want   error
	}{
		{"google account", `INSERT INTO users (id, email, google_id, email_verified_at) VALUES ($1, $2, 'repo-test-google-' || $1::text, CURRENT_TIMESTAMP)`, models.ErrEmailRegisteredWithGoogle},
		{"confirmed password account", `INSERT INTO users (id, email, password_hash, email_verified_at) VALUES ($1, $2, 'bcrypt-hash-placeholder', CURRENT_TIMESTAMP)`, models.ErrEmailRegisteredWithPassword},
		{"unconfirmed signup", `INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'bcrypt-hash-placeholder')`, models.ErrEmailUnconfirmed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			email := "repo-test-" + uuid.NewString() + "@example.com"
			id := uuid.New()
			_, err := db.DB.Exec(ctx, tc.insert, id, email)
			require.NoError(t, err)
			cleanupExec(t, `DELETE FROM users WHERE id = $1`, id)

			user, err := userRepo.CreateUnconfirmedUser(ctx, strings.ToUpper(email), "bcrypt-hash-placeholder", "Test User")
			assert.Nil(t, user)
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestGetOrCreateUser_StoresEmailLowercase(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"

	user, err := userRepo.GetOrCreateUser(ctx, strings.ToUpper(email), "repo-test-google-"+uuid.NewString(), "Test User")
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, user.ID)

	assert.Equal(t, email, user.Email)
}

func TestGetOrCreateUser_CaseVariantOfPasswordAccountConflicts(t *testing.T) {
	ctx := context.Background()
	email := "repo-test-" + uuid.NewString() + "@example.com"
	id := uuid.New()
	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, password_hash, email) VALUES ($1, $2, $3)`,
		id, "bcrypt-hash-placeholder", email,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, id)

	user, err := userRepo.GetOrCreateUser(ctx, strings.ToUpper(email), "repo-test-google-"+uuid.NewString(), "Test User")
	assert.Nil(t, user)
	assert.ErrorIs(t, err, models.ErrEmailRegisteredWithPassword)
}
