package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
)

//go:embed migrations
var migrationsFS embed.FS

var DB *pgxpool.Pool

func InitDB(databaseURL string) error {
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL not set")
	}

	// Neon's pooled endpoint runs PgBouncer in transaction-pooling mode, which breaks server-side perpared statements under concurrent queries - force the simple query protocol
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to parse database url: %w", err)
	}
	poolConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	DB, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to open database pool: %w", err)
	}

	// Test the connection
	if err := DB.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	slog.Info("database connection established")

	// Run migrations
	err = runMigrations(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func runMigrations(databaseURL string) error {
	migratorConnURL, err := migratorURL(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to prepare migrator url: %w", err)
	}

	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, migratorConnURL)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			slog.Error("failed to close migrator", "source_error", srcErr, "db_error", dbErr)
		}
	}()

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	slog.Info("migrations completed successfully")
	return nil
}

func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}

// withPgx5Scheme rewrites databaseURL's scheme to pgx5, the scheme golang-migrate's pgx/v5 driver
// registers itself under — it dispatches by URL scheme, not by which driver package is imported.
func withPgx5Scheme(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse database url: %w", err)
	}
	u.Scheme = "pgx5"
	return u.String(), nil
}

// directEndpoint drops "-pooler" from a Neon host. golang-migrate serialises migrators with a session-level
// advisory lock, which Neon's PgBouncer (transaction mode) can strand on a shared server connection.
func directEndpoint(databaseURL string) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse database url: %w", err)
	}

	label, rest, hasRest := strings.Cut(u.Hostname(), ".")
	if !strings.HasSuffix(label, "-pooler") {
		return databaseURL, nil
	}

	host := strings.TrimSuffix(label, "-pooler")
	if hasRest {
		host += "." + rest
	}
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	u.Host = host
	return u.String(), nil
}

var migrationLockTimeout = 10 * time.Second

// withLockTimeout makes Postgres cancel any lock wait longer than d. golang-migrate takes its advisory lock
// with no timeout of its own when it builds the migrator, so without this a held lock hangs the process.
func withLockTimeout(databaseURL string, d time.Duration) (string, error) {
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse database url: %w", err)
	}

	q := u.Query()
	option := fmt.Sprintf("-c lock_timeout=%d", d.Milliseconds())
	if existing := q.Get("options"); existing != "" {
		option = existing + " " + option
	}
	q.Set("options", option)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func migratorURL(databaseURL string) (string, error) {
	direct, err := directEndpoint(databaseURL)
	if err != nil {
		return "", err
	}
	timed, err := withLockTimeout(direct, migrationLockTimeout)
	if err != nil {
		return "", err
	}
	return withPgx5Scheme(timed)
}
