package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

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

// Expiry times for the tokens
const (
	accessTokenExpiry  = 15 * time.Minute
	refreshTokenExpiry = 7 * 24 * time.Hour
)

func GenerateAccessToken(email, userID, role, secret string) (string, error) {
	claims := CustomClaims{
		Email:  email,
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return signToken(claims, secret)
}

func GenerateRefreshToken(userID, familyID, secret string) (string, error) {
	claims := RefreshClaims{
		FamilyID: familyID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: userID,
			// ID avoids collisions when two tokens are issued for the same
			// family within the same second (other claims are second-precision).
			ID:        uuid.NewString(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(refreshTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return signToken(claims, secret)
}

func signToken(claims jwt.Claims, secret string) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("secret key not set")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateAccessToken(tokenString string, secret string) (*CustomClaims, error) {
	claims := &CustomClaims{}
	err := parseToken(tokenString, secret, claims)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func ValidateRefreshToken(tokenString string, secret string) (*RefreshClaims, error) {
	claims := &RefreshClaims{}
	err := parseToken(tokenString, secret, claims)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func parseToken(tokenString string, secret string, claims jwt.Claims) error {
	if secret == "" {
		return fmt.Errorf("secret key not set")
	}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return fmt.Errorf("failed to parse token: %w", err)
	}

	if !token.Valid {
		return fmt.Errorf("invalid token")
	}

	return nil
}
