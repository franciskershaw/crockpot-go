package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/db"
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
