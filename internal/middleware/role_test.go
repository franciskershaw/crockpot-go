package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
)

func newRoleRouter(role string, roles ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if role != "" {
			c.Set("role", role)
		}
		c.Next()
	})
	r.Use(middleware.RequireRole(roles...))
	r.GET("/admin-only", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r
}

func doRoleRequest(r *gin.Engine) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequireRole(t *testing.T) {
	t.Run("matching role calls next", func(t *testing.T) {
		r := newRoleRouter("ADMIN", "ADMIN")

		w := doRoleRequest(r)

		if w.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	rejectCases := []struct {
		name string
		role string
	}{
		{"FREE role", "FREE"},
		{"PREMIUM role", "PREMIUM"},
		{"PRO role", "PRO"},
		{"missing role", ""},
	}

	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRoleRouter(tc.role, "ADMIN")

			w := doRoleRequest(r)

			if w.Code != http.StatusForbidden {
				t.Errorf("expected status %d, got %d", http.StatusForbidden, w.Code)
			}
			if got := w.Body.String(); got != `{"error":"forbidden"}` {
				t.Errorf("expected body %q, got %q", `{"error":"forbidden"}`, got)
			}
		})
	}
}
