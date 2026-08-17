package repository_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/repository"
	"github.com/google/uuid"
)

var (
	userRepo                   *repository.PostgresUserRepository
	refreshTokenRepo           *repository.PostgresRefreshTokenRepository
	emailVerificationTokenRepo *repository.PostgresEmailVerificationTokenRepository
	passwordResetTokenRepo     *repository.PostgresPasswordResetTokenRepository
	transactor                 *repository.PostgresTransactor
	repoUserID                 uuid.UUID
)

func TestMain(m *testing.M) {
	if os.Getenv("DATABASE_URL") == "" {
		if os.Getenv("ALLOW_SKIP_DB_TESTS") == "1" {
			fmt.Println("skipping repository tests: DATABASE_URL not set (ALLOW_SKIP_DB_TESTS=1)")
			os.Exit(0)
		}
		fmt.Println("FATAL: DATABASE_URL not set. Set it, or set ALLOW_SKIP_DB_TESTS=1 to skip intentionally.")
		os.Exit(1)
	}

	if err := db.InitDB(os.Getenv("DATABASE_URL")); err != nil {
		fmt.Printf("failed to init db: %v\n", err)
		os.Exit(1)
	}

	userRepo = repository.NewPostgresUserRepository(db.DB)
	refreshTokenRepo = repository.NewPostgresRefreshTokenRepository(db.DB)
	emailVerificationTokenRepo = repository.NewPostgresEmailVerificationTokenRepository(db.DB)
	passwordResetTokenRepo = repository.NewPostgresPasswordResetTokenRepository(db.DB)
	transactor = repository.NewPostgresTransactor(db.DB)
	repoUserID = uuid.New()

	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO users (id, google_id, email) VALUES ($1, $2, $3)`,
		repoUserID, "repo-test-google-"+repoUserID.String(), "repo-test-"+repoUserID.String()+"@example.com",
	)
	if err != nil {
		fmt.Printf("failed to create test user: %v\n", err)
		db.CloseDB()
		os.Exit(1)
	}

	code := m.Run()

	if _, err := db.DB.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, repoUserID); err != nil {
		fmt.Printf("failed to delete test user: %v\n", err)
	}
	db.CloseDB()
	os.Exit(code)
}

// cleanupExec registers a t.Cleanup that runs query/args and asserts it succeeded.
func cleanupExec(t *testing.T, query string, args ...any) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := db.DB.Exec(context.Background(), query, args...); err != nil {
			t.Errorf("cleanup query failed: %v", err)
		}
	})
}
