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

func TestCreateFamily_PersistsRow(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	hash := "repo-test-hash-" + uuid.NewString()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	family, err := refreshTokenRepo.CreateFamily(ctx, id, repoUserID.String(), hash, expiresAt)
	require.NoError(t, err)
	require.NotNil(t, family)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, family.ID)

	assert.Equal(t, id, family.ID.String())
	assert.Equal(t, repoUserID, family.UserID)
	assert.Equal(t, hash, family.TokenHash)
	assert.Nil(t, family.PreviousTokenHash)
	assert.Nil(t, family.RevokedAt)
	assert.WithinDuration(t, expiresAt, family.ExpiresAt, time.Second)
}

func TestDeleteStaleFamiliesForUser_DeletesExpiredAndRevoked_KeepsActive(t *testing.T) {
	ctx := context.Background()

	expiredID := uuid.NewString()
	revokedID := uuid.NewString()
	activeID := uuid.NewString()

	_, err := refreshTokenRepo.CreateFamily(ctx, expiredID, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(-time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, expiredID)

	_, err = refreshTokenRepo.CreateFamily(ctx, revokedID, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, revokedID)
	_, err = db.DB.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = CURRENT_TIMESTAMP WHERE id = $1`, revokedID)
	require.NoError(t, err)

	_, err = refreshTokenRepo.CreateFamily(ctx, activeID, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, activeID)

	err = refreshTokenRepo.DeleteStaleFamiliesForUser(ctx, repoUserID.String())
	require.NoError(t, err)

	var remainingIDs []string
	rows, err := db.DB.Query(ctx, `SELECT id FROM refresh_tokens WHERE user_id = $1`, repoUserID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		remainingIDs = append(remainingIDs, id.String())
	}

	assert.NotContains(t, remainingIDs, expiredID, "expired family should have been deleted")
	assert.NotContains(t, remainingIDs, revokedID, "revoked family should have been deleted")
	assert.Contains(t, remainingIDs, activeID, "active family should not have been deleted")
}

func TestRevokeAllFamiliesForUser_RevokesLive_LeavesAlreadyRevokedAndExpiredUntouched(t *testing.T) {
	ctx := context.Background()

	liveAID := uuid.NewString()
	liveBID := uuid.NewString()
	alreadyRevokedID := uuid.NewString()
	expiredUnrevokedID := uuid.NewString()
	fixedPastRevokedAt := time.Now().Add(-24 * time.Hour)

	_, err := refreshTokenRepo.CreateFamily(ctx, liveAID, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, liveAID)

	_, err = refreshTokenRepo.CreateFamily(ctx, liveBID, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, liveBID)

	_, err = refreshTokenRepo.CreateFamily(ctx, alreadyRevokedID, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, alreadyRevokedID)
	_, err = db.DB.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = $2 WHERE id = $1`, alreadyRevokedID, fixedPastRevokedAt)
	require.NoError(t, err)

	// Expired but never revoked — CreateFamily rejects a past expiry, so insert directly.
	_, err = db.DB.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		expiredUnrevokedID, repoUserID, "repo-test-hash-"+uuid.NewString(), time.Now().Add(-time.Hour),
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, expiredUnrevokedID)

	err = refreshTokenRepo.RevokeAllFamiliesForUser(ctx, repoUserID.String())
	require.NoError(t, err)

	assertRevokedAt := func(id string) *time.Time {
		var revokedAt *time.Time
		row := db.DB.QueryRow(ctx, `SELECT revoked_at FROM refresh_tokens WHERE id = $1`, id)
		require.NoError(t, row.Scan(&revokedAt))
		return revokedAt
	}

	assert.NotNil(t, assertRevokedAt(liveAID), "live family A should have been revoked")
	assert.NotNil(t, assertRevokedAt(liveBID), "live family B should have been revoked")

	revokedAt := assertRevokedAt(alreadyRevokedID)
	require.NotNil(t, revokedAt)
	assert.WithinDuration(t, fixedPastRevokedAt, *revokedAt, time.Second, "already-revoked family's revoked_at should not be overwritten")

	assert.Nil(t, assertRevokedAt(expiredUnrevokedID), "expired-but-unrevoked family should not be touched")
}

func TestRotateFamily_ShiftsCurrentIntoPrevious(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	originalHash := "repo-test-hash-" + uuid.NewString()
	newHash := "repo-test-hash-" + uuid.NewString()
	newExpiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, err := refreshTokenRepo.CreateFamily(ctx, id, repoUserID.String(), originalHash, time.Now().Add(time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, id)

	err = refreshTokenRepo.RotateFamily(ctx, id, newHash, newExpiresAt)
	require.NoError(t, err)

	var tokenHash string
	var previousTokenHash *string
	var previousTokenRotatedAt *time.Time
	var expiresAt time.Time
	row := db.DB.QueryRow(ctx, `SELECT token_hash, previous_token_hash, previous_token_rotated_at, expires_at FROM refresh_tokens WHERE id = $1`, id)
	require.NoError(t, row.Scan(&tokenHash, &previousTokenHash, &previousTokenRotatedAt, &expiresAt))

	assert.Equal(t, newHash, tokenHash)
	require.NotNil(t, previousTokenHash)
	assert.Equal(t, originalHash, *previousTokenHash)
	require.NotNil(t, previousTokenRotatedAt)
	assert.WithinDuration(t, time.Now(), *previousTokenRotatedAt, 5*time.Second)
	assert.WithinDuration(t, newExpiresAt, expiresAt, time.Second)
}

func TestFindFamilyByID_ReturnsFamily(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	hash := "repo-test-hash-" + uuid.NewString()

	_, err := refreshTokenRepo.CreateFamily(ctx, id, repoUserID.String(), hash, time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, id)

	family, err := refreshTokenRepo.FindFamilyByID(ctx, id, repoUserID.String())
	require.NoError(t, err)
	require.NotNil(t, family)
	assert.Equal(t, id, family.ID.String())
	assert.Equal(t, hash, family.TokenHash)
}

func TestFindFamilyByID_ReturnsErrRefreshTokenFamilyNotFoundForUnknownID(t *testing.T) {
	ctx := context.Background()

	family, err := refreshTokenRepo.FindFamilyByID(ctx, uuid.NewString(), repoUserID.String())
	assert.Nil(t, family)
	assert.ErrorIs(t, err, models.ErrRefreshTokenFamilyNotFound)
}

func TestFindFamilyByID_ReturnsErrRefreshTokenFamilyNotFoundForWrongUser(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	otherUserID := uuid.New()

	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, google_id, email) VALUES ($1, $2, $3)`,
		otherUserID, "repo-test-google-"+otherUserID.String(), "repo-test-"+otherUserID.String()+"@example.com",
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, otherUserID)

	_, err = refreshTokenRepo.CreateFamily(ctx, id, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, id)

	family, err := refreshTokenRepo.FindFamilyByID(ctx, id, otherUserID.String())
	assert.Nil(t, family)
	assert.ErrorIs(t, err, models.ErrRefreshTokenFamilyNotFound)
}

func TestRevokeFamily_SetsRevokedAt(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()

	_, err := refreshTokenRepo.CreateFamily(ctx, id, repoUserID.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(7*24*time.Hour))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM refresh_tokens WHERE id = $1`, id)

	err = refreshTokenRepo.RevokeFamily(ctx, id)
	require.NoError(t, err)

	var revokedAt *time.Time
	row := db.DB.QueryRow(ctx, `SELECT revoked_at FROM refresh_tokens WHERE id = $1`, id)
	require.NoError(t, row.Scan(&revokedAt))
	assert.NotNil(t, revokedAt)
}
