package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

const oauthStateTTL = 10 * time.Minute

type GoogleOAuthManager struct {
	config      *oauth2.Config
	verifier    *oidc.IDTokenVerifier
	stateSecret string
}

type IDTokenClaims struct {
	Email       string `json:"email"`
	GoogleID    string `json:"sub"`
	DisplayName string `json:"name"`
	AvatarURL   string `json:"picture"`
}

// NewGoogleOAuthManager initializes the Google OAuth2 manager
func NewGoogleOAuthManager(clientID, clientSecret, redirectURL, stateSecret string) (*GoogleOAuthManager, error) {
	ctx := context.Background()

	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC provider: %w", err)
	}

	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			oidc.ScopeOpenID,
			"email",
			"profile",
		},
		Endpoint: provider.Endpoint(),
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	return newGoogleOAuthManager(config, verifier, stateSecret), nil
}

// newGoogleOAuthManager lets tests inject a fake config, skipping NewGoogleOAuthManager's live discovery call.
func newGoogleOAuthManager(config *oauth2.Config, verifier *oidc.IDTokenVerifier, stateSecret string) *GoogleOAuthManager {
	return &GoogleOAuthManager{
		config:      config,
		verifier:    verifier,
		stateSecret: stateSecret,
	}
}

// GenerateState creates a signed, short-lived state token for CSRF protection.
func (g *GoogleOAuthManager) GenerateState() (string, error) {
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(oauthStateTTL)),
	}
	return signToken(claims, g.stateSecret)
}

// ValidateState verifies the state token's signature and expiry.
func (g *GoogleOAuthManager) ValidateState(state string) bool {
	claims := &jwt.RegisteredClaims{}
	return parseToken(state, g.stateSecret, claims) == nil
}

// GetAuthURL returns the URL to redirect the user to Google's consent screen
func (g *GoogleOAuthManager) GetAuthURL(state string) string {
	return g.config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// ExchangeCodeForToken exchanges the authorization code for tokens
func (g *GoogleOAuthManager) ExchangeCodeForToken(ctx context.Context, code string) (*oauth2.Token, error) {
	token, err := g.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code for token: %w", err)
	}
	return token, nil
}

// VerifyIDToken parses and verifies the ID token, extracting user claims
func (g *GoogleOAuthManager) VerifyIDToken(ctx context.Context, token *oauth2.Token) (*IDTokenClaims, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	idToken, err := g.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify id token: %w", err)
	}

	var claims IDTokenClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to extract claims: %w", err)
	}

	return &claims, nil
}
