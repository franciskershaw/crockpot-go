package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

const testStateSecret = "test-oauth-state-secret"

// newTestOAuthManager avoids the live OIDC discovery call NewGoogleOAuthManager
// makes; verifier is nil, safe as long as no test calls VerifyIDToken.
func newTestOAuthManager(clientID string) *GoogleOAuthManager {
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/callback",
		Scopes: []string{
			oidc.ScopeOpenID,
			"email",
			"profile",
		},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
	}
	return newGoogleOAuthManager(config, nil, testStateSecret)
}

func TestGenerateState(t *testing.T) {
	manager := newTestOAuthManager("test-id")

	state, err := manager.GenerateState()
	if err != nil {
		t.Fatalf("GenerateState returned unexpected error: %v", err)
	}
	if state == "" {
		t.Fatal("GenerateState returned empty string")
	}

	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(state, claims, func(token *jwt.Token) (any, error) {
		return []byte(testStateSecret), nil
	})
	if err != nil || !token.Valid {
		t.Fatalf("GenerateState did not return a validly-signed token: %v", err)
	}

	if claims.ExpiresAt == nil {
		t.Fatal("expected exp claim to be set")
	}
	gotTTL := time.Until(claims.ExpiresAt.Time)
	if gotTTL < 9*time.Minute || gotTTL > 10*time.Minute {
		t.Errorf("expected exp claim ~10m from now, got %v", gotTTL)
	}
}

func TestValidateState_ValidToken(t *testing.T) {
	manager := newTestOAuthManager("test-id")

	state, err := manager.GenerateState()
	if err != nil {
		t.Fatalf("GenerateState returned unexpected error: %v", err)
	}

	if !manager.ValidateState(state) {
		t.Error("ValidateState returned false for a freshly generated state")
	}
}

func TestValidateState_InvalidSignature(t *testing.T) {
	manager := newTestOAuthManager("test-id")

	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("wrong-secret"))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	if manager.ValidateState(signed) {
		t.Error("ValidateState returned true for a token signed with the wrong secret")
	}
}

func TestValidateState_Expired(t *testing.T) {
	manager := newTestOAuthManager("test-id")

	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testStateSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	if manager.ValidateState(signed) {
		t.Error("ValidateState returned true for an expired state")
	}
}

func TestGetAuthURL(t *testing.T) {
	manager := newTestOAuthManager("test-id")

	authURL := manager.GetAuthURL("test-state")

	if authURL == "" {
		t.Error("GetAuthURL returned empty string")
	}

	if !strings.Contains(authURL, "client_id=test-id") {
		t.Error("auth URL missing client_id parameter")
	}

	if !strings.Contains(authURL, "redirect_uri") {
		t.Error("auth URL missing redirect_uri parameter")
	}

	if !strings.Contains(authURL, "state=test-state") {
		t.Error("auth URL missing state parameter")
	}
}
