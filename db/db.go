package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
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

	ctx := context.Background()
	DB, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return fmt.Errorf("failed to open database pool: %w", err)
	}

	// Test the connection
	if err := DB.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	fmt.Println("Database connection established")

	// Run migrations
	err = runMigrations(databaseURL)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func runMigrations(databaseURL string) error {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, databaseURL)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	fmt.Println("Migrations completed successfully")
	return nil
}

func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}
