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

func TestPasswordResetTokenCreate_PersistsRow(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()
	expiresAt := time.Now().Add(time.Hour)

	token, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, expiresAt)
	require.NoError(t, err)
	require.NotNil(t, token)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, token.ID)

	assert.Equal(t, repoUserID, token.UserID)
	assert.Equal(t, hash, token.TokenHash)
	assert.Nil(t, token.UsedAt)
	assert.WithinDuration(t, expiresAt, token.ExpiresAt, time.Second)
}

func TestPasswordResetTokenFindActiveByUserID_ReturnsActiveToken(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, created.ID)

	found, err := passwordResetTokenRepo.FindActiveByUserID(ctx, repoUserID.String())
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, hash, found.TokenHash)
}

func TestPasswordResetTokenFindActiveByUserID_ReturnsErrWhenNoneActive(t *testing.T) {
	ctx := context.Background()
	userWithNoToken := uuid.New()

	_, err := passwordResetTokenRepo.FindActiveByUserID(ctx, userWithNoToken.String())
	assert.ErrorIs(t, err, models.ErrNoActivePasswordResetToken)
}

func TestPasswordResetTokenFindActiveByTokenHash_ReturnsActiveToken(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, created.ID)

	found, err := passwordResetTokenRepo.FindActiveByTokenHash(ctx, hash)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, created.ID, found.ID)
	assert.Equal(t, repoUserID, found.UserID)
}

func TestPasswordResetTokenFindActiveByTokenHash_ReturnsErrWhenNoneActive(t *testing.T) {
	ctx := context.Background()

	_, err := passwordResetTokenRepo.FindActiveByTokenHash(ctx, "no-such-hash-"+uuid.NewString())
	assert.ErrorIs(t, err, models.ErrNoActivePasswordResetToken)
}

func TestPasswordResetTokenMarkUsed_ExcludesFromActiveLookup(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, created.ID)

	claimed, err := passwordResetTokenRepo.MarkUsed(ctx, created.ID.String())
	require.NoError(t, err)
	assert.True(t, claimed)

	_, err = passwordResetTokenRepo.FindActiveByTokenHash(ctx, hash)
	assert.ErrorIs(t, err, models.ErrNoActivePasswordResetToken)
}

func TestPasswordResetTokenMarkUsed_ReturnsFalseWhenAlreadyUsed(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, created.ID)

	claimed, err := passwordResetTokenRepo.MarkUsed(ctx, created.ID.String())
	require.NoError(t, err)
	require.True(t, claimed)

	claimed, err = passwordResetTokenRepo.MarkUsed(ctx, created.ID.String())
	require.NoError(t, err)
	assert.False(t, claimed, "a second MarkUsed on an already-used token must not re-claim it")
}

func TestPasswordResetTokenMarkUsed_ReturnsFalseWhenExpired(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, created.ID)

	_, err = db.DB.Exec(ctx, `UPDATE password_reset_tokens SET expires_at = $2 WHERE id = $1`, created.ID, time.Now().Add(-time.Minute))
	require.NoError(t, err)

	claimed, err := passwordResetTokenRepo.MarkUsed(ctx, created.ID.String())
	require.NoError(t, err)
	assert.False(t, claimed, "an expired token must not be claimable even if never used")
}

// Proves the atomic conditional UPDATE actually serializes concurrent claims, against the real DB.
func TestPasswordResetTokenMarkUsed_ConcurrentCallersOnlyOneClaims(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, created.ID)

	const attempts = 10
	results := make([]bool, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			claimed, err := passwordResetTokenRepo.MarkUsed(ctx, created.ID.String())
			require.NoError(t, err)
			results[i] = claimed
		}(i)
	}
	wg.Wait()

	claimedCount := 0
	for _, c := range results {
		if c {
			claimedCount++
		}
	}
	assert.Equal(t, 1, claimedCount, "exactly one concurrent MarkUsed call should have claimed the token")
}

func TestPasswordResetTokenDeleteActiveForUser_RemovesActiveRow(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()

	created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, created.ID)

	err = passwordResetTokenRepo.DeleteActiveForUser(ctx, repoUserID.String())
	require.NoError(t, err)

	_, err = passwordResetTokenRepo.FindActiveByUserID(ctx, repoUserID.String())
	assert.ErrorIs(t, err, models.ErrNoActivePasswordResetToken)
}
