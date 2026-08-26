package testutil

import (
	"fmt"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/auth"
)

const (
	TestAccessSecret     = "test-secret-access"
	TestRefreshSecret    = "test-secret-refresh"
	TestOAuthStateSecret = "test-secret-oauth-state"
	TestEmail            = "test@example.com"
	TestUserID           = "user-123"
	TestRole             = "FREE"
	TestFamilyID         = "family-456"
)

// AuthHeader builds a real, validly-signed "Bearer ..." access token header value.
func AuthHeader(t *testing.T, email, userID, role string) string {
	t.Helper()
	token, err := auth.GenerateAccessToken(email, userID, role, TestAccessSecret)
	if err != nil {
		t.Fatalf("failed to generate test access token: %v", err)
	}
	return fmt.Sprintf("Bearer %s", token)
}
