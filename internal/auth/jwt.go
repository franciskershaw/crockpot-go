package auth

import "github.com/golang-jwt/jwt/v5"

type CustomClaims struct {
	Email  string `json:"email"`
	UserID string `json:"userID"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// RefreshClaims carries the token family ID so any presented token, however stale, can be traced back to its family.
type RefreshClaims struct {
	FamilyID string `json:"familyId"`
	jwt.RegisteredClaims
}

func GenerateAccessToken(email, userID, role, secret string) (string, error) {
	return "stub-token", nil
}

func GenerateRefreshToken(userID, familyID, secret string) (string, error) {
	return "stub-token", nil
}

func signToken(claims jwt.Claims, secret string) (string, error) {
	return "", nil
}

func ValidateAccessToken(tokenString string, secret string) (*CustomClaims, error) {
	return &CustomClaims{}, nil
}

func ValidateRefreshToken(tokenString string, secret string) (*RefreshClaims, error) {
	return &RefreshClaims{}, nil
}

func parseToken(tokenString string, secret string, claims jwt.Claims) error {
	return nil
}
