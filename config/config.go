package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvProduction  Environment = "production"
)

// LegacyImageFolders hold the migrated recipes' photos (production's upload folder is one of them); only production may destroy in them.
var LegacyImageFolders = []string{"Crockpot", "recipes"}

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
	ResendAPIKey        string
	EmailFrom           string

	CloudinaryCloudName    string
	CloudinaryAPIKey       string
	CloudinaryAPISecret    string
	CloudinaryUploadFolder string
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
		ResendAPIKey:        os.Getenv("RESEND_API_KEY"),
		EmailFrom:           os.Getenv("EMAIL_FROM"),

		CloudinaryCloudName:    os.Getenv("CLOUDINARY_CLOUD_NAME"),
		CloudinaryAPIKey:       os.Getenv("CLOUDINARY_API_KEY"),
		CloudinaryAPISecret:    os.Getenv("CLOUDINARY_API_SECRET"),
		CloudinaryUploadFolder: os.Getenv("CLOUDINARY_UPLOAD_FOLDER"),
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
		{"GOOGLE_CLIENT_ID", cfg.GoogleClientID},
		{"GOOGLE_CLIENT_SECRET", cfg.GoogleClientSecret},
		{"GOOGLE_REDIRECT_URI", cfg.GoogleRedirectURL},
		{"FRONTEND_URL", cfg.FrontendURL},
		{"RESEND_API_KEY", cfg.ResendAPIKey},
		{"EMAIL_FROM", cfg.EmailFrom},
		{"CLOUDINARY_CLOUD_NAME", cfg.CloudinaryCloudName},
		{"CLOUDINARY_API_KEY", cfg.CloudinaryAPIKey},
		{"CLOUDINARY_API_SECRET", cfg.CloudinaryAPISecret},
		{"CLOUDINARY_UPLOAD_FOLDER", cfg.CloudinaryUploadFolder},
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

	if err := validateJWTSecrets(cfg); err != nil {
		return err
	}

	return validateUploadFolder(cfg.CloudinaryUploadFolder, cfg.Environment)
}

const minJWTSecretBytes = 32

func validateJWTSecrets(cfg *Config) error {
	secrets := []struct {
		name string
		val  string
	}{
		{"JWT_SECRET_ACCESS", cfg.JWTSecretAccess},
		{"JWT_SECRET_REFRESH", cfg.JWTSecretRefresh},
		{"JWT_SECRET_OAUTH_STATE", cfg.JWTSecretOAuthState},
	}

	for i, s := range secrets {
		if len(s.val) < minJWTSecretBytes {
			return fmt.Errorf("%s must be at least %d bytes; generate one with openssl rand -base64 32", s.name, minJWTSecretBytes)
		}
		for _, other := range secrets[:i] {
			if s.val == other.val {
				return fmt.Errorf("%s and %s must differ", other.name, s.name)
			}
		}
	}

	return nil
}

// validateUploadFolder keeps a non-production environment from uploading into, and so destroying in, production's folders.
func validateUploadFolder(folder string, env Environment) error {
	if strings.HasPrefix(folder, "/") || strings.HasSuffix(folder, "/") || strings.Contains(folder, "//") {
		return fmt.Errorf("CLOUDINARY_UPLOAD_FOLDER %q must not start or end with / or contain //", folder)
	}
	if env != EnvProduction && slices.Contains(LegacyImageFolders, folder) {
		return fmt.Errorf("CLOUDINARY_UPLOAD_FOLDER %q is a production folder; use e.g. dev/recipes outside production", folder)
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
