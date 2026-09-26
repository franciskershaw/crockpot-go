package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func unitIDByName(t *testing.T, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT id FROM units WHERE name = $1`, name).Scan(&id))
	return id
}

func unitAbbreviationByName(t *testing.T, name string) string {
	t.Helper()
	var abbreviation string
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT abbreviation FROM units WHERE name = $1`, name).Scan(&abbreviation))
	return abbreviation
}

type testIngredient struct {
	itemID   uuid.UUID
	unitID   *uuid.UUID
	quantity float64
}

// insertTestRecipeWithIngredients bypasses recipeRepo.Create for control over serves and per-ingredient unit/quantity that recipeOpts doesn't expose.
func insertTestRecipeWithIngredients(t *testing.T, createdBy uuid.UUID, serves int, ingredients []testIngredient) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO recipes (id, name, time_in_minutes, instructions, serves, approved, created_by_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, "repo-test-sl-recipe-"+id.String(), 30, []string{"step 1"}, serves, true, createdBy,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, id)

	for i, ing := range ingredients {
		_, err := db.DB.Exec(context.Background(),
			`INSERT INTO recipe_ingredients (recipe_id, item_id, unit_id, quantity, position) VALUES ($1, $2, $3, $4, $5)`,
			id, ing.itemID, ing.unitID, ing.quantity, i,
		)
		require.NoError(t, err)
	}
	return id
}

func addToMenu(t *testing.T, userID, recipeID uuid.UUID, serves int) {
	t.Helper()
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), userID.String(), recipeID.String(), serves, false))
}

type shoppingListItemRow struct {
	id       uuid.UUID
	itemID   uuid.UUID
	unitID   *uuid.UUID
	quantity float64
	obtained bool
	isManual bool
}

