package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/franciskershaw/crockpot-go/internal/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testAuthSecret = "test-secret-access"
	testEmail      = "test@example.com"
	testUserID     = "user-123"
	testRole       = "FREE"
)

func newAuthRouter(secret string, handlerCalled *bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.AuthMiddleware(secret))
	r.GET("/protected", func(c *gin.Context) {
		*handlerCalled = true
		c.Status(http.StatusOK)
	})
	return r
}

// Separate from newAuthRouter because this is the only case that needs to read back context values, not just whether the handler ran.
func newAuthRouterCapturingContext(secret string) (router *gin.Engine, gotUserID *string, gotEmail *string) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.AuthMiddleware(secret))

	var userID, email string
	r.GET("/protected", func(c *gin.Context) {
		if v, ok := c.Get("userID"); ok {
			userID, _ = v.(string)
		}
		if v, ok := c.Get("email"); ok {
			email, _ = v.(string)
		}
		c.Status(http.StatusOK)
	})
	return r, &userID, &email
}

func TestAuthMiddleware(t *testing.T) {
	t.Run("missing header", func(t *testing.T) {
		var called bool
		r := newAuthRouter(testAuthSecret, &called)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
		if called {
			t.Error("expected handler NOT to be called when the Authorization header is missing")
		}
	})

	t.Run("malformed header", func(t *testing.T) {
		var called bool
		r := newAuthRouter(testAuthSecret, &called)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Basic sometoken")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
		if called {
			t.Error("expected handler NOT to be called for a malformed Authorization header")
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		var called bool
		r := newAuthRouter(testAuthSecret, &called)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer not-a-valid-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
		if called {
			t.Error("expected handler NOT to be called for an invalid token")
		}
	})

	t.Run("expired token", func(t *testing.T) {
		claims := auth.CustomClaims{
			Email:  testEmail,
			UserID: testUserID,
			Role:   testRole,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
			},
		}
		tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testAuthSecret))
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}

		var called bool
		r := newAuthRouter(testAuthSecret, &called)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
		}
		if called {
			t.Error("expected handler NOT to be called for an expired token")
		}
	})

	t.Run("valid token sets context", func(t *testing.T) {
		token, err := auth.GenerateAccessToken(testEmail, testUserID, testRole, testAuthSecret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		r, gotUserID, gotEmail := newAuthRouterCapturingContext(testAuthSecret)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
		}
		if *gotUserID != testUserID {
			t.Errorf("expected userID %s in context, got %s", testUserID, *gotUserID)
		}
		if *gotEmail != testEmail {
			t.Errorf("expected email %s in context, got %s", testEmail, *gotEmail)
		}
	})
}
