package main

import (
	// "context"
	// "log/slog"
	"net/http"
	// "sync"
	"time"

	"github.com/gin-gonic/gin"
)

// type tokenSweepRepository interface {
// 	DeleteAllStaleFamilies(ctx context.Context) error
// }

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

// func runTokenSweeper(ctx context.Context, repo tokenSweepRepository, interval time.Duration, wg *sync.WaitGroup) {
// 	defer wg.Done()

// 	ticker := time.NewTicker(interval)
// 	defer ticker.Stop()

// 	for {
// 		select {
// 		case <-ticker.C:
// 			if err := repo.DeleteAllStaleFamilies(ctx); err != nil {
// 				slog.Error("token sweeper: failed to delete stale refresh token families", "error", err)
// 			}
// 		case <-ctx.Done():
// 			return
// 		}
// 	}
// }
