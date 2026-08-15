package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAccessSecret  = "test-secret-access"
	testRefreshSecret = "test-secret-refresh"
	testEmail         = "test@example.com"
	testUserID        = "user-123"
	testRole          = "FREE"
	testFamilyID      = "family-456"
)

func mustGenerateAccessToken(t *testing.T, secret string) string {
	t.Helper()
	token, err := GenerateAccessToken(testEmail, testUserID, testRole, secret)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}
	return token
}

func mustGenerateRefreshToken(t *testing.T, secret string) string {
	t.Helper()
	token, err := GenerateRefreshToken(testUserID, testFamilyID, secret)
	if err != nil {
		t.Fatalf("failed to generate refresh token: %v", err)
	}
	return token
}

func mustSign(t *testing.T, claims jwt.Claims, secret string) string {
	t.Helper()
	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}
	return tokenString
}

// tamperSignature corrupts a valid token's signature segment, simulating an
// attacker-modified token rather than one merely signed with the wrong key.
func tamperSignature(token string) string {
	return token[:len(token)-4] + "abcd"
}

func TestGenerateAccessToken(t *testing.T) {
	token, err := GenerateAccessToken(testEmail, testUserID, testRole, testAccessSecret)
	if err != nil {
		t.Errorf("GenerateAccessToken failed: %v", err)
	}

	if token == "" {
		t.Errorf("expected token, got empty string")
	}

	claims, err := ValidateAccessToken(token, testAccessSecret)
	if err != nil {
		t.Errorf("ValidateAccessToken failed: %v", err)
	}

	if claims.Email != testEmail {
		t.Errorf("expected email %s, got %s", testEmail, claims.Email)
	}

	if claims.UserID != testUserID {
		t.Errorf("expected userID %s, got %s", testUserID, claims.UserID)
	}

	if claims.Role != testRole {
		t.Errorf("expected Role %s, got %s", testRole, claims.Role)
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	token, err := GenerateRefreshToken(testUserID, testFamilyID, testRefreshSecret)
	if err != nil {
		t.Errorf("GenerateRefreshToken failed: %v", err)
	}

	if token == "" {
		t.Errorf("expected token, got empty string")
	}

	claims, err := ValidateRefreshToken(token, testRefreshSecret)
	if err != nil {
		t.Errorf("ValidateRefreshToken failed: %v", err)
	}

	if claims.Subject != testUserID {
		t.Errorf("expected Subject %s, got %s", testUserID, claims.Subject)
	}

	if claims.FamilyID != testFamilyID {
		t.Errorf("expected FamilyID %s, got %s", testFamilyID, claims.FamilyID)
	}
}

func TestGenerateAccessToken_EmptySecret(t *testing.T) {
	_, err := GenerateAccessToken(testEmail, testUserID, testRole, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestGenerateRefreshToken_EmptySecret(t *testing.T) {
	_, err := GenerateRefreshToken(testUserID, testFamilyID, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateAccessToken_EmptySecret(t *testing.T) {
	token := mustGenerateAccessToken(t, testAccessSecret)

	_, err := ValidateAccessToken(token, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateRefreshToken_EmptySecret(t *testing.T) {
	token := mustGenerateRefreshToken(t, testRefreshSecret)

	_, err := ValidateRefreshToken(token, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateAccessToken_ExpiredToken(t *testing.T) {
	claims := CustomClaims{
		Email:  testEmail,
		UserID: testUserID,
		Role:   testRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	tokenString := mustSign(t, claims, testAccessSecret)

	_, err := ValidateAccessToken(tokenString, testAccessSecret)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestValidateAccessToken_TamperedSignature(t *testing.T) {
	token := mustGenerateAccessToken(t, testAccessSecret)
	tampered := tamperSignature(token)

	_, err := ValidateAccessToken(tampered, testAccessSecret)
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}

func TestValidateRefreshToken_ExpiredToken(t *testing.T) {
	claims := RefreshClaims{
		FamilyID: testFamilyID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   testUserID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	tokenString := mustSign(t, claims, testRefreshSecret)

	_, err := ValidateRefreshToken(tokenString, testRefreshSecret)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestValidateRefreshToken_TamperedSignature(t *testing.T) {
	token := mustGenerateRefreshToken(t, testRefreshSecret)
	tampered := tamperSignature(token)

	_, err := ValidateRefreshToken(tampered, testRefreshSecret)
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}
