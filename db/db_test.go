package db

import "testing"

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
