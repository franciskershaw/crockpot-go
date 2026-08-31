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
