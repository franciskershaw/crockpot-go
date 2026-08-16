package config

import (
	"fmt"
	"os"
	"strings"
)

type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
)

type Config struct {
	Port                string
	Environment         Environment
	DatabaseURL         string
	JWTSecretAccess     string
	JWTSecretRefresh    string
	JWTSecretOAuthState string
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleRedirectURL   string
	FrontendURL         string
	TrustedProxies      []string
}

func Load() (*Config, error) {
	cfg := loadFromEnv()

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func loadFromEnv() *Config {
	return &Config{
		Port:                getEnvWithFallback("PORT", "8080"),
		Environment:         Environment(getEnvWithFallback("APP_ENV", string(EnvDevelopment))),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		JWTSecretAccess:     os.Getenv("JWT_SECRET_ACCESS"),
		JWTSecretRefresh:    os.Getenv("JWT_SECRET_REFRESH"),
		JWTSecretOAuthState: os.Getenv("JWT_SECRET_OAUTH_STATE"),
		GoogleClientID:      os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirectURL:   os.Getenv("GOOGLE_REDIRECT_URI"),
		FrontendURL:         os.Getenv("FRONTEND_URL"),
		TrustedProxies:      getEnvAsSlice("TRUSTED_PROXIES"),
	}
}

func validate(cfg *Config) error {
	required := []struct {
		name string
		val  string
	}{
		{"DATABASE_URL", cfg.DatabaseURL},
		{"JWT_SECRET_ACCESS", cfg.JWTSecretAccess},
		{"JWT_SECRET_REFRESH", cfg.JWTSecretRefresh},
		{"JWT_SECRET_OAUTH_STATE", cfg.JWTSecretOAuthState},
		{"FRONTEND_URL", cfg.FrontendURL},
	}

	var missing []string
	for _, r := range required {
		if r.val == "" {
			missing = append(missing, r.name)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	return nil
}

func getEnvWithFallback(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func getEnvAsSlice(key string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}
