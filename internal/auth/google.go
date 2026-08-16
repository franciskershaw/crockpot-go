package auth

import (
	"context"
	"errors"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

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

func NewGoogleOAuthManager(clientID, clientSecret, redirectURL, stateSecret string) (*GoogleOAuthManager, error) {
	return nil, errors.New("not implemented")
}

func newGoogleOAuthManager(config *oauth2.Config, verifier *oidc.IDTokenVerifier, stateSecret string) *GoogleOAuthManager {
	return &GoogleOAuthManager{config: config, verifier: verifier, stateSecret: stateSecret}
}

func (g *GoogleOAuthManager) GenerateState() (string, error) {
	return "", errors.New("not implemented")
}

func (g *GoogleOAuthManager) ValidateState(state string) bool {
	return false
}

func (g *GoogleOAuthManager) GetAuthURL(state string) string {
	return ""
}

func (g *GoogleOAuthManager) ExchangeCodeForToken(ctx context.Context, code string) (*oauth2.Token, error) {
	return nil, errors.New("not implemented")
}

func (g *GoogleOAuthManager) VerifyIDToken(ctx context.Context, token *oauth2.Token) (*IDTokenClaims, error) {
	return nil, errors.New("not implemented")
}
