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

func newBaseRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.AuthMiddleware(secret))
	return r
}

func newAuthRouter(secret string, handlerCalled *bool) *gin.Engine {
	r := newBaseRouter(secret)
	r.GET("/protected", func(c *gin.Context) {
		*handlerCalled = true
		c.Status(http.StatusOK)
	})
	return r
}

// Separate from newAuthRouter because this is the only case that needs to read back context values, not just whether the handler ran.
func newAuthRouterCapturingContext(secret string) (router *gin.Engine, gotUserID *string, gotEmail *string) {
	r := newBaseRouter(secret)
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
		Email:  testEmail,
		UserID: testUserID,
		Role:   testRole,
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
		{"expired token", "Bearer " + expiredAccessToken(t, testAuthSecret)},
	}

	for _, tc := range rejectCases {
		t.Run(tc.name, func(t *testing.T) {
			var called bool
			r := newAuthRouter(testAuthSecret, &called)

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
		token, err := auth.GenerateAccessToken(testEmail, testUserID, testRole, testAuthSecret)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		r, gotUserID, gotEmail := newAuthRouterCapturingContext(testAuthSecret)

		w := doRequest(r, "Bearer "+token)

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
