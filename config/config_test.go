package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET_ACCESS", "test-access-secret")
	t.Setenv("JWT_SECRET_REFRESH", "test-refresh-secret")
	t.Setenv("JWT_SECRET_OAUTH_STATE", "test-oauth-state-secret")
	t.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-client-secret")
	t.Setenv("GOOGLE_REDIRECT_URI", "http://localhost:8080/callback")
	t.Setenv("FRONTEND_URL", "http://localhost:5173")
	t.Setenv("RESEND_API_KEY", "test-resend-key")
	t.Setenv("EMAIL_FROM", "noreply@example.com")
}

// unsetEnv clears key for the test and restores its prior value after.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	orig, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("failed to unset %s: %v", key, err)
	}
	if existed {
		t.Cleanup(func() {
			if err := os.Setenv(key, orig); err != nil {
				t.Errorf("failed to restore %s: %v", key, err)
			}
		})
	}
}

func mustLoad(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	return cfg
}

func TestLoad_RequiresEnvVar(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"JWT_SECRET_ACCESS",
		"JWT_SECRET_REFRESH",
		"JWT_SECRET_OAUTH_STATE",
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"GOOGLE_REDIRECT_URI",
		"FRONTEND_URL",
		"RESEND_API_KEY",
		"EMAIL_FROM",
	}

	for _, key := range required {
		t.Run(key, func(t *testing.T) {
			setRequiredEnv(t)
			unsetEnv(t, key)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected Load() to error when %s is unset", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("expected error to mention %s, got: %v", key, err)
			}
		})
	}
}

func TestLoad_ReportsAllMissingVarsTogether(t *testing.T) {
	unsetEnv(t, "DATABASE_URL")
	unsetEnv(t, "JWT_SECRET_ACCESS")
	unsetEnv(t, "JWT_SECRET_REFRESH")
	unsetEnv(t, "JWT_SECRET_OAUTH_STATE")
	t.Setenv("FRONTEND_URL", "http://localhost:5173")

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to return an error")
	}
	for _, key := range []string{"DATABASE_URL", "JWT_SECRET_ACCESS", "JWT_SECRET_REFRESH", "JWT_SECRET_OAUTH_STATE"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("expected error to mention %s, got: %v", key, err)
		}
	}
}

func TestLoad_Environment(t *testing.T) {
	tests := []struct {
		name   string
		appEnv string // "" means unset
		want   Environment
	}{
		{"defaults to development when unset", "", EnvDevelopment},
		{"reads APP_ENV when set", "production", EnvProduction},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			if tt.appEnv == "" {
				unsetEnv(t, "APP_ENV")
			} else {
				t.Setenv("APP_ENV", tt.appEnv)
			}

			cfg := mustLoad(t)
			if cfg.Environment != tt.want {
				t.Errorf("Environment = %q, want %q", cfg.Environment, tt.want)
			}
		})
	}
}

func TestLoad_TrustedProxies(t *testing.T) {
	tests := []struct {
		name  string
		value string // "" means unset
		want  []string
	}{
		{"unset defaults to empty", "", nil},
		{"parses comma-separated list", "10.0.0.1,10.0.0.2", []string{"10.0.0.1", "10.0.0.2"}},
		{"trims whitespace", "10.0.0.1, 10.0.0.2 ", []string{"10.0.0.1", "10.0.0.2"}},
		{"drops empty entries", "10.0.0.1,,10.0.0.2", []string{"10.0.0.1", "10.0.0.2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRequiredEnv(t)
			if tt.value == "" {
				unsetEnv(t, "TRUSTED_PROXIES")
			} else {
				t.Setenv("TRUSTED_PROXIES", tt.value)
			}

			cfg := mustLoad(t)
			if !slices.Equal(cfg.TrustedProxies, tt.want) {
				t.Errorf("TrustedProxies = %v, want %v", cfg.TrustedProxies, tt.want)
			}
		})
	}
}

func TestLoad_ReadsGoogleOAuthFields(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("GOOGLE_CLIENT_ID", "client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "client-secret")
	t.Setenv("GOOGLE_REDIRECT_URI", "http://localhost:8080/callback")

	cfg := mustLoad(t)

	if cfg.GoogleClientID != "client-id" {
		t.Errorf("GoogleClientID = %q, want %q", cfg.GoogleClientID, "client-id")
	}
	if cfg.GoogleClientSecret != "client-secret" {
		t.Errorf("GoogleClientSecret = %q, want %q", cfg.GoogleClientSecret, "client-secret")
	}
	if cfg.GoogleRedirectURL != "http://localhost:8080/callback" {
		t.Errorf("GoogleRedirectURL = %q, want %q", cfg.GoogleRedirectURL, "http://localhost:8080/callback")
	}
}

func TestLoad_ReadsEmailFields(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RESEND_API_KEY", "resend-key")
	t.Setenv("EMAIL_FROM", "hello@crockpot.app")

	cfg := mustLoad(t)

	if cfg.ResendAPIKey != "resend-key" {
		t.Errorf("ResendAPIKey = %q, want %q", cfg.ResendAPIKey, "resend-key")
	}
	if cfg.EmailFrom != "hello@crockpot.app" {
		t.Errorf("EmailFrom = %q, want %q", cfg.EmailFrom, "hello@crockpot.app")
	}
}