func getShoppingListItemsByUser(t *testing.T, userID uuid.UUID) []shoppingListItemRow {
	t.Helper()
	rows, err := db.DB.Query(context.Background(), `
		SELECT sli.id, sli.item_id, sli.unit_id, sli.quantity::float8, sli.obtained, sli.is_manual
		FROM shopping_list_items sli
		JOIN shopping_lists sl ON sl.id = sli.shopping_list_id
		WHERE sl.user_id = $1
		ORDER BY sli.item_id, sli.unit_id`, userID)
	require.NoError(t, err)
	defer rows.Close()

	var out []shoppingListItemRow
	for rows.Next() {
		var r shoppingListItemRow
		require.NoError(t, rows.Scan(&r.id, &r.itemID, &r.unitID, &r.quantity, &r.obtained, &r.isManual))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func findShoppingListItem(items []shoppingListItemRow, itemID uuid.UUID, unitID *uuid.UUID) (shoppingListItemRow, bool) {
	for _, r := range items {
		if r.itemID != itemID {
			continue
		}
		if (r.unitID == nil) != (unitID == nil) {
			continue
		}
		if r.unitID != nil && unitID != nil && *r.unitID != *unitID {
			continue
		}
		return r, true
	}
	return shoppingListItemRow{}, false
}

func setShoppingListItemObtained(t *testing.T, itemRowID uuid.UUID, obtained bool) {
	t.Helper()
	_, err := db.DB.Exec(context.Background(),
		`UPDATE shopping_list_items SET obtained = $1 WHERE id = $2`, obtained, itemRowID)
	require.NoError(t, err)
}

func insertManualShoppingListItem(t *testing.T, userID, itemID uuid.UUID, unitID *uuid.UUID, quantity float64, obtained bool) uuid.UUID {
	t.Helper()
	listID := getOrCreateShoppingListID(t, userID)

	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO shopping_list_items (id, shopping_list_id, item_id, unit_id, quantity, obtained, is_manual)
		 VALUES ($1, $2, $3, $4, $5, $6, true)`,
		id, listID, itemID, unitID, quantity, obtained,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM shopping_list_items WHERE id = $1`, id)
	return id
}

// Call after any item/category fixtures: LIFO cleanup order then cascades away shopping_list_items/dismissed_items (including rows Regenerate creates later) before those items' own ON DELETE RESTRICT cleanup runs.
func registerShoppingListCascadeCleanup(t *testing.T, userID uuid.UUID) {
	t.Helper()
	cleanupExec(t, `DELETE FROM shopping_lists WHERE user_id = $1`, userID)
}

func getOrCreateShoppingListID(t *testing.T, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.DB.QueryRow(context.Background(), `SELECT id FROM shopping_lists WHERE user_id = $1`, userID).Scan(&id)
	if err == nil {
		return id
	}
	id = uuid.New()
	_, err = db.DB.Exec(context.Background(), `INSERT INTO shopping_lists (id, user_id) VALUES ($1, $2)`, id, userID)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM shopping_lists WHERE id = $1`, id)
	return id
}

func insertDismissedItem(t *testing.T, userID, itemID uuid.UUID, unitID *uuid.UUID, quantityAtDismissal float64) {
	t.Helper()
	listID := getOrCreateShoppingListID(t, userID)
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO shopping_list_dismissed_items (id, shopping_list_id, item_id, unit_id, quantity_at_dismissal)
		 VALUES ($1, $2, $3, $4, $5)`,
		id, listID, itemID, unitID, quantityAtDismissal,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM shopping_list_dismissed_items WHERE id = $1`, id)
}

func countDismissedItems(t *testing.T, userID, itemID uuid.UUID) int {
	t.Helper()
	return rowCount(t, `
		SELECT count(*) FROM shopping_list_dismissed_items d
		JOIN shopping_lists sl ON sl.id = d.shopping_list_id
		WHERE sl.user_id = $1 AND d.item_id = $2`, userID, itemID)
}

func TestRegenerate_LazilyCreatesShoppingListRow(t *testing.T) {
	userID := insertTestUser(t, "Regen No Menu")

	err := shoppingListRepo.Regenerate(context.Background(), userID.String())
	require.NoError(t, err)

	var listID uuid.UUID
	err = db.DB.QueryRow(context.Background(), `SELECT id FROM shopping_lists WHERE user_id = $1`, userID).Scan(&listID)
	require.NoError(t, err, "shopping_lists row must be created on first regenerate")
	cleanupExec(t, `DELETE FROM shopping_lists WHERE id = $1`, listID)
}

func TestRegenerate_CombinesSameUnitQuantitiesAcrossRecipes(t *testing.T) {
	userID := insertTestUser(t, "Same Unit Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeA := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	recipeB := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 50}})
	addToMenu(t, userID, recipeA, 4)
	addToMenu(t, userID, recipeB, 4)

	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok, "expected a combined row for the shared item")
	assert.Equal(t, 150.0, row.quantity)
}

func TestRegenerate_MergesCompatibleCrossUnitMetric(t *testing.T) {
	userID := insertTestUser(t, "Cross Unit Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	tablespoons := unitIDByName(t, "tablespoons")
	milliliters := unitIDByName(t, "milliliters")
	registerShoppingListCascadeCleanup(t, userID)

	// 2 tablespoons (15ml each = 30ml) + 100ml = 130ml combined, under the volume base unit.
	recipeA := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &tablespoons, 2}})
	recipeB := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &milliliters, 100}})
	addToMenu(t, userID, recipeA, 4)
	addToMenu(t, userID, recipeB, 4)

	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &milliliters)
	require.True(t, ok, "expected one row under the volume base unit (milliliters)")
	assert.Equal(t, 130.0, row.quantity)
	_, tbspRowExists := findShoppingListItem(items, itemID, &tablespoons)
	assert.False(t, tbspRowExists, "must not leave a separate tablespoons row")
}

func TestRegenerate_NullAndCountUnitsDoNotMerge(t *testing.T) {
	userID := insertTestUser(t, "Count Unit Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	cans := unitIDByName(t, "cans")
	registerShoppingListCascadeCleanup(t, userID)

	recipeA := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, nil, 2}})
	recipeB := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &cans, 3}})
	addToMenu(t, userID, recipeA, 4)
	addToMenu(t, userID, recipeB, 4)

	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	nullRow, ok := findShoppingListItem(items, itemID, nil)
	require.True(t, ok)
	assert.Equal(t, 2.0, nullRow.quantity)
	cansRow, ok := findShoppingListItem(items, itemID, &cans)
	require.True(t, ok)
	assert.Equal(t, 3.0, cansRow.quantity)
}

func TestRegenerate_PreservesObtainedOnUnrelatedMenuChange(t *testing.T) {
	userID := insertTestUser(t, "Preserve Obtained Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemA := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	itemB := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeA := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemA, &grams, 200}})
	addToMenu(t, userID, recipeA, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemA, &grams)
	require.True(t, ok)
	setShoppingListItemObtained(t, row.id, true)

	recipeB := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemB, &grams, 50}})
	addToMenu(t, userID, recipeB, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items = getShoppingListItemsByUser(t, userID)
	row, ok = findShoppingListItem(items, itemA, &grams)
	require.True(t, ok)
	assert.True(t, row.obtained, "unrelated menu change must not reset obtained")
	assert.Equal(t, 200.0, row.quantity, "unrelated item's quantity must be unchanged")
}

func TestRegenerate_QuantityChangeUpdatesRowInPlace(t *testing.T) {
	userID := insertTestUser(t, "Update In Place Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	before, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	setShoppingListItemObtained(t, before.id, true)

	// Doubling the menu-entry serves doubles the scaled quantity without changing the recipe's ingredient list.
	addToMenu(t, userID, recipeID, 8)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items = getShoppingListItemsByUser(t, userID)
	after, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.Equal(t, before.id, after.id, "row must be updated in place, not deleted and reinserted")
	assert.Equal(t, 200.0, after.quantity)
	assert.True(t, after.obtained, "quantity change alone must not reset obtained")
}

func TestRegenerate_RemovesItemNoLongerNeeded(t *testing.T) {
	userID := insertTestUser(t, "Remove Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	_, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok, "sanity check: item must be present before removal")

	require.NoError(t, menuRepo.RemoveEntry(context.Background(), userID.String(), recipeID.String()))
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items = getShoppingListItemsByUser(t, userID)
	_, ok = findShoppingListItem(items, itemID, &grams)
	assert.False(t, ok, "item no longer needed by any menu recipe must be removed")
}

func TestRegenerate_ManualItemsNeverTouched(t *testing.T) {
	userID := insertTestUser(t, "Manual Item Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	manualOnlyItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	sharedItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")

	manualOnlyRowID := insertManualShoppingListItem(t, userID, manualOnlyItem, &grams, 999, true)
	manualSharedRowID := insertManualShoppingListItem(t, userID, sharedItem, &grams, 999, false)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{sharedItem, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)

	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)

	manualOnly, ok := findShoppingListItem(items, manualOnlyItem, &grams)
	require.True(t, ok, "manual item with no recipe backing must survive regen")
	assert.Equal(t, manualOnlyRowID, manualOnly.id)
	assert.Equal(t, 999.0, manualOnly.quantity)
	assert.True(t, manualOnly.obtained)

	var manualSharedCount int
	for _, r := range items {
		if r.itemID == sharedItem && r.isManual {
			manualSharedCount++
			assert.Equal(t, manualSharedRowID, r.id)
			assert.Equal(t, 999.0, r.quantity, "manual row's quantity must not be overwritten by regen")
		}
	}
	assert.Equal(t, 1, manualSharedCount, "manual row for an item also on the generated list must be left alone")

	generatedShared, ok := findGeneratedShoppingListItem(items, sharedItem, &grams)
	require.True(t, ok, "regen must still insert its own generated row for the shared item")
	assert.Equal(t, 100.0, generatedShared.quantity)
}

func findGeneratedShoppingListItem(items []shoppingListItemRow, itemID uuid.UUID, unitID *uuid.UUID) (shoppingListItemRow, bool) {
	for _, r := range items {
		if r.isManual {
			continue
		}
		if r.itemID != itemID {
			continue
		}
		if (r.unitID == nil) != (unitID == nil) {
			continue
		}
		if r.unitID != nil && unitID != nil && *r.unitID != *unitID {
			continue
		}
		return r, true
	}
	return shoppingListItemRow{}, false
}

func TestRegenerate_DismissedItemDoesNotReappearWhenQuantityUnchanged(t *testing.T) {
	userID := insertTestUser(t, "Dismiss Stable Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)

	_, err := db.DB.Exec(context.Background(), `DELETE FROM shopping_list_items WHERE id = $1`, row.id)
	require.NoError(t, err)
	insertDismissedItem(t, userID, itemID, &grams, 100)

	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items = getShoppingListItemsByUser(t, userID)
	_, ok = findShoppingListItem(items, itemID, &grams)
	assert.False(t, ok, "dismissed item must not reappear while its required quantity is unchanged")
	assert.Equal(t, 1, countDismissedItems(t, userID, itemID), "dismissal record must survive an unchanged regen")
}

func TestRegenerate_DismissedItemReappearsWhenQuantityChanges(t *testing.T) {
	userID := insertTestUser(t, "Dismiss Reappear Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	_, err := db.DB.Exec(context.Background(), `DELETE FROM shopping_list_items WHERE id = $1`, row.id)
	require.NoError(t, err)
	insertDismissedItem(t, userID, itemID, &grams, 100)

	// Doubling serves changes the required quantity, which must invalidate the stale dismissal.
	addToMenu(t, userID, recipeID, 8)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items = getShoppingListItemsByUser(t, userID)
	reappeared, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok, "dismissed item must reappear once its required quantity changes")
	assert.Equal(t, 200.0, reappeared.quantity)
	assert.False(t, reappeared.obtained, "a reappearing item is a fresh requirement, not previously obtained")
	assert.Equal(t, 0, countDismissedItems(t, userID, itemID), "stale dismissal record must be cleaned up")
}

func strPtr(u uuid.UUID) *string {
	s := u.String()
	return &s
}

func TestAddManualItem_LazilyCreatesShoppingListRowAndInsertsNewRow(t *testing.T) {
	userID := insertTestUser(t, "Manual Add No List Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	err := shoppingListRepo.AddManualItem(context.Background(), userID.String(), itemID.String(), strPtr(grams), 3)
	require.NoError(t, err)

	var listCount int
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT count(*) FROM shopping_lists WHERE user_id = $1`, userID).Scan(&listCount))
	assert.Equal(t, 1, listCount, "manual add must lazily create the shopping_lists row")

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.Equal(t, 3.0, row.quantity)
	assert.True(t, row.isManual)
	assert.False(t, row.obtained)
}

