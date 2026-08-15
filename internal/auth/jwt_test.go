package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testAccessSecret  = "test-secret-access"
	testRefreshSecret = "test-secret-refresh"
)

func TestGenerateAccessToken(t *testing.T) {
	token, err := GenerateAccessToken("test@example.com", "user-123", "FREE", testAccessSecret)
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

	if claims.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", claims.Email)
	}

	if claims.UserID != "user-123" {
		t.Errorf("expected userID user-123, got %s", claims.UserID)
	}

	if claims.Role != "FREE" {
		t.Errorf("expected Role FREE, got %s", claims.Role)
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	token, err := GenerateRefreshToken("user-123", "family-456", testRefreshSecret)
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

	if claims.Subject != "user-123" {
		t.Errorf("expected Subject user-123, got %s", claims.Subject)
	}

	if claims.FamilyID != "family-456" {
		t.Errorf("expected FamilyID family-456, got %s", claims.FamilyID)
	}
}

func TestGenerateAccessToken_EmptySecret(t *testing.T) {
	_, err := GenerateAccessToken("test@example.com", "user-123", "FREE", "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestGenerateRefreshToken_EmptySecret(t *testing.T) {
	_, err := GenerateRefreshToken("user-123", "family-456", "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateAccessToken_EmptySecret(t *testing.T) {
	token, err := GenerateAccessToken("test@example.com", "user-123", "FREE", testAccessSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, err = ValidateAccessToken(token, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateRefreshToken_EmptySecret(t *testing.T) {
	token, err := GenerateRefreshToken("user-123", "family-456", testRefreshSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, err = ValidateRefreshToken(token, "")
	if err == nil {
		t.Error("expected error for empty secret, got nil")
	}
}

func TestValidateAccessToken_ExpiredToken(t *testing.T) {
	claims := CustomClaims{
		Email:  "test@example.com",
		UserID: "user-123",
		Role:   "FREE",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(testAccessSecret))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = ValidateAccessToken(tokenString, testAccessSecret)
	if err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestValidateAccessToken_TamperedSignature(t *testing.T) {
	token, err := GenerateAccessToken("test@example.com", "user-123", "FREE", testAccessSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	tampered := token[:len(token)-4] + "abcd"

	_, err = ValidateAccessToken(tampered, testAccessSecret)
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}

func TestValidateRefreshToken_ExpiredToken(t *testing.T) {
	claims := RefreshClaims{
		FamilyID: "family-456",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-2 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(testRefreshSecret))
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	_, err = ValidateRefreshToken(tokenString, testRefreshSecret)
	if err == nil {
		t.Errorf("expected error for expired token, got nil")
	}
}

func TestValidateRefreshToken_TamperedSignature(t *testing.T) {
	token, err := GenerateRefreshToken("user-123", "family-456", testRefreshSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	tampered := token[:len(token)-4] + "abcd"

	_, err = ValidateRefreshToken(tampered, testRefreshSecret)
	if err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}
