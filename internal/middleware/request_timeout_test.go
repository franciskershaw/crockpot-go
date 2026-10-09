package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
)

func newRequestTimeoutRouter(d time.Duration, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestTimeout(d))
	r.GET("/slow", handler)
	return r
}

func TestRequestTimeout_SetsDeadlineOnRequestContext(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	r := newRequestTimeoutRouter(time.Minute, func(c *gin.Context) {
		deadline, hasDeadline = c.Request.Context().Deadline()
		c.Status(http.StatusOK)
	})

	start := time.Now()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))

	if !hasDeadline {
		t.Fatal("request context has no deadline")
	}
	if deadline.After(start.Add(time.Minute + time.Second)) {
		t.Errorf("deadline %v is later than the configured minute", deadline)
	}
}

func TestRequestTimeout_ReleasesBlockedHandlerAtDeadline(t *testing.T) {
	var ctxErr error
	r := newRequestTimeoutRouter(50*time.Millisecond, func(c *gin.Context) {
		select {
		case <-c.Request.Context().Done():
			ctxErr = c.Request.Context().Err()
		case <-time.After(2 * time.Second):
		}
		c.Status(http.StatusOK)
	})

	start := time.Now()
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/slow", nil))

	if !errors.Is(ctxErr, context.DeadlineExceeded) {
		t.Errorf("handler context error = %v, want deadline exceeded", ctxErr)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("handler released after %v, want about 50ms", elapsed)
	}
}