func TestAddManualItem_MergesIntoExistingManualRow(t *testing.T) {
	userID := insertTestUser(t, "Manual Add Merge Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	existingRowID := insertManualShoppingListItem(t, userID, itemID, &grams, 2, false)

	err := shoppingListRepo.AddManualItem(context.Background(), userID.String(), itemID.String(), strPtr(grams), 5)
	require.NoError(t, err)

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.Equal(t, existingRowID, row.id, "must update the existing manual row in place, not insert a second one")
	assert.Equal(t, 7.0, row.quantity)

	var count int
	for _, r := range items {
		if r.itemID == itemID {
			count++
		}
	}
	assert.Equal(t, 1, count, "must not create a second row for the same manual item")
}

func TestAddManualItem_MergesIntoGeneratedRow(t *testing.T) {
	userID := insertTestUser(t, "Manual Add Merge Generated Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	err := shoppingListRepo.AddManualItem(context.Background(), userID.String(), itemID.String(), strPtr(grams), 3)
	require.NoError(t, err)

	items := getShoppingListItemsByUser(t, userID)
	require.Len(t, items, 1, "adding an item already on the list must not create a second row")
	assert.Equal(t, 103.0, items[0].quantity)
	assert.False(t, items[0].isManual, "the row stays recipe-driven")
}

func TestAddManualItem_MergeIntoTickedGeneratedRowUnticksIt(t *testing.T) {
	userID := insertTestUser(t, "Manual Add Untick Generated Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))
	row, ok := findShoppingListItem(getShoppingListItemsByUser(t, userID), itemID, &grams)
	require.True(t, ok)
	setShoppingListItemObtained(t, row.id, true)

	require.NoError(t, shoppingListRepo.AddManualItem(context.Background(), userID.String(), itemID.String(), strPtr(grams), 3))

	merged, ok := findShoppingListItem(getShoppingListItemsByUser(t, userID), itemID, &grams)
	require.True(t, ok)
	assert.False(t, merged.obtained, "adding more of a ticked item must untick it")
}

func TestAddManualItem_MergeIntoTickedManualRowUnticksIt(t *testing.T) {
	userID := insertTestUser(t, "Manual Add Untick Manual Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	registerShoppingListCascadeCleanup(t, userID)

	insertManualShoppingListItem(t, userID, itemID, nil, 2, true)

	require.NoError(t, shoppingListRepo.AddManualItem(context.Background(), userID.String(), itemID.String(), nil, 1))

	items := getShoppingListItemsByUser(t, userID)
	require.Len(t, items, 1)
	assert.Equal(t, 3.0, items[0].quantity)
	assert.False(t, items[0].obtained, "adding more of a ticked item must untick it")
}

func TestAddManualItem_UnknownItem_ReturnsInvalidItemError(t *testing.T) {
	userID := insertTestUser(t, "Manual Add Unknown Item Cook")
	registerShoppingListCascadeCleanup(t, userID)

	err := shoppingListRepo.AddManualItem(context.Background(), userID.String(), uuid.NewString(), nil, 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrShoppingListInvalidItem)
}

func TestAddManualItem_RejectedAdd_WithinTxLeavesNoStrayShoppingListRow(t *testing.T) {
	userID := insertTestUser(t, "Manual Add Rejected Rollback Cook")

	// Mirrors the fixed handler: wrapping in a transaction rolls back GetOrCreateShoppingList's write too.
	txErr := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
		return shoppingListRepo.AddManualItem(ctx, userID.String(), uuid.NewString(), nil, 1)
	})
	require.Error(t, txErr)
	assert.ErrorIs(t, txErr, models.ErrShoppingListInvalidItem)

	var count int
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT count(*) FROM shopping_lists WHERE user_id = $1`, userID).Scan(&count))
	assert.Equal(t, 0, count, "a rejected add wrapped in a transaction must not leave a stray shopping_lists row")
}

func TestAddManualItem_ConcurrentDuplicateAddsForNewItemMergeIntoOneRow(t *testing.T) {
	userID := insertTestUser(t, "Concurrent Manual Add Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	// Mirrors what the fixed handler does: wrap the repo call in its own transaction, same as
	// TestGetOrCreateUser_ConcurrentFirstLoginsForSameAccountBothSucceed's pattern.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = transactor.WithinTx(context.Background(), func(ctx context.Context) error {
				return shoppingListRepo.AddManualItem(ctx, userID.String(), itemID.String(), strPtr(grams), 1)
			})
		}(i)
	}
	wg.Wait()

	require.NoError(t, errs[0])
	require.NoError(t, errs[1])

	items := getShoppingListItemsByUser(t, userID)
	var count int
	var totalQuantity float64
	for _, r := range items {
		if r.itemID == itemID {
			count++
			totalQuantity = r.quantity
		}
	}
	assert.Equal(t, 1, count, "concurrent duplicate manual adds for a brand-new item must merge into one row, not two")
	assert.Equal(t, 2.0, totalQuantity, "both concurrent adds' quantities must land in the single merged row")
}

func TestAddManualItem_UnitNotAllowedForItem_ReturnsError(t *testing.T) {
	userID := insertTestUser(t, "Manual Add Unit Not Allowed Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	tablespoons := unitIDByName(t, "tablespoons")
	insertTestItemAllowedUnit(t, itemID, grams)
	registerShoppingListCascadeCleanup(t, userID)

	err := shoppingListRepo.AddManualItem(context.Background(), userID.String(), itemID.String(), strPtr(tablespoons), 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrIngredientUnitNotAllowed)

	items := getShoppingListItemsByUser(t, userID)
	assert.Empty(t, items, "a rejected add must not leave a row behind")
}

func boolPtr(b bool) *bool { return &b }

func floatPtr(f float64) *float64 { return &f }

func TestUpdateItem_TogglesObtained(t *testing.T) {
	userID := insertTestUser(t, "Update Item Obtained Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	rowID := insertManualShoppingListItem(t, userID, itemID, &grams, 2, false)

	err := shoppingListRepo.UpdateItem(context.Background(), userID.String(), rowID.String(), boolPtr(true), nil)
	require.NoError(t, err)

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.True(t, row.obtained)
	assert.Equal(t, 2.0, row.quantity, "quantity must be unchanged when only obtained is set")
}

func TestUpdateItem_EditsQuantityOnManualRow(t *testing.T) {
	userID := insertTestUser(t, "Update Item Manual Quantity Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	rowID := insertManualShoppingListItem(t, userID, itemID, &grams, 2, false)

	err := shoppingListRepo.UpdateItem(context.Background(), userID.String(), rowID.String(), nil, floatPtr(6))
	require.NoError(t, err)

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.Equal(t, 6.0, row.quantity)
	assert.False(t, row.obtained, "obtained must be unchanged when only quantity is set")
}

func TestUpdateItem_EditsQuantityOnGeneratedRow(t *testing.T) {
	userID := insertTestUser(t, "Update Item Generated Quantity Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)

	err := shoppingListRepo.UpdateItem(context.Background(), userID.String(), row.id.String(), nil, floatPtr(999))
	require.NoError(t, err)

	items = getShoppingListItemsByUser(t, userID)
	updated, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.Equal(t, 999.0, updated.quantity)
}

func TestUpdateItem_BothFieldsAtOnce(t *testing.T) {
	userID := insertTestUser(t, "Update Item Both Fields Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	rowID := insertManualShoppingListItem(t, userID, itemID, &grams, 2, false)

	err := shoppingListRepo.UpdateItem(context.Background(), userID.String(), rowID.String(), boolPtr(true), floatPtr(9))
	require.NoError(t, err)

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.True(t, row.obtained)
	assert.Equal(t, 9.0, row.quantity)
}

func TestUpdateItem_UnknownID_ReturnsNotFound(t *testing.T) {
	userID := insertTestUser(t, "Update Item Unknown ID Cook")
	registerShoppingListCascadeCleanup(t, userID)

	err := shoppingListRepo.UpdateItem(context.Background(), userID.String(), uuid.NewString(), boolPtr(true), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrShoppingListItemNotFound)
}

func TestUpdateItem_AnotherUsersRow_ReturnsNotFound(t *testing.T) {
	ownerID := insertTestUser(t, "Update Item Owner Cook")
	otherID := insertTestUser(t, "Update Item Other Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, ownerID)
	registerShoppingListCascadeCleanup(t, otherID)

	rowID := insertManualShoppingListItem(t, ownerID, itemID, &grams, 2, false)

	err := shoppingListRepo.UpdateItem(context.Background(), otherID.String(), rowID.String(), boolPtr(true), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrShoppingListItemNotFound)

	items := getShoppingListItemsByUser(t, ownerID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)
	assert.False(t, row.obtained, "another user's failed update must not have touched the row")
}

func TestDeleteItem_ManualRow_PlainDeleteNoDismissal(t *testing.T) {
	userID := insertTestUser(t, "Delete Item Manual Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	rowID := insertManualShoppingListItem(t, userID, itemID, &grams, 2, false)

	err := shoppingListRepo.DeleteItem(context.Background(), userID.String(), rowID.String())
	require.NoError(t, err)

	items := getShoppingListItemsByUser(t, userID)
	assert.Empty(t, items)
	assert.Equal(t, 0, countDismissedItems(t, userID, itemID), "deleting a manual item must not write a dismissal record")
}

func TestDeleteItem_GeneratedRow_DeletesAndWritesDismissal(t *testing.T) {
	userID := insertTestUser(t, "Delete Item Generated Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok)

	err := shoppingListRepo.DeleteItem(context.Background(), userID.String(), row.id.String())
	require.NoError(t, err)

	items = getShoppingListItemsByUser(t, userID)
	_, ok = findShoppingListItem(items, itemID, &grams)
	assert.False(t, ok, "row must be deleted")
	assert.Equal(t, 1, countDismissedItems(t, userID, itemID), "deleting a generated item must write a dismissal record")

	// An unrelated regen must not bring it back while the required quantity is unchanged.
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))
	items = getShoppingListItemsByUser(t, userID)
	_, ok = findShoppingListItem(items, itemID, &grams)
	assert.False(t, ok, "dismissed item must not reappear while its required quantity is unchanged")
}

func TestDeleteItem_GeneratedRowAddedTo_StaysDismissedOnUnrelatedMenuChange(t *testing.T) {
	assertDeletedAdjustedRowStaysDismissed(t, "Delete Added-To Row Cook", func(userID, itemID, rowID uuid.UUID, grams uuid.UUID) {
		require.NoError(t, shoppingListRepo.AddManualItem(context.Background(), userID.String(), itemID.String(), strPtr(grams), 3))
	})
}

func TestDeleteItem_GeneratedRowQuantityEdited_StaysDismissedOnUnrelatedMenuChange(t *testing.T) {
	assertDeletedAdjustedRowStaysDismissed(t, "Delete Edited Row Cook", func(userID, _, rowID uuid.UUID, _ uuid.UUID) {
		require.NoError(t, shoppingListRepo.UpdateItem(context.Background(), userID.String(), rowID.String(), nil, floatPtr(999)))
	})
}

func assertDeletedAdjustedRowStaysDismissed(t *testing.T, name string, adjust func(userID, itemID, rowID, grams uuid.UUID)) {
	t.Helper()
	userID := insertTestUser(t, name)
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	unrelatedItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeA := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeA, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))
	row, ok := findShoppingListItem(getShoppingListItemsByUser(t, userID), itemID, &grams)
	require.True(t, ok)

	adjust(userID, itemID, row.id, grams)
	require.NoError(t, shoppingListRepo.DeleteItem(context.Background(), userID.String(), row.id.String()))

	recipeB := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{unrelatedItem, &grams, 50}})
	addToMenu(t, userID, recipeB, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	_, ok = findShoppingListItem(getShoppingListItemsByUser(t, userID), itemID, &grams)
	assert.False(t, ok, "a deleted row must stay dismissed while the recipes' need is unchanged, whatever its displayed quantity was")
}

func TestDeleteItem_UnknownID_ReturnsNotFound(t *testing.T) {
	userID := insertTestUser(t, "Delete Item Unknown ID Cook")
	registerShoppingListCascadeCleanup(t, userID)

	err := shoppingListRepo.DeleteItem(context.Background(), userID.String(), uuid.NewString())
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrShoppingListItemNotFound)
}

func TestDeleteItem_AnotherUsersRow_ReturnsNotFoundAndLeavesRowIntact(t *testing.T) {
	ownerID := insertTestUser(t, "Delete Item Owner Cook")
	otherID := insertTestUser(t, "Delete Item Other Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, ownerID)
	registerShoppingListCascadeCleanup(t, otherID)

	rowID := insertManualShoppingListItem(t, ownerID, itemID, &grams, 2, false)

	err := shoppingListRepo.DeleteItem(context.Background(), otherID.String(), rowID.String())
	require.Error(t, err)
	assert.ErrorIs(t, err, models.ErrShoppingListItemNotFound)

	items := getShoppingListItemsByUser(t, ownerID)
	_, ok := findShoppingListItem(items, itemID, &grams)
	assert.True(t, ok, "another user's failed delete must not have removed the row")
}

func TestClearList_RemovesAllItems_ManualAndGenerated(t *testing.T) {
	userID := insertTestUser(t, "Clear List Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	manualItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	generatedItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	insertManualShoppingListItem(t, userID, manualItem, &grams, 2, false)
	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{generatedItem, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	require.Len(t, items, 2, "sanity check: both items must be present before clearing")

	err := shoppingListRepo.ClearList(context.Background(), userID.String())
	require.NoError(t, err)

	items = getShoppingListItemsByUser(t, userID)
	assert.Empty(t, items)
}

func TestClearList_NoExistingList_NoOp(t *testing.T) {
	userID := insertTestUser(t, "Clear List No List Cook")

	err := shoppingListRepo.ClearList(context.Background(), userID.String())
	require.NoError(t, err)

	var count int
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT count(*) FROM shopping_lists WHERE user_id = $1`, userID).Scan(&count))
	assert.Equal(t, 0, count, "clearing a list that never existed must not create one")
}

