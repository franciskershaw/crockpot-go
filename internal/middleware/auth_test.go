package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/franciskershaw/crockpot-go/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func newAuthRouter(secret string, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.AuthMiddleware(secret))
	r.GET("/protected", handler)
	return r
}

func doRequest(r *gin.Engine, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func expiredAccessToken(t *testing.T, secret string) string {
	t.Helper()
	claims := auth.CustomClaims{
		Email:  testutil.TestEmail,
		UserID: testutil.TestUserID,
		Role:   testutil.TestRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return tokenString
}

func TestAuthMiddleware(t *testing.T) {
	rejectCases := []struct {
		name       string
		authHeader string
	}{
		{"missing header", ""},
		{"malformed header", "Basic sometoken"},
		{"invalid token", "Bearer not-a-valid-token"},
		{"expired token", "Bearer " + expiredAccessToken(t, testutil.TestAccessSecret)},
	}

	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			var called bool
			r := newAuthRouter(testutil.TestAccessSecret, func(c *gin.Context) {
				called = true
				c.Status(http.StatusOK)
			})

			w := doRequest(r, tc.authHeader)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
			}
			if called {
				t.Error("expected handler NOT to be called")
			}
		})
	}

	t.Run("valid token sets context", func(t *testing.T) {
		token, err := auth.GenerateAccessToken(testutil.TestEmail, testutil.TestUserID, testutil.TestRole, testutil.TestAccessSecret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		var gotUserID, gotEmail, gotRole string
		r := newAuthRouter(testutil.TestAccessSecret, func(c *gin.Context) {
			if v, ok := c.Get("userID"); ok {
				gotUserID, _ = v.(string)
			}
			if v, ok := c.Get("email"); ok {
				gotEmail, _ = v.(string)
			}
			if v, ok := c.Get("role"); ok {
				gotRole, _ = v.(string)
			}
			c.Status(http.StatusOK)
		})

		w := doRequest(r, "Bearer "+token)

		if w.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
		}
		if gotUserID != testutil.TestUserID {
			t.Errorf("expected userID %s in context, got %s", testutil.TestUserID, gotUserID)
		}
		if gotEmail != testutil.TestEmail {
			t.Errorf("expected email %s in context, got %s", testutil.TestEmail, gotEmail)
		}
		if gotRole != testutil.TestRole {
			t.Errorf("expected role %s in context, got %s", testutil.TestRole, gotRole)
		}
	})
}
