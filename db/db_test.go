package db

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestInitDB_EmptyDatabaseURL(t *testing.T) {
	err := InitDB("")
	if err == nil {
		t.Fatal("expected error for empty databaseURL, got nil")
	}
}

func TestWithPgx5Scheme_RewritesSchemePreservesRest(t *testing.T) {
	got, err := withPgx5Scheme("postgresql://user:pass@host.example.com/dbname?sslmode=require&channel_binding=require")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "pgx5://user:pass@host.example.com/dbname?sslmode=require&channel_binding=require"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWithPgx5Scheme_ReturnsErrorForUnparseableURL(t *testing.T) {
	_, err := withPgx5Scheme("://not-a-url")
	if err == nil {
		t.Fatal("expected an error for an unparseable url, got nil")
	}
}

func TestNewPoolConfig_SizesPoolAndUsesSimpleProtocol(t *testing.T) {
	cfg, err := newPoolConfig("postgresql://user:pass@host.example.com/dbname?sslmode=require")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxConns != 10 {
		t.Errorf("MaxConns = %d, want 10", cfg.MaxConns)
	}
	if cfg.ConnConfig.DefaultQueryExecMode != pgx.QueryExecModeSimpleProtocol {
		t.Errorf("DefaultQueryExecMode = %v, want simple protocol (Neon's pooler)", cfg.ConnConfig.DefaultQueryExecMode)
	}
}