func TestClearList_DoesNotWriteDismissals_ItemReappearsOnNextRegen(t *testing.T) {
	userID := insertTestUser(t, "Clear List No Dismissal Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	require.NoError(t, shoppingListRepo.ClearList(context.Background(), userID.String()))
	assert.Equal(t, 0, countDismissedItems(t, userID, itemID), "clear list must not write dismissal records")

	// The recipe is still on the menu, so the very next regenerate must bring the item straight back.
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))
	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemID, &grams)
	require.True(t, ok, "an unrelated dismissal must not have suppressed the still-needed item")
	assert.Equal(t, 100.0, row.quantity)
}

func TestClearList_OnlyClearsCallingUsersList(t *testing.T) {
	clearedUserID := insertTestUser(t, "Clear List Target Cook")
	otherUserID := insertTestUser(t, "Clear List Bystander Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, clearedUserID)
	registerShoppingListCascadeCleanup(t, otherUserID)

	insertManualShoppingListItem(t, clearedUserID, itemID, &grams, 2, false)
	insertManualShoppingListItem(t, otherUserID, itemID, &grams, 3, false)

	require.NoError(t, shoppingListRepo.ClearList(context.Background(), clearedUserID.String()))

	assert.Empty(t, getShoppingListItemsByUser(t, clearedUserID))
	assert.Len(t, getShoppingListItemsByUser(t, otherUserID), 1, "another user's list must be untouched")
}

func TestRegenerate_HandEditedGeneratedQuantityResetsOnUnrelatedMenuChange(t *testing.T) {
	userID := insertTestUser(t, "Hand Edit Reset Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemA := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	itemB := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeA := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemA, &grams, 100}})
	addToMenu(t, userID, recipeA, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	row, ok := findShoppingListItem(items, itemA, &grams)
	require.True(t, ok)

	require.NoError(t, shoppingListRepo.UpdateItem(context.Background(), userID.String(), row.id.String(), nil, floatPtr(999)))
	items = getShoppingListItemsByUser(t, userID)
	edited, ok := findShoppingListItem(items, itemA, &grams)
	require.True(t, ok)
	require.Equal(t, 999.0, edited.quantity, "sanity check: the hand edit must have taken")

	// An unrelated recipe joining the menu triggers a regenerate that must recompute every generated row.
	recipeB := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemB, &grams, 50}})
	addToMenu(t, userID, recipeB, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items = getShoppingListItemsByUser(t, userID)
	reset, ok := findShoppingListItem(items, itemA, &grams)
	require.True(t, ok)
	assert.Equal(t, 100.0, reset.quantity, "hand-edited quantity on a generated row must reset to the recipe-calculated amount on the next regenerate")
}

func TestGet_NoShoppingListRow_ReturnsEmptyWithoutCreatingOne(t *testing.T) {
	userID := insertTestUser(t, "Get No List Cook")

	list, err := shoppingListRepo.Get(context.Background(), userID.String())
	require.NoError(t, err)
	assert.Empty(t, list.Items)

	var count int
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT count(*) FROM shopping_lists WHERE user_id = $1`, userID).Scan(&count))
	assert.Equal(t, 0, count, "GET must never create a shopping_lists row")
}

func TestGet_ReturnsHydratedManualItem(t *testing.T) {
	userID := insertTestUser(t, "Get Hydrated Cook")
	catName := "repo-test-sl-cat-" + uuid.NewString()
	cat := insertTestItemCategory(t, catName, "repo-test-sl-icon-"+uuid.NewString())
	itemName := "repo-test-sl-item-" + uuid.NewString()
	itemID := insertTestItem(t, itemName, cat)
	grams := unitIDByName(t, "grams")
	gramsAbbreviation := unitAbbreviationByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	rowID := insertManualShoppingListItem(t, userID, itemID, &grams, 250, true)

	list, err := shoppingListRepo.Get(context.Background(), userID.String())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)

	got := list.Items[0]
	assert.Equal(t, rowID, got.ID)
	assert.Equal(t, itemID, got.ItemID)
	assert.Equal(t, itemName, got.ItemName)
	assert.Equal(t, cat, got.ItemCategoryID)
	assert.Equal(t, catName, got.ItemCategoryName)
	require.NotNil(t, got.UnitID)
	assert.Equal(t, grams, *got.UnitID)
	require.NotNil(t, got.UnitAbbreviation)
	assert.Equal(t, gramsAbbreviation, *got.UnitAbbreviation)
	assert.Equal(t, 250.0, got.Quantity)
	assert.True(t, got.Obtained)
	assert.True(t, got.IsManual)
}

func TestGet_NullUnitItem_UnitFieldsAreNil(t *testing.T) {
	userID := insertTestUser(t, "Get Null Unit Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	registerShoppingListCascadeCleanup(t, userID)

	insertManualShoppingListItem(t, userID, itemID, nil, 2, false)

	list, err := shoppingListRepo.Get(context.Background(), userID.String())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, itemID, list.Items[0].ItemID)
	assert.Nil(t, list.Items[0].UnitID)
	assert.Nil(t, list.Items[0].UnitAbbreviation)
}

func TestGet_ReflectsRegenerate(t *testing.T) {
	userID := insertTestUser(t, "Get Reflects Regen Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 120}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	list, err := shoppingListRepo.Get(context.Background(), userID.String())
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, itemID, list.Items[0].ItemID)
	assert.Equal(t, 120.0, list.Items[0].Quantity)
	assert.False(t, list.Items[0].IsManual)
}

func TestRegenerateFromScratch_RestoresListToInitialState(t *testing.T) {
	userID := insertTestUser(t, "Regen From Scratch Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	tickedItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	dismissedItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	editedItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	manualItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{
		{tickedItem, &grams, 100},
		{dismissedItem, &grams, 200},
		{editedItem, &grams, 50},
	})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))

	items := getShoppingListItemsByUser(t, userID)
	ticked, ok := findShoppingListItem(items, tickedItem, &grams)
	require.True(t, ok)
	setShoppingListItemObtained(t, ticked.id, true)
	dismissed, ok := findShoppingListItem(items, dismissedItem, &grams)
	require.True(t, ok)
	require.NoError(t, shoppingListRepo.DeleteItem(context.Background(), userID.String(), dismissed.id.String()))
	edited, ok := findShoppingListItem(items, editedItem, &grams)
	require.True(t, ok)
	require.NoError(t, shoppingListRepo.UpdateItem(context.Background(), userID.String(), edited.id.String(), nil, floatPtr(999)))
	insertManualShoppingListItem(t, userID, manualItem, &grams, 3, false)

	require.NoError(t, shoppingListRepo.RegenerateFromScratch(context.Background(), userID.String()))

	items = getShoppingListItemsByUser(t, userID)
	require.Len(t, items, 3, "only the menu's recipe-driven items must remain")
	for itemID, want := range map[uuid.UUID]float64{tickedItem: 100, dismissedItem: 200, editedItem: 50} {
		row, ok := findShoppingListItem(items, itemID, &grams)
		require.True(t, ok, "every recipe-driven item must be on the list")
		assert.Equal(t, want, row.quantity, "quantity must be the recipe-calculated amount")
		assert.False(t, row.obtained, "every row must come back unticked")
		assert.False(t, row.isManual)
	}
	assert.Equal(t, 0, countDismissedItems(t, userID, dismissedItem), "dismissal records must be cleared")

	// Dismissal still works as normal after a from-scratch regenerate.
	restored, _ := findShoppingListItem(items, dismissedItem, &grams)
	require.NoError(t, shoppingListRepo.DeleteItem(context.Background(), userID.String(), restored.id.String()))
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))
	_, ok = findShoppingListItem(getShoppingListItemsByUser(t, userID), dismissedItem, &grams)
	assert.False(t, ok, "an item deleted after a from-scratch regenerate must stay dismissed")
}

func TestRegenerateFromScratch_RebuildsAfterClearList(t *testing.T) {
	userID := insertTestUser(t, "Regen From Scratch After Clear Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	recipeID := insertTestRecipeWithIngredients(t, userID, 4, []testIngredient{{itemID, &grams, 100}})
	addToMenu(t, userID, recipeID, 4)
	require.NoError(t, shoppingListRepo.Regenerate(context.Background(), userID.String()))
	require.NoError(t, shoppingListRepo.ClearList(context.Background(), userID.String()))
	require.Empty(t, getShoppingListItemsByUser(t, userID), "sanity check: list must be empty after clearing")

	require.NoError(t, shoppingListRepo.RegenerateFromScratch(context.Background(), userID.String()))

	row, ok := findShoppingListItem(getShoppingListItemsByUser(t, userID), itemID, &grams)
	require.True(t, ok, "the menu's items must be rebuilt after a clear")
	assert.Equal(t, 100.0, row.quantity)
}

func TestRegenerateFromScratch_NoShoppingListRow_CreatesEmptyList(t *testing.T) {
	userID := insertTestUser(t, "Regen From Scratch No List Cook")
	registerShoppingListCascadeCleanup(t, userID)

	require.NoError(t, shoppingListRepo.RegenerateFromScratch(context.Background(), userID.String()))

	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM shopping_lists WHERE user_id = $1`, userID), "a shopping list row must be created")
	assert.Empty(t, getShoppingListItemsByUser(t, userID), "an empty menu must produce an empty list")
}

