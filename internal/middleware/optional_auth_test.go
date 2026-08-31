package middleware_test

import (
	"net/http"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/franciskershaw/crockpot-go/internal/testutil"
	"github.com/gin-gonic/gin"
)

func newOptionalAuthRouter(secret string, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.OptionalAuthMiddleware(secret))
	r.GET("/protected", handler)
	return r
}

func TestOptionalAuthMiddleware_AnonymousPassesThroughWithoutClaims(t *testing.T) {
	cases := []struct {
		name       string
		authHeader string
	}{
		{"no header", ""},
		{"malformed header", "Basic sometoken"},
		{"invalid token", "Bearer not-a-valid-token"},
		{"expired token", "Bearer " + expiredAccessToken(t, testutil.TestAccessSecret)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var called bool
			var hasUserID, hasEmail, hasRole bool
			r := newOptionalAuthRouter(testutil.TestAccessSecret, func(c *gin.Context) {
				called = true
				_, hasUserID = c.Get("userID")
				_, hasEmail = c.Get("email")
				_, hasRole = c.Get("role")
				c.Status(http.StatusOK)
			})

			w := doRequest(r, tc.authHeader)

			if !called {
				t.Fatal("expected handler to be called")
			}
			if w.Code == http.StatusUnauthorized {
				t.Errorf("optional auth must never 401, got %d", w.Code)
			}
			if hasUserID || hasEmail || hasRole {
				t.Errorf("expected no claims in context, got userID=%v email=%v role=%v", hasUserID, hasEmail, hasRole)
			}
		})
	}
}

func TestOptionalAuthMiddleware_ValidTokenSetsClaims(t *testing.T) {
	token, err := auth.GenerateAccessToken(testutil.TestEmail, testutil.TestUserID, testutil.TestRole, testutil.TestAccessSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	var gotUserID, gotEmail, gotRole string
	r := newOptionalAuthRouter(testutil.TestAccessSecret, func(c *gin.Context) {
		gotUserID = c.GetString("userID")
		gotEmail = c.GetString("email")
		gotRole = c.GetString("role")
		c.Status(http.StatusOK)
	})

	w := doRequest(r, "Bearer "+token)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if gotUserID != testutil.TestUserID {
		t.Errorf("expected userID %s, got %s", testutil.TestUserID, gotUserID)
	}
	if gotEmail != testutil.TestEmail {
		t.Errorf("expected email %s, got %s", testutil.TestEmail, gotEmail)
	}
	if gotRole != testutil.TestRole {
		t.Errorf("expected role %s, got %s", testutil.TestRole, gotRole)
	}
}
