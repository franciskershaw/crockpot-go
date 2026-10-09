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

const writeTimeout = 15 * time.Second

// requestTimeout ends a handler's DB work just before the server gives up writing its response.
const requestTimeout = writeTimeout - time.Second

type refreshTokenSweepRepository interface {
	DeleteAllStaleFamilies(ctx context.Context) error
}

// staleTokenDeleter is shared by email_verification_tokens and password_reset_tokens — both
// expose the same DeleteAllStale(ctx) error shape for their sweep.
type staleTokenDeleter interface {
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
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
	}
}

// runTokenSweeper deletes stale rows from all three token tables on each tick, independently —
// one table's failure is logged and doesn't block the other two, since they share no failure cause.
func runTokenSweeper(
	ctx context.Context,
	refreshTokens refreshTokenSweepRepository,
	emailVerificationTokens staleTokenDeleter,
	passwordResetTokens staleTokenDeleter,
	interval time.Duration,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	// Deploys restart the process more often than the interval, so a tick-only sweep might never run.
	sweepStaleTokens(ctx, refreshTokens, emailVerificationTokens, passwordResetTokens)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			sweepStaleTokens(ctx, refreshTokens, emailVerificationTokens, passwordResetTokens)
		case <-ctx.Done():
			return
		}
	}
}

func sweepStaleTokens(
	ctx context.Context,
	refreshTokens refreshTokenSweepRepository,
	emailVerificationTokens staleTokenDeleter,
	passwordResetTokens staleTokenDeleter,
) {
	if err := refreshTokens.DeleteAllStaleFamilies(ctx); err != nil {
		slog.Error("token sweeper: failed to delete stale refresh token families", "error", err)
	}
	if err := emailVerificationTokens.DeleteAllStale(ctx); err != nil {
		slog.Error("token sweeper: failed to delete stale email verification tokens", "error", err)
	}
	if err := passwordResetTokens.DeleteAllStale(ctx); err != nil {
		slog.Error("token sweeper: failed to delete stale password reset tokens", "error", err)
	}
}
