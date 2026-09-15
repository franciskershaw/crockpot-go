package repository_test

import (
	"context"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
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
