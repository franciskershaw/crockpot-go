package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeStaleDeleter implements both refreshTokenSweepRepository and staleTokenDeleter, so one fake
// covers all three of runTokenSweeper's dependency parameters.
type fakeStaleDeleter struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeStaleDeleter) DeleteAllStaleFamilies(ctx context.Context) error { return f.record() }
func (f *fakeStaleDeleter) DeleteAllStale(ctx context.Context) error         { return f.record() }

func (f *fakeStaleDeleter) record() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func (f *fakeStaleDeleter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func waitForCalls(t *testing.T, f *fakeStaleDeleter, min int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f.callCount() >= min {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for at least %d call(s), got %d", min, f.callCount())
}

func TestRunTokenSweeper_CallsAllThreeEachTick(t *testing.T) {
	refresh := &fakeStaleDeleter{}
	emailVerification := &fakeStaleDeleter{}
	passwordReset := &fakeStaleDeleter{}
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go runTokenSweeper(ctx, refresh, emailVerification, passwordReset, time.Millisecond, &wg)

	waitForCalls(t, refresh, 1, time.Second)
	waitForCalls(t, emailVerification, 1, time.Second)
	waitForCalls(t, passwordReset, 1, time.Second)

	cancel()
	wg.Wait()
}

func TestRunTokenSweeper_OneFailureDoesNotBlockOthers(t *testing.T) {
	refresh := &fakeStaleDeleter{err: errors.New("boom")}
	emailVerification := &fakeStaleDeleter{}
	passwordReset := &fakeStaleDeleter{}
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go runTokenSweeper(ctx, refresh, emailVerification, passwordReset, time.Millisecond, &wg)

	waitForCalls(t, emailVerification, 1, time.Second)
	waitForCalls(t, passwordReset, 1, time.Second)

	cancel()
	wg.Wait()

	if refresh.callCount() == 0 {
		t.Error("expected the failing refresh-token delete to still have been attempted")
	}
}

func TestRunTokenSweeper_StopsOnContextCancellation(t *testing.T) {
	refresh := &fakeStaleDeleter{}
	emailVerification := &fakeStaleDeleter{}
	passwordReset := &fakeStaleDeleter{}
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go runTokenSweeper(ctx, refresh, emailVerification, passwordReset, time.Hour, &wg)

	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected runTokenSweeper to exit promptly on context cancellation, even mid-interval")
	}
}
