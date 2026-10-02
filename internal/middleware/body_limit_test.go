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
	r.Use(middleware.BodySizeLimit(maxBytes, nil))
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
	r.Use(middleware.BodySizeLimit(10, nil))
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

func newOverrideRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BodySizeLimit(10, map[string]int64{
		"POST /recipes":      100,
		"PATCH /recipes/:id": 100,
	}))
	read := func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "read_failed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	}
	r.POST("/recipes", read)
	r.PATCH("/recipes/:id", read)
	r.PUT("/recipes/:id", read)
	r.POST("/other", read)
	return r
}

func TestBodySizeLimit_Overrides(t *testing.T) {
	tests := []struct {
		name          string
		method, path  string
		size          int
		understateLen bool
		want          int
	}{
		{"override route allows a body above the default", http.MethodPost, "/recipes", 50, false, http.StatusOK},
		{"override matches the route pattern, not the literal path", http.MethodPatch, "/recipes/abc-123", 50, false, http.StatusOK},
		{"override route still rejects above its own cap", http.MethodPost, "/recipes", 150, false, http.StatusRequestEntityTooLarge},
		{"override caps the actual read when Content-Length is understated", http.MethodPost, "/recipes", 150, true, http.StatusRequestEntityTooLarge},
		{"another method on the same pattern keeps the default", http.MethodPut, "/recipes/abc-123", 50, false, http.StatusRequestEntityTooLarge},
		{"other routes keep the default", http.MethodPost, "/other", 50, false, http.StatusRequestEntityTooLarge},
		{"unmatched routes keep the default", http.MethodPost, "/nope", 50, false, http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newOverrideRouter()
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(strings.Repeat("a", tt.size)))
			if tt.understateLen {
				req.ContentLength = -1
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.want {
				t.Errorf("status = %d, want %d (body %q)", w.Code, tt.want, w.Body.String())
			}
		})
	}
}
