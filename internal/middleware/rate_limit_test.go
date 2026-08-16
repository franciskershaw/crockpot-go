package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

// failingStore is a limiter.Store whose Get always errors, used to exercise
// RateLimit's error path (genuine store failure, distinct from limit-reached).
type failingStore struct{}

func (failingStore) Get(ctx context.Context, key string, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errors.New("store unavailable")
}

func (failingStore) Peek(ctx context.Context, key string, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errors.New("store unavailable")
}

func (failingStore) Reset(ctx context.Context, key string, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errors.New("store unavailable")
}

func (failingStore) Increment(ctx context.Context, key string, count int64, rate limiter.Rate) (limiter.Context, error) {
	return limiter.Context{}, errors.New("store unavailable")
}

func newRateLimitedRouter(rate limiter.Rate) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.NewRateLimitMiddleware(memory.NewStore(), rate).Handler())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	return r
}

func doRateLimitedGet(r *gin.Engine, remoteAddr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRateLimit_AllowsRequestsUnderLimit(t *testing.T) {
	r := newRateLimitedRouter(limiter.Rate{Period: time.Minute, Limit: 2})

	for i := 0; i < 2; i++ {
		if w := doRateLimitedGet(r, "1.2.3.4:1234"); w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}
}

func TestRateLimit_BlocksRequestsOverLimit(t *testing.T) {
	r := newRateLimitedRouter(limiter.Rate{Period: time.Minute, Limit: 2})

	for i := 0; i < 2; i++ {
		if w := doRateLimitedGet(r, "1.2.3.4:1234"); w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	w := doRateLimitedGet(r, "1.2.3.4:1234")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if got := w.Body.String(); got != `{"error":"rate limit exceeded"}` {
		t.Errorf("expected rate-limit-exceeded error body, got %s", got)
	}
	retryAfter, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil {
		t.Fatalf("expected Retry-After to be a number, got %q: %v", w.Header().Get("Retry-After"), err)
	}
	if retryAfter <= 0 || retryAfter > 60 {
		t.Errorf("expected Retry-After within the 60s period, got %d", retryAfter)
	}
}

func TestRateLimit_TracksDifferentIPsSeparately(t *testing.T) {
	r := newRateLimitedRouter(limiter.Rate{Period: time.Minute, Limit: 1})

	if w := doRateLimitedGet(r, "1.2.3.4:1234"); w.Code != http.StatusOK {
		t.Fatalf("IP A first request: expected 200, got %d", w.Code)
	}
	if w := doRateLimitedGet(r, "1.2.3.4:1234"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("IP A second request: expected 429, got %d", w.Code)
	}
	if w := doRateLimitedGet(r, "5.6.7.8:5678"); w.Code != http.StatusOK {
		t.Fatalf("IP B first request: expected 200 (separate bucket from IP A), got %d", w.Code)
	}
}

func TestRateLimit_StoreErrorReturnsCleanServerError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.NewRateLimitMiddleware(failingStore{}, limiter.Rate{Period: time.Minute, Limit: 2}).Handler())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	w := doRateLimitedGet(r, "1.2.3.4:1234")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	if got := w.Body.String(); got != `{"error":"internal server error"}` {
		t.Errorf("expected clean internal-server-error body, got %s", got)
	}
}
