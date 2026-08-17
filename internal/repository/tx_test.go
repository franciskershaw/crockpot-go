package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithinTx_CommitsWritesOnSuccess(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()
	var createdID string

	err := transactor.WithinTx(ctx, func(ctx context.Context) error {
		created, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour))
		if err != nil {
			return err
		}
		createdID = created.ID.String()
		return nil
	})
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM password_reset_tokens WHERE id = $1`, createdID)

	found, err := passwordResetTokenRepo.FindActiveByTokenHash(ctx, hash)
	require.NoError(t, err)
	assert.Equal(t, createdID, found.ID.String())
}

func TestWithinTx_RollsBackWritesOnError(t *testing.T) {
	ctx := context.Background()
	hash := "repo-test-hash-" + uuid.NewString()
	sentinelErr := errors.New("boom")

	err := transactor.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := passwordResetTokenRepo.Create(ctx, repoUserID.String(), hash, time.Now().Add(time.Hour)); err != nil {
			return err
		}
		return sentinelErr
	})
	require.ErrorIs(t, err, sentinelErr)

	_, err = passwordResetTokenRepo.FindActiveByTokenHash(ctx, hash)
	assert.ErrorIs(t, err, models.ErrNoActivePasswordResetToken, "the create inside the failed transaction should have been rolled back")
}

func TestAcquireUserLock_SerializesConcurrentTransactionsForSameUser(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	var intervals [][2]time.Time
	errs := make([]error, 2)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = transactor.WithinTx(ctx, func(ctx context.Context) error {
				if err := passwordResetTokenRepo.AcquireUserLock(ctx, repoUserID.String()); err != nil {
					return err
				}
				start := time.Now()
				time.Sleep(200 * time.Millisecond)
				end := time.Now()
				mu.Lock()
				intervals = append(intervals, [2]time.Time{start, end})
				mu.Unlock()
				return nil
			})
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, intervals, 2)

	a, b := intervals[0], intervals[1]
	overlap := a[0].Before(b[1]) && b[0].Before(a[1])
	assert.False(t, overlap, "concurrent AcquireUserLock holders for the same user should not run concurrently")
}
