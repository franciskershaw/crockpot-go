package repository_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Postgres does not auto-index the referencing side of an FK; assert each FK column leads some index.
func TestSchemaFKColumnsAreIndexed(t *testing.T) {
	ctx := context.Background()

	fkColumns := []struct{ table, column string }{
		{"items", "category_id"},
		{"item_allowed_units", "unit_id"},
		{"recipes", "created_by_id"},
		{"recipe_ingredients", "item_id"},
		{"recipe_ingredients", "unit_id"},
		{"recipe_favourites", "recipe_id"},
		{"recipe_menu_entries", "recipe_id"},
		{"menu_history_baseline", "recipe_id"},
		{"menu_history_events", "recipe_menu_id"},
		{"menu_history_events", "recipe_id"},
		{"shopping_list_items", "item_id"},
		{"shopping_list_items", "unit_id"},
		{"shopping_list_dismissed_items", "shopping_list_id"},
		{"shopping_list_dismissed_items", "item_id"},
		{"shopping_list_dismissed_items", "unit_id"},
	}

	for _, fk := range fkColumns {
		t.Run(fk.table+"."+fk.column, func(t *testing.T) {
			var indexed bool
			err := db.DB.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1
					FROM pg_index ix
					JOIN pg_class t ON t.oid = ix.indrelid
					JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ix.indkey[0]
					WHERE t.relname = $1
					  AND a.attname = $2
				)`, fk.table, fk.column).Scan(&indexed)
			require.NoError(t, err)
			assert.True(t, indexed,
				"%s.%s is a foreign key with no index whose leading column is %s", fk.table, fk.column, fk.column)
		})
	}
}

// Upsert-entry needs an atomic ON CONFLICT target and created_at for ordering.
func TestRecipeMenuEntriesUpsertSupport(t *testing.T) {
	ctx := context.Background()

	t.Run("unique constraint on (recipe_menu_id, recipe_id)", func(t *testing.T) {
		rows, err := db.DB.Query(ctx, `
			SELECT a.attname
			FROM pg_constraint con
			JOIN pg_class t ON t.oid = con.conrelid
			JOIN LATERAL unnest(con.conkey) AS colnum ON true
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = colnum
			WHERE t.relname = 'recipe_menu_entries' AND con.contype = 'u'`)
		require.NoError(t, err)
		defer rows.Close()

		var cols []string
		for rows.Next() {
			var name string
			require.NoError(t, rows.Scan(&name))
			cols = append(cols, name)
		}
		require.NoError(t, rows.Err())

		assert.ElementsMatch(t, []string{"recipe_menu_id", "recipe_id"}, cols,
			"recipe_menu_entries has no unique constraint on exactly (recipe_menu_id, recipe_id)")
	})

	t.Run("created_at column exists, not null, defaulted", func(t *testing.T) {
		var isNullable, columnDefault string
		err := db.DB.QueryRow(ctx, `
			SELECT is_nullable, COALESCE(column_default, '')
			FROM information_schema.columns
			WHERE table_name = 'recipe_menu_entries' AND column_name = 'created_at'`).
			Scan(&isNullable, &columnDefault)
		require.NoError(t, err)
		assert.Equal(t, "NO", isNullable, "recipe_menu_entries.created_at must be NOT NULL")
		assert.Contains(t, columnDefault, "CURRENT_TIMESTAMP", "recipe_menu_entries.created_at must default to the current time")
	})
}

// Cross-unit shopping-list merging needs dimension/base_factor on units, correctly backfilled for every seeded unit.
func TestUnitsDimensionAndBaseFactor(t *testing.T) {
	ctx := context.Background()

	t.Run("columns exist, not null", func(t *testing.T) {
		var dimType, dimNullable, factorType, factorNullable string
		err := db.DB.QueryRow(ctx, `
			SELECT c1.data_type, c1.is_nullable, c2.data_type, c2.is_nullable
			FROM information_schema.columns c1, information_schema.columns c2
			WHERE c1.table_name = 'units' AND c1.column_name = 'dimension'
			  AND c2.table_name = 'units' AND c2.column_name = 'base_factor'`).
			Scan(&dimType, &dimNullable, &factorType, &factorNullable)
		require.NoError(t, err, "units.dimension and units.base_factor must both exist")
		assert.Equal(t, "text", dimType, "units.dimension must be TEXT")
		assert.Equal(t, "NO", dimNullable, "units.dimension must be NOT NULL")
		assert.Equal(t, "numeric", factorType, "units.base_factor must be NUMERIC")
		assert.Equal(t, "NO", factorNullable, "units.base_factor must be NOT NULL")
	})

	t.Run("dimension is constrained to mass/volume/count", func(t *testing.T) {
		_, err := db.DB.Exec(ctx, `UPDATE units SET dimension = 'bogus' WHERE name = 'grams'`)
		assert.Error(t, err, "units.dimension must reject a value outside mass/volume/count")
	})

	t.Run("seeded units carry the correct dimension and base_factor", func(t *testing.T) {
		cases := []struct {
			name       string
			dimension  string
			baseFactor float64
		}{
			{"grams", "mass", 1},
			{"kilogram", "mass", 1000},
			{"milliliters", "volume", 1},
			{"litres", "volume", 1000},
			{"tablespoons", "volume", 15},
			{"teaspoons", "volume", 5},
			{"cup", "volume", 250},
			{"pint", "volume", 568},
			{"bottle", "count", 1},
			{"box", "count", 1},
			{"cans", "count", 1},
			{"cloves", "count", 1},
			{"cobs", "count", 1},
			{"fillets", "count", 1},
			{"jars", "count", 1},
			{"loaves", "count", 1},
			{"pack", "count", 1},
			{"rolls", "count", 1},
			{"sachet", "count", 1},
			{"slices", "count", 1},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var dimension string
				var baseFactor float64
				err := db.DB.QueryRow(ctx, `SELECT dimension, base_factor FROM units WHERE name = $1`, tc.name).
					Scan(&dimension, &baseFactor)
				require.NoError(t, err, "seeded unit %q must exist", tc.name)
				assert.Equal(t, tc.dimension, dimension, "unit %q dimension", tc.name)
				assert.Equal(t, tc.baseFactor, baseFactor, "unit %q base_factor", tc.name)
			})
		}
	})
}

// A dismissed generated item stays gone only until its required quantity changes.
func TestShoppingListDismissedItemsSchema(t *testing.T) {
	ctx := context.Background()

	t.Run("table and columns exist with expected types", func(t *testing.T) {
		rows, err := db.DB.Query(ctx, `
			SELECT column_name, data_type, is_nullable
			FROM information_schema.columns
			WHERE table_name = 'shopping_list_dismissed_items'`)
		require.NoError(t, err)
		defer rows.Close()

		cols := map[string]struct{ dataType, nullable string }{}
		for rows.Next() {
			var name, dataType, nullable string
			require.NoError(t, rows.Scan(&name, &dataType, &nullable))
			cols[name] = struct{ dataType, nullable string }{dataType, nullable}
		}
		require.NoError(t, rows.Err())

		require.NotEmpty(t, cols, "shopping_list_dismissed_items table must exist")
		assert.Equal(t, "uuid", cols["shopping_list_id"].dataType)
		assert.Equal(t, "NO", cols["shopping_list_id"].nullable)
		assert.Equal(t, "uuid", cols["item_id"].dataType)
		assert.Equal(t, "NO", cols["item_id"].nullable)
		assert.Equal(t, "uuid", cols["unit_id"].dataType, "unit_id must exist and be nullable, not NOT NULL")
		assert.Equal(t, "YES", cols["unit_id"].nullable)
		assert.Equal(t, "numeric", cols["quantity_at_dismissal"].dataType)
		assert.Equal(t, "NO", cols["quantity_at_dismissal"].nullable)
	})

	t.Run("cascades on shopping_lists deletion", func(t *testing.T) {
		var deleteRule string
		err := db.DB.QueryRow(ctx, `
			SELECT rc.delete_rule
			FROM information_schema.referential_constraints rc
			JOIN information_schema.key_column_usage kcu
			  ON kcu.constraint_name = rc.constraint_name
			WHERE kcu.table_name = 'shopping_list_dismissed_items'
			  AND kcu.column_name = 'shopping_list_id'`).Scan(&deleteRule)
		require.NoError(t, err, "shopping_list_dismissed_items.shopping_list_id must have an FK to shopping_lists")
		assert.Equal(t, "CASCADE", deleteRule)
	})
}

// Postgres refuses a TRUNCATE unless every table with an FK into a truncated table is truncated in the same statement.
func TestMigrateTruncateCoversEveryReferencingTable(t *testing.T) {
	ctx := context.Background()

	src, err := os.ReadFile("../sqlc/queries/migrate.sql")
	require.NoError(t, err)
	m := regexp.MustCompile(`(?s)name: MigrateTruncate.*?TRUNCATE\s+(.*?)\s+RESTART IDENTITY`).FindSubmatch(src)
	require.NotNil(t, m, "could not find the MigrateTruncate statement in migrate.sql")

	truncated := map[string]bool{}
	for _, name := range strings.Split(string(m[1]), ",") {
		truncated[strings.TrimSpace(name)] = true
	}
	require.Greater(t, len(truncated), 1, "parsed no tables out of MigrateTruncate")

	rows, err := db.DB.Query(ctx, `
		SELECT child.relname, parent.relname
		FROM pg_constraint c
		JOIN pg_class child ON child.oid = c.conrelid
		JOIN pg_class parent ON parent.oid = c.confrelid
		WHERE c.contype = 'f'`)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var child, parent string
		require.NoError(t, rows.Scan(&child, &parent))
		if truncated[parent] {
			assert.True(t, truncated[child],
				"%s has an FK into %s, which MigrateTruncate wipes, but %s is not in the same TRUNCATE", child, parent, child)
		}
	}
	require.NoError(t, rows.Err())
}

func tableExists(t *testing.T, name string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1)`,
		name).Scan(&exists))
	return exists
}

