package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const tokenSweepInterval = 24 * time.Hour

type refreshTokenSweepRepository interface {
	DeleteAllStaleFamilies(ctx context.Context) error
}

type emailVerificationTokenSweepRepository interface {
	DeleteAllStale(ctx context.Context) error
}

type passwordResetTokenSweepRepository interface {
	DeleteAllStale(ctx context.Context) error
}

// configureGinMode returns gin.ReleaseMode if env == "production", else gin.DebugMode.
func configureGinMode(env string) string {
	if env == "production" {
		return gin.ReleaseMode
	}
	return gin.DebugMode
}

// newHTTPServer builds *http.Server lifecycle timeouts, replacing gin's Run() shorthand.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// runTokenSweeper deletes stale rows from all three token tables on each tick, independently —
// one table's failure is logged and doesn't block the other two, since they share no failure cause.
func runTokenSweeper(
	ctx context.Context,
	refreshTokens refreshTokenSweepRepository,
	emailVerificationTokens emailVerificationTokenSweepRepository,
	passwordResetTokens passwordResetTokenSweepRepository,
	interval time.Duration,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := refreshTokens.DeleteAllStaleFamilies(ctx); err != nil {
				slog.Error("token sweeper: failed to delete stale refresh token families", "error", err)
			}
			if err := emailVerificationTokens.DeleteAllStale(ctx); err != nil {
				slog.Error("token sweeper: failed to delete stale email verification tokens", "error", err)
			}
			if err := passwordResetTokens.DeleteAllStale(ctx); err != nil {
				slog.Error("token sweeper: failed to delete stale password reset tokens", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