func TestRegenerateFromScratch_OnlyAffectsCallingUsersList(t *testing.T) {
	targetID := insertTestUser(t, "Regen From Scratch Target Cook")
	otherID := insertTestUser(t, "Regen From Scratch Bystander Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, targetID)
	registerShoppingListCascadeCleanup(t, otherID)

	insertManualShoppingListItem(t, targetID, itemID, &grams, 2, false)
	insertManualShoppingListItem(t, otherID, itemID, &grams, 3, false)
	insertDismissedItem(t, otherID, itemID, &grams, 5)

	require.NoError(t, shoppingListRepo.RegenerateFromScratch(context.Background(), targetID.String()))

	assert.Empty(t, getShoppingListItemsByUser(t, targetID), "sanity check: the calling user's list must be reset")
	assert.Len(t, getShoppingListItemsByUser(t, otherID), 1, "another user's items must be untouched")
	assert.Equal(t, 1, countDismissedItems(t, otherID, itemID), "another user's dismissals must be untouched")
}

func TestRegenerateFromScratch_WithinTxRollsBackOnError(t *testing.T) {
	userID := insertTestUser(t, "Regen From Scratch Rollback Cook")
	cat := insertTestItemCategory(t, "repo-test-sl-cat-"+uuid.NewString(), "repo-test-sl-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	dismissedItem := insertTestItem(t, "repo-test-sl-item-"+uuid.NewString(), cat)
	grams := unitIDByName(t, "grams")
	registerShoppingListCascadeCleanup(t, userID)

	insertManualShoppingListItem(t, userID, itemID, &grams, 2, false)
	insertDismissedItem(t, userID, dismissedItem, &grams, 5)
	sentinelErr := errors.New("boom")

	txErr := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
		require.NoError(t, shoppingListRepo.RegenerateFromScratch(ctx, userID.String()))
		list, err := shoppingListRepo.Get(ctx, userID.String())
		require.NoError(t, err)
		require.Empty(t, list.Items, "sanity check: the reset must be visible inside the transaction")
		return sentinelErr
	})
	require.ErrorIs(t, txErr, sentinelErr)

	assert.Len(t, getShoppingListItemsByUser(t, userID), 1, "the manual item must survive the rolled-back reset")
	assert.Equal(t, 1, countDismissedItems(t, userID, dismissedItem), "the dismissal must survive the rolled-back reset")
}
