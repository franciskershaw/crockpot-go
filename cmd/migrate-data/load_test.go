package main

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var dbOnce sync.Once

// requireDB migrates and connects to the dev DB the same way the repository tests do; it fails, not skips, when DATABASE_URL is unset.
func requireDB(t *testing.T) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		if os.Getenv("ALLOW_SKIP_DB_TESTS") == "1" {
			t.Skip("DATABASE_URL not set (ALLOW_SKIP_DB_TESTS=1)")
		}
		t.Fatal("DATABASE_URL not set. Set it, or set ALLOW_SKIP_DB_TESTS=1 to skip intentionally.")
	}
	var err error
	dbOnce.Do(func() { err = db.InitDB(url) })
	if err != nil {
		t.Fatalf("db.InitDB: %v", err)
	}
}

// historyTx opens a transaction that is always rolled back, so nothing a test writes reaches the dev DB.
func historyTx(t *testing.T) (context.Context, pgx.Tx, *sqlc.Queries) {
	t.Helper()
	requireDB(t)
	ctx := context.Background()
	tx, err := db.DB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return ctx, tx, sqlc.New(tx)
}

func seedHistoryUser(t *testing.T, ctx context.Context, tx pgx.Tx) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO users (id, google_id, email) VALUES ($1, $2, $3)`,
		id, "load-test-google-"+id.String(), "load-test-"+id.String()+"@example.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func seedHistoryRecipe(t *testing.T, ctx context.Context, tx pgx.Tx, createdBy uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO recipes (id, name, time_in_minutes, instructions, serves, approved, created_by_id)
		VALUES ($1, $2, 30, ARRAY['step'], 4, true, $3)`, id, "load-test-recipe-"+id.String(), createdBy); err != nil {
		t.Fatalf("seed recipe: %v", err)
	}
	return id
}

func countRows(t *testing.T, ctx context.Context, tx pgx.Tx, query string, args ...any) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestInsertMenuHistoryWritesBaselineRows(t *testing.T) {
	ctx, tx, q := historyTx(t)
	francis, zoe := seedHistoryUser(t, ctx, tx), seedHistoryUser(t, ctx, tx)
	r1, r2 := seedHistoryRecipe(t, ctx, tx, francis), seedHistoryRecipe(t, ctx, tx, francis)
	first := time.Date(2025, 8, 1, 18, 0, 0, 0, time.UTC)
	last := time.Date(2025, 9, 20, 17, 30, 0, 0, time.UTC)
	removed := time.Date(2025, 9, 27, 9, 15, 0, 0, time.UTC)

	rows := []baselineRow{
		{UserID: francis, RecipeID: r1, TimesAdded: 5, FirstAdded: first, LastAdded: last, LastRemoved: removed},
		{UserID: francis, RecipeID: r2, TimesAdded: 2, FirstAdded: first, LastAdded: first, LastRemoved: first},
		{UserID: zoe, RecipeID: r1, TimesAdded: 3, FirstAdded: first, LastAdded: last, LastRemoved: removed},
	}
	if err := insertMenuHistory(ctx, q, rows); err != nil {
		t.Fatalf("insertMenuHistory: %v", err)
	}

	if n := countRows(t, ctx, tx, `SELECT count(*) FROM recipe_menus WHERE user_id = ANY($1::uuid[])`, []string{francis.String(), zoe.String()}); n != 2 {
		t.Fatalf("got %d menus for two users, want 2 (one each, not one per history row)", n)
	}
	if n := countRows(t, ctx, tx, `
		SELECT count(*) FROM menu_history_baseline b JOIN recipe_menus rm ON rm.id = b.recipe_menu_id
		WHERE rm.user_id = ANY($1::uuid[])`, []string{francis.String(), zoe.String()}); n != 3 {
		t.Fatalf("got %d baseline rows, want 3", n)
	}

	var times int
	var gotFirst, gotLast, gotRemoved time.Time
	err := tx.QueryRow(ctx, `
		SELECT b.times_added_to_menu, b.first_added_to_menu, b.last_added_to_menu, b.last_removed_from_menu
		FROM menu_history_baseline b JOIN recipe_menus rm ON rm.id = b.recipe_menu_id
		WHERE rm.user_id = $1 AND b.recipe_id = $2`, francis, r1).Scan(&times, &gotFirst, &gotLast, &gotRemoved)
	if err != nil {
		t.Fatalf("read back francis/r1: %v", err)
	}
	if times != 5 || !gotFirst.Equal(first) || !gotLast.Equal(last) || !gotRemoved.Equal(removed) {
		t.Fatalf("francis/r1 = %d %v %v %v, want 5 %v %v %v", times, gotFirst, gotLast, gotRemoved, first, last, removed)
	}
}

func TestInsertMenuHistoryReusesAnExistingMenu(t *testing.T) {
	ctx, tx, q := historyTx(t)
	user := seedHistoryUser(t, ctx, tx)
	recipe := seedHistoryRecipe(t, ctx, tx, user)
	if _, err := tx.Exec(ctx, `INSERT INTO recipe_menus (user_id) VALUES ($1)`, user); err != nil {
		t.Fatalf("seed menu: %v", err)
	}
	at := time.Date(2025, 8, 1, 18, 0, 0, 0, time.UTC)

	err := insertMenuHistory(ctx, q, []baselineRow{{UserID: user, RecipeID: recipe, TimesAdded: 1, FirstAdded: at, LastAdded: at, LastRemoved: at}})
	if err != nil {
		t.Fatalf("insertMenuHistory: %v", err)
	}

	if n := countRows(t, ctx, tx, `SELECT count(*) FROM recipe_menus WHERE user_id = $1`, user); n != 1 {
		t.Fatalf("got %d menus, want 1", n)
	}
	if n := countRows(t, ctx, tx, `
		SELECT count(*) FROM menu_history_baseline b JOIN recipe_menus rm ON rm.id = b.recipe_menu_id
		WHERE rm.user_id = $1`, user); n != 1 {
		t.Fatalf("got %d baseline rows on the existing menu, want 1", n)
	}
}

// Passes before the write exists; guards against creating menus for users who have no history.
func TestInsertMenuHistoryWithNoRowsCreatesNothing(t *testing.T) {
	ctx, tx, q := historyTx(t)
	user := seedHistoryUser(t, ctx, tx)

	if err := insertMenuHistory(ctx, q, nil); err != nil {
		t.Fatalf("insertMenuHistory: %v", err)
	}

	if n := countRows(t, ctx, tx, `SELECT count(*) FROM recipe_menus WHERE user_id = $1`, user); n != 0 {
		t.Fatalf("got %d menus, want 0", n)
	}
}
