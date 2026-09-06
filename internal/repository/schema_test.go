package repository_test

import (
	"context"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
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
		{"menu_history_entries", "recipe_id"},
		{"shopping_list_items", "item_id"},
		{"shopping_list_items", "unit_id"},
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
