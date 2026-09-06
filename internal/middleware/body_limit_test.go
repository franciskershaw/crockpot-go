package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
)

func newBodyLimitRouter(maxBytes int64, handlerCalled *bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BodySizeLimit(maxBytes))
	r.POST("/ping", func(c *gin.Context) {
		*handlerCalled = true
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	return r
}

func TestBodySizeLimit_RejectsOversizedDeclaredContentLength(t *testing.T) {
	var called bool
	r := newBodyLimitRouter(10, &called)

	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(strings.Repeat("a", 20)))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", w.Code)
	}
	if called {
		t.Error("expected handler NOT to be called for an oversized body")
	}
	if got := w.Body.String(); !strings.Contains(got, "request_too_large") {
		t.Errorf("expected request_too_large error code, got %q", got)
	}
}

func TestBodySizeLimit_AllowsBodyWithinLimit(t *testing.T) {
	var called bool
	r := newBodyLimitRouter(1024, &called)

	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader("small body"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Error("expected handler to be called for an in-limit body")
	}
}

func TestBodySizeLimit_CapsActualReadWhenContentLengthUnderstated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BodySizeLimit(10))
	var readErr error
	r.POST("/ping", func(c *gin.Context) {
		_, readErr = io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})

	req := httptest.NewRequest(http.MethodPost, "/ping", strings.NewReader(strings.Repeat("a", 20)))
	req.ContentLength = -1 // unknown length, e.g. chunked — bypasses the declared-length fast path
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if readErr == nil {
		t.Fatal("expected reading past maxBytes to error when Content-Length understates the real body")
	}
}
