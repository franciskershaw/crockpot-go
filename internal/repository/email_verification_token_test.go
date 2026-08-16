package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailVerificationTokenCreate_PersistsRow(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()
	expiresAt := time.Now().Add(10 * time.Minute)

	token, err := emailVerificationTokenRepo.Create(ctx, repoUserID.String(), hash, expiresAt)
	require.NoError(t, err)
	require.NotNil(t, token)
	cleanupExec(t, `DELETE FROM email_verification_tokens WHERE id = $1`, token.ID)

	assert.Equal(t, repoUserID, token.UserID)
	assert.Equal(t, hash, token.TokenHash)
	assert.Equal(t, 0, token.Attempts)
	assert.Nil(t, token.UsedAt)
	assert.WithinDuration(t, expiresAt, token.ExpiresAt, time.Second)
}

func TestEmailVerificationTokenFindActiveByUserID_ReturnsActiveToken(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := emailVerificationTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(10*time.Minute))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM email_verification_tokens WHERE id = $1`, created.ID)

	found, err := emailVerificationTokenRepo.FindActiveByUserID(ctx, repoUserID.String())
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, hash, found.TokenHash)
}

func TestEmailVerificationTokenFindActiveByUserID_ReturnsErrWhenNoneActive(t *testing.T) {
	ctx := context.Background()
	userWithNoToken := uuid.New()

	_, err := emailVerificationTokenRepo.FindActiveByUserID(ctx, userWithNoToken.String())
	assert.ErrorIs(t, err, models.ErrNoActiveEmailVerificationToken)
}

func TestEmailVerificationTokenIncrementAttempts_IncrementsCount(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := emailVerificationTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(10*time.Minute))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM email_verification_tokens WHERE id = $1`, created.ID)

	updated, err := emailVerificationTokenRepo.IncrementAttempts(ctx, created.ID.String())
	require.NoError(t, err)
	assert.Equal(t, 1, updated.Attempts)

	updated, err = emailVerificationTokenRepo.IncrementAttempts(ctx, created.ID.String())
	require.NoError(t, err)
	assert.Equal(t, 2, updated.Attempts)
}

func TestEmailVerificationTokenMarkUsed_ExcludesFromActiveLookup(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := emailVerificationTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(10*time.Minute))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM email_verification_tokens WHERE id = $1`, created.ID)

	err = emailVerificationTokenRepo.MarkUsed(ctx, created.ID.String())
	require.NoError(t, err)

	_, err = emailVerificationTokenRepo.FindActiveByUserID(ctx, repoUserID.String())
	assert.ErrorIs(t, err, models.ErrNoActiveEmailVerificationToken)
}

func TestEmailVerificationTokenDeleteActiveForUser_RemovesActiveRow(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := emailVerificationTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(10*time.Minute))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM email_verification_tokens WHERE id = $1`, created.ID)

	err = emailVerificationTokenRepo.DeleteActiveForUser(ctx, repoUserID.String())
	require.NoError(t, err)

	_, err = emailVerificationTokenRepo.FindActiveByUserID(ctx, repoUserID.String())
	assert.ErrorIs(t, err, models.ErrNoActiveEmailVerificationToken)
}
