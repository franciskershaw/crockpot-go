package db

import (
	"context"
	"math/rand/v2"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/jackc/pgx/v5"
)

func requireDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		if os.Getenv("ALLOW_SKIP_DB_TESTS") == "1" {
			t.Skip("DATABASE_URL not set (ALLOW_SKIP_DB_TESTS=1)")
		}
		t.Fatal("DATABASE_URL not set. Set it, or set ALLOW_SKIP_DB_TESTS=1 to skip intentionally.")
	}
	return url
}

func TestDirectEndpoint(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			"pooled Neon host loses -pooler",
			"postgresql://user:pass@ep-cool-name-abc123-pooler.c-2.eu-west-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require",
			"postgresql://user:pass@ep-cool-name-abc123.c-2.eu-west-2.aws.neon.tech/neondb?sslmode=require&channel_binding=require",
		},
		{
			"port is preserved",
			"postgresql://user:pass@ep-cool-name-abc123-pooler.c-2.eu-west-2.aws.neon.tech:5432/neondb",
			"postgresql://user:pass@ep-cool-name-abc123.c-2.eu-west-2.aws.neon.tech:5432/neondb",
		},
		{
			"already-direct host is unchanged",
			"postgresql://user:pass@ep-cool-name-abc123.c-2.eu-west-2.aws.neon.tech/neondb",
			"postgresql://user:pass@ep-cool-name-abc123.c-2.eu-west-2.aws.neon.tech/neondb",
		},
		{
			"non-Neon host is unchanged",
			"postgres://user:pass@localhost:5432/crockpot?sslmode=disable",
			"postgres://user:pass@localhost:5432/crockpot?sslmode=disable",
		},
		{
			"only a trailing -pooler on the first host label counts",
			"postgresql://user:pass@ep-pooler-thing-abc123.c-2.eu-west-2.aws.neon.tech/neondb",
			"postgresql://user:pass@ep-pooler-thing-abc123.c-2.eu-west-2.aws.neon.tech/neondb",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := directEndpoint(tc.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDirectEndpoint_ReturnsErrorForUnparseableURL(t *testing.T) {
	if _, err := directEndpoint("://not-a-url"); err == nil {
		t.Fatal("expected an error for an unparseable url, got nil")
	}
}

func TestWithLockTimeout(t *testing.T) {
	t.Run("adds a lock_timeout option and keeps the rest of the url", func(t *testing.T) {
		got, err := withLockTimeout("pgx5://user:pass@host.example.com/dbname?sslmode=require", 10*time.Second)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("result does not parse: %v", err)
		}
		if opts := u.Query().Get("options"); opts != "-c lock_timeout=10000" {
			t.Errorf("options = %q, want %q", opts, "-c lock_timeout=10000")
		}
		if u.Query().Get("sslmode") != "require" || u.Host != "host.example.com" || u.User.String() != "user:pass" || u.Path != "/dbname" {
			t.Errorf("rest of the url changed: %q", got)
		}
	})

	t.Run("appends to an existing options value", func(t *testing.T) {
		got, err := withLockTimeout("pgx5://user:pass@host.example.com/dbname?options=-c%20statement_timeout%3D5000", 2500*time.Millisecond)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("result does not parse: %v", err)
		}
		if opts := u.Query().Get("options"); opts != "-c statement_timeout=5000 -c lock_timeout=2500" {
			t.Errorf("options = %q, want both settings", opts)
		}
	})

	t.Run("unparseable url is an error", func(t *testing.T) {
		if _, err := withLockTimeout("://not-a-url", time.Second); err == nil {
			t.Fatal("expected an error for an unparseable url, got nil")
		}
	})
}

func TestDirectEndpoint_HonoursSessionAdvisoryLocks(t *testing.T) {
	ctx := context.Background()
	direct, err := directEndpoint(requireDatabaseURL(t))
	if err != nil {
		t.Fatalf("directEndpoint: %v", err)
	}

	a, err := pgx.Connect(ctx, direct)
	if err != nil {
		t.Fatalf("connect a: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(ctx) })
	b, err := pgx.Connect(ctx, direct)
	if err != nil {
		t.Fatalf("connect b: %v", err)
	}
	t.Cleanup(func() { _ = b.Close(ctx) })

	var app string
	if err := a.QueryRow(ctx, `SHOW application_name`).Scan(&app); err != nil {
		t.Fatalf("show application_name: %v", err)
	}
	if app == "pgbouncer" {
		t.Fatal("connection is still going through the pooler; refusing to take a session lock on it")
	}

	key := rand.Int64N(1_000_000_000) + 7_000_000_000
	if _, err := a.Exec(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
		t.Fatalf("lock: %v", err)
	}

	var pid1, pid2 int
	if err := a.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid1); err != nil {
		t.Fatal(err)
	}
	if err := a.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid2); err != nil {
		t.Fatal(err)
	}
	if pid1 != pid2 {
		t.Fatalf("one client connection reached two server sessions (%d, %d); locks cannot be trusted", pid1, pid2)
	}

	var got bool
	if err := b.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatal("a second session acquired a lock the first still holds")
	}

	var unlocked bool
	if err := a.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, key).Scan(&unlocked); err != nil {
		t.Fatal(err)
	}
	if !unlocked {
		t.Fatal("unlock did not release a lock this session held")
	}
	if err := b.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("the second session could not take the lock after it was released")
	}
	_, _ = b.Exec(ctx, `SELECT pg_advisory_unlock($1)`, key)
}

// Runs against the direct endpoint on purpose: a lock taken through the pooler can outlive the test.
func TestRunMigrations_FailsFastWhenTheLockIsHeld(t *testing.T) {
	direct := strings.Replace(requireDatabaseURL(t), "-pooler", "", 1)
	migratorURL, err := withPgx5Scheme(direct)
	if err != nil {
		t.Fatal(err)
	}

	holder, err := database.Open(migratorURL)
	if err != nil {
		t.Fatalf("open holder: %v", err)
	}
	if err := holder.Lock(); err != nil {
		t.Fatalf("holder lock: %v", err)
	}
	t.Cleanup(func() {
		_ = holder.Unlock()
		_ = holder.Close()
	})

	old := migrationLockTimeout
	migrationLockTimeout = time.Second
	t.Cleanup(func() { migrationLockTimeout = old })

	done := make(chan error, 1)
	go func() { done <- runMigrations(direct) }()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "lock timeout") {
			t.Fatalf("err = %v, want an error naming the lock timeout", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("runMigrations is still blocked 20s after a 1s lock timeout")
	}
}
