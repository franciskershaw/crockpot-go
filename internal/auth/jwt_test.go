package auth

import (
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/testutil"
	"github.com/golang-jwt/jwt/v5"
)

func mustGenerateAccessToken(t *testing.T, secret string) string {
	t.Helper()
	token, err := GenerateAccessToken(testutil.TestEmail, testutil.TestUserID, testutil.TestRole, secret)
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}
	return token
}

func mustGenerateRefreshToken(t *testing.T, secret string) string {
	t.Helper()
	token, err := GenerateRefreshToken(testutil.TestUserID, testutil.TestFamilyID, secret)
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
	token, err := GenerateAccessToken(testutil.TestEmail, testutil.TestUserID, testutil.TestRole, testutil.TestAccessSecret)
	if err != nil {
		t.Errorf("GenerateAccessToken failed: %v", err)
	}

	if token == "" {
		t.Errorf("expected token, got empty string")
	}

	claims, err := ValidateAccessToken(token, testutil.TestAccessSecret)
	if err != nil {
		t.Errorf("ValidateAccessToken failed: %v", err)
	}

	if claims.Email != testutil.TestEmail {
		t.Errorf("expected email %s, got %s", testutil.TestEmail, claims.Email)
	}

	if claims.UserID != testutil.TestUserID {
		t.Errorf("expected userID %s, got %s", testutil.TestUserID, claims.UserID)
	}

	if claims.Role != testutil.TestRole {
		t.Errorf("expected Role %s, got %s", testutil.TestRole, claims.Role)
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	token, err := GenerateRefreshToken(testutil.TestUserID, testutil.TestFamilyID, testutil.TestRefreshSecret)
	if err != nil {
		t.Errorf("GenerateRefreshToken failed: %v", err)
	}

	if token == "" {
		t.Errorf("expected token, got empty string")
	}

	claims, err := ValidateRefreshToken(token, testutil.TestRefreshSecret)
	if err != nil {
		t.Errorf("ValidateRefreshToken failed: %v", err)
	}

	if claims.Subject != testutil.TestUserID {
		t.Errorf("expected Subject %s, got %s", testutil.TestUserID, claims.Subject)
	}

	if claims.FamilyID != testutil.TestFamilyID {
		t.Errorf("expected FamilyID %s, got %s", testutil.TestFamilyID, claims.FamilyID)
	}
}

func TestGenerateAccessToken_EmptySecret(t *testing.T) {
	_, err := GenerateAccessToken(testutil.TestEmail, testutil.TestUserID, testutil.TestRole, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestGenerateRefreshToken_EmptySecret(t *testing.T) {
	_, err := GenerateRefreshToken(testutil.TestUserID, testutil.TestFamilyID, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateAccessToken_EmptySecret(t *testing.T) {
	token := mustGenerateAccessToken(t, testutil.TestAccessSecret)

	_, err := ValidateAccessToken(token, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateRefreshToken_EmptySecret(t *testing.T) {
	token := mustGenerateRefreshToken(t, testutil.TestRefreshSecret)

	_, err := ValidateRefreshToken(token, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateAccessToken_ExpiredToken(t *testing.T) {
	claims := CustomClaims{
		Email:  testutil.TestEmail,
		UserID: testutil.TestUserID,
		Role:   testutil.TestRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	tokenString := mustSign(t, claims, testutil.TestAccessSecret)

	_, err := ValidateAccessToken(tokenString, testutil.TestAccessSecret)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestValidateAccessToken_RejectsNonHS256Signature(t *testing.T) {
	claims := CustomClaims{
		Email:  testutil.TestEmail,
		UserID: testutil.TestUserID,
		Role:   testutil.TestRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	tokenString, err := token.SignedString([]byte(testutil.TestAccessSecret))
	if err != nil {
		t.Fatalf("failed to sign test token: %v", err)
	}

	_, err = ValidateAccessToken(tokenString, testutil.TestAccessSecret)
	if err == nil {
		t.Error("expected error for a token signed with HS384, got nil")
	}
}

func TestValidateAccessToken_TamperedSignature(t *testing.T) {
	token := mustGenerateAccessToken(t, testutil.TestAccessSecret)
	tampered := tamperSignature(token)

	_, err := ValidateAccessToken(tampered, testutil.TestAccessSecret)
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}

func TestValidateRefreshToken_ExpiredToken(t *testing.T) {
	claims := RefreshClaims{
		FamilyID: testutil.TestFamilyID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   testutil.TestUserID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	tokenString := mustSign(t, claims, testutil.TestRefreshSecret)

	_, err := ValidateRefreshToken(tokenString, testutil.TestRefreshSecret)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestValidateRefreshToken_TamperedSignature(t *testing.T) {
	token := mustGenerateRefreshToken(t, testutil.TestRefreshSecret)
	tampered := tamperSignature(token)

	_, err := ValidateRefreshToken(tampered, testutil.TestRefreshSecret)
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}