// insertTestMenu creates a recipe_menus row; it is removed when its user is deleted.
func insertTestMenu(t *testing.T, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`INSERT INTO recipe_menus (user_id) VALUES ($1) RETURNING id`, userID).Scan(&id))
	return id
}

func insertTestHistoryEvent(t *testing.T, menuID, recipeID uuid.UUID, eventType string) error {
	t.Helper()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO menu_history_events (recipe_menu_id, recipe_id, event_type) VALUES ($1, $2, $3)`,
		menuID, recipeID, eventType)
	return err
}

func TestMenuHistoryEventsSchema(t *testing.T) {
	require.True(t, tableExists(t, "menu_history_events"), "menu_history_events table must exist")

	t.Run("event_type accepts add and remove, rejects anything else", func(t *testing.T) {
		user := insertTestUser(t, "Owner")
		recipe := insertTestRecipeRow(t, user, true)
		menu := insertTestMenu(t, user)

		assert.NoError(t, insertTestHistoryEvent(t, menu, recipe, "add"))
		assert.NoError(t, insertTestHistoryEvent(t, menu, recipe, "remove"))
		assert.Error(t, insertTestHistoryEvent(t, menu, recipe, "bogus"), "event_type must be constrained to add/remove")
	})

	t.Run("occurred_at is not null and defaults to now", func(t *testing.T) {
		user := insertTestUser(t, "Owner")
		recipe := insertTestRecipeRow(t, user, true)
		menu := insertTestMenu(t, user)
		require.NoError(t, insertTestHistoryEvent(t, menu, recipe, "add"))

		var isNullable string
		require.NoError(t, db.DB.QueryRow(context.Background(), `
			SELECT is_nullable FROM information_schema.columns
			WHERE table_name = 'menu_history_events' AND column_name = 'occurred_at'`).Scan(&isNullable))
		assert.Equal(t, "NO", isNullable)

		var ageSeconds float64
		require.NoError(t, db.DB.QueryRow(context.Background(),
			`SELECT EXTRACT(EPOCH FROM (now() - occurred_at)) FROM menu_history_events WHERE recipe_menu_id = $1`,
			menu).Scan(&ageSeconds))
		assert.Less(t, ageSeconds, 60.0, "occurred_at must default to the insert time")
	})

	t.Run("deleting a recipe deletes its events", func(t *testing.T) {
		user := insertTestUser(t, "Owner")
		recipe := insertTestRecipeRow(t, user, true)
		menu := insertTestMenu(t, user)
		require.NoError(t, insertTestHistoryEvent(t, menu, recipe, "add"))

		_, err := db.DB.Exec(context.Background(), `DELETE FROM recipes WHERE id = $1`, recipe)
		require.NoError(t, err)
		assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM menu_history_events WHERE recipe_menu_id = $1`, menu))
	})

	t.Run("deleting a user deletes their events", func(t *testing.T) {
		owner := insertTestUser(t, "Owner")
		caller := insertTestUser(t, "Caller")
		recipe := insertTestRecipeRow(t, owner, true)
		menu := insertTestMenu(t, caller)
		require.NoError(t, insertTestHistoryEvent(t, menu, recipe, "add"))

		_, err := db.DB.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, caller)
		require.NoError(t, err)
		assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM menu_history_events WHERE recipe_menu_id = $1`, menu))
	})
}

func TestMenuHistoryBaselineSchema(t *testing.T) {
	t.Run("renamed from menu_history_entries", func(t *testing.T) {
		assert.True(t, tableExists(t, "menu_history_baseline"), "menu_history_baseline table must exist")
		assert.False(t, tableExists(t, "menu_history_entries"), "menu_history_entries must be renamed away")
	})

	t.Run("one row per (recipe_menu_id, recipe_id)", func(t *testing.T) {
		require.True(t, tableExists(t, "menu_history_baseline"), "menu_history_baseline table must exist")
		user := insertTestUser(t, "Owner")
		recipe := insertTestRecipeRow(t, user, true)
		menu := insertTestMenu(t, user)

		insert := func() error {
			_, err := db.DB.Exec(context.Background(), `
				INSERT INTO menu_history_baseline
				    (recipe_menu_id, recipe_id, times_added_to_menu, first_added_to_menu, last_added_to_menu, last_removed_from_menu)
				VALUES ($1, $2, 3, now(), now(), now())`, menu, recipe)
			return err
		}
		require.NoError(t, insert())
		assert.Error(t, insert(), "a second baseline row for the same menu and recipe must be rejected")
	})
}
