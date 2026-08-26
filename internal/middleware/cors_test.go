package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
)

func newCORSRouter(allowedOrigin string, handlerCalled *bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.CORS(allowedOrigin))
	r.GET("/ping", func(c *gin.Context) {
		*handlerCalled = true
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
	return r
}

func TestCORS_AllowsConfiguredOrigin(t *testing.T) {
	var called bool
	r := newCORSRouter("http://localhost:5173", &called)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Error("expected handler to be called for the configured origin")
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("expected Access-Control-Allow-Origin=http://localhost:5173, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials=true, got %q", got)
	}
}

func TestCORS_OmitsHeadersForUnconfiguredOrigin(t *testing.T) {
	var called bool
	r := newCORSRouter("http://localhost:5173", &called)

	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Origin for an unconfigured origin, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("expected no Access-Control-Allow-Credentials for an unconfigured origin, got %q", got)
	}
}

func TestCORS_HandlesPreflightWithoutCallingHandler(t *testing.T) {
	var called bool
	r := newCORSRouter("http://localhost:5173", &called)

	req := httptest.NewRequest(http.MethodOptions, "/ping", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", w.Code)
	}
	if called {
		t.Error("expected handler NOT to be called for a preflight request")
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("expected Access-Control-Allow-Origin=http://localhost:5173 on preflight response, got %q", got)
	}
}
