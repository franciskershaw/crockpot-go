package repository_test

import (
	"context"
	"sort"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertTestItem(t *testing.T, name string, categoryID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO items (id, name, category_id) VALUES ($1, $2, $3)`,
		id, name, categoryID,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM items WHERE id = $1`, id)
	return id
}

func insertTestItemAllowedUnit(t *testing.T, itemID, unitID uuid.UUID) {
	t.Helper()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO item_allowed_units (item_id, unit_id) VALUES ($1, $2)`,
		itemID, unitID,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM item_allowed_units WHERE item_id = $1 AND unit_id = $2`, itemID, unitID)
}

func getItemAllowedUnitIDs(t *testing.T, itemID uuid.UUID) []uuid.UUID {
	t.Helper()
	rows, err := db.DB.Query(context.Background(),
		`SELECT unit_id FROM item_allowed_units WHERE item_id = $1 ORDER BY unit_id`, itemID)
	require.NoError(t, err)
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

func sortedUUIDs(ids []uuid.UUID) []uuid.UUID {
	sorted := make([]uuid.UUID, len(ids))
	copy(sorted, ids)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].String() < sorted[j].String() })
	return sorted
}

func TestCreateItem_NoAllowedUnits(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	name := "repo-test-item-" + uuid.NewString()

	item, err := itemRepo.Create(ctx, name, categoryID.String(), nil)
	require.NoError(t, err)
	require.NotNil(t, item)
	cleanupExec(t, `DELETE FROM items WHERE id = $1`, item.ID)

	assert.Equal(t, name, item.Name)
	assert.Equal(t, categoryID, item.CategoryID)
	assert.Empty(t, item.AllowedUnitIDs)
	assert.NotNil(t, item.AllowedUnitIDs, "expected an empty slice, not nil")
}

func TestCreateItem_WithAllowedUnits(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	unit1 := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	unit2 := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	name := "repo-test-item-" + uuid.NewString()

	item, err := itemRepo.Create(ctx, name, categoryID.String(), []string{unit1.String(), unit2.String()})
	require.NoError(t, err)
	require.NotNil(t, item)
	cleanupExec(t, `DELETE FROM items WHERE id = $1`, item.ID)

	assert.ElementsMatch(t, []uuid.UUID{unit1, unit2}, item.AllowedUnitIDs)
	assert.Equal(t, sortedUUIDs([]uuid.UUID{unit1, unit2}), sortedUUIDs(getItemAllowedUnitIDs(t, item.ID)))
}

func TestCreateItem_DuplicateName(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	name := "repo-test-item-" + uuid.NewString()
	insertTestItem(t, name, categoryID)

	item, err := itemRepo.Create(ctx, name, categoryID.String(), nil)
	assert.Nil(t, item)
	assert.ErrorIs(t, err, models.ErrItemNameTaken)
}

func TestCreateItem_InvalidCategory(t *testing.T) {
	ctx := context.Background()

	item, err := itemRepo.Create(ctx, "repo-test-item-"+uuid.NewString(), uuid.NewString(), nil)
	assert.Nil(t, item)
	assert.ErrorIs(t, err, models.ErrItemInvalidCategory)
}

func TestCreateItem_InvalidUnit(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	name := "repo-test-item-" + uuid.NewString()
	// Called unwrapped (no transactor), so the items row from step 1 legitimately
	// persists despite the allowed-unit insert failing — clean it up by name.
	cleanupExec(t, `DELETE FROM items WHERE name = $1`, name)

	item, err := itemRepo.Create(ctx, name, categoryID.String(), []string{uuid.NewString()})
	assert.Nil(t, item)
	assert.ErrorIs(t, err, models.ErrItemInvalidUnit)
}

func TestCreateItem_RollsBackOnInvalidUnitID(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	name := "repo-test-item-" + uuid.NewString()

	txErr := transactor.WithinTx(ctx, func(ctx context.Context) error {
		_, err := itemRepo.Create(ctx, name, categoryID.String(), []string{uuid.NewString()})
		return err
	})
	assert.ErrorIs(t, txErr, models.ErrItemInvalidUnit)

	var count int
	row := db.DB.QueryRow(ctx, `SELECT COUNT(*) FROM items WHERE name = $1`, name)
	require.NoError(t, row.Scan(&count))
	assert.Equal(t, 0, count, "expected no item row to survive the rolled-back transaction")
}

func TestListItems_OrderedByName_GroupsAllowedUnitsCorrectly(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	unitA := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	unitB := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())

	prefix := "repo-test-list-item-" + uuid.NewString() + "-"
	zedID := insertTestItem(t, prefix+"Zed", categoryID)
	insertTestItemAllowedUnit(t, zedID, unitA)
	alphaID := insertTestItem(t, prefix+"Alpha", categoryID)
	insertTestItemAllowedUnit(t, alphaID, unitB)

	items, err := itemRepo.List(ctx)
	require.NoError(t, err)

	var gotNames []string
	byName := map[string]*models.Item{}
	for _, item := range items {
		if len(item.Name) >= len(prefix) && item.Name[:len(prefix)] == prefix {
			gotNames = append(gotNames, item.Name)
			byName[item.Name] = item
		}
	}
	require.Equal(t, []string{prefix + "Alpha", prefix + "Zed"}, gotNames)
	assert.Equal(t, []uuid.UUID{unitB}, byName[prefix+"Alpha"].AllowedUnitIDs, "alpha's units must not leak zed's")
	assert.Equal(t, []uuid.UUID{unitA}, byName[prefix+"Zed"].AllowedUnitIDs, "zed's units must not leak alpha's")
}

func TestUpdateItem_PartialName(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	id := insertTestItem(t, "repo-test-item-"+uuid.NewString(), categoryID)
	newName := "repo-test-item-" + uuid.NewString()

	updated, err := itemRepo.Update(ctx, id.String(), newName, "", nil)
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, newName, updated.Name)
	assert.Equal(t, categoryID, updated.CategoryID, "category should be left unchanged")
}

func TestUpdateItem_PartialCategory(t *testing.T) {
	ctx := context.Background()
	originalCategory := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	newCategory := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	name := "repo-test-item-" + uuid.NewString()
	id := insertTestItem(t, name, originalCategory)

	updated, err := itemRepo.Update(ctx, id.String(), "", newCategory.String(), nil)
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, name, updated.Name, "name should be left unchanged")
	assert.Equal(t, newCategory, updated.CategoryID)
}

func TestUpdateItem_ReplaceAllowedUnits(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	oldUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	newUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	id := insertTestItem(t, "repo-test-item-"+uuid.NewString(), categoryID)
	insertTestItemAllowedUnit(t, id, oldUnit)

	updated, err := itemRepo.Update(ctx, id.String(), "", "", []string{newUnit.String()})
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, []uuid.UUID{newUnit}, updated.AllowedUnitIDs)
	assert.Equal(t, []uuid.UUID{newUnit}, getItemAllowedUnitIDs(t, id))
}

func TestUpdateItem_ClearAllowedUnits(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	oldUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	id := insertTestItem(t, "repo-test-item-"+uuid.NewString(), categoryID)
	insertTestItemAllowedUnit(t, id, oldUnit)

	updated, err := itemRepo.Update(ctx, id.String(), "", "", []string{})
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Empty(t, updated.AllowedUnitIDs)
	assert.Empty(t, getItemAllowedUnitIDs(t, id))
}

func TestUpdateItem_OmitAllowedUnits_LeavesUnchanged(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	existingUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	id := insertTestItem(t, "repo-test-item-"+uuid.NewString(), categoryID)
	insertTestItemAllowedUnit(t, id, existingUnit)
	newName := "repo-test-item-" + uuid.NewString()

	updated, err := itemRepo.Update(ctx, id.String(), newName, "", nil)
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, []uuid.UUID{existingUnit}, updated.AllowedUnitIDs, "omitted allowedUnitIds must leave the existing set untouched")
}

func TestUpdateItem_DuplicateName(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	takenName := "repo-test-item-" + uuid.NewString()
	insertTestItem(t, takenName, categoryID)
	id := insertTestItem(t, "repo-test-item-"+uuid.NewString(), categoryID)

	updated, err := itemRepo.Update(ctx, id.String(), takenName, "", nil)
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrItemNameTaken)
}

func TestUpdateItem_NotFound(t *testing.T) {
	ctx := context.Background()

	updated, err := itemRepo.Update(ctx, uuid.NewString(), "repo-test-item-"+uuid.NewString(), "", nil)
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrItemNotFound)
}

func TestDeleteItem_Success_CascadesAllowedUnits(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	unit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	id := insertTestItem(t, "repo-test-item-"+uuid.NewString(), categoryID)
	insertTestItemAllowedUnit(t, id, unit)

	err := itemRepo.Delete(ctx, id.String())
	require.NoError(t, err)

	row := db.DB.QueryRow(ctx, `SELECT id FROM items WHERE id = $1`, id)
	var found uuid.UUID
	assert.ErrorIs(t, row.Scan(&found), pgx.ErrNoRows, "expected the item row to be gone")
	assert.Empty(t, getItemAllowedUnitIDs(t, id), "expected item_allowed_units rows to cascade away")
}

func TestDeleteItem_NotFound(t *testing.T) {
	ctx := context.Background()

	err := itemRepo.Delete(ctx, uuid.NewString())
	assert.ErrorIs(t, err, models.ErrItemNotFound)
}

func TestDeleteItem_InUse(t *testing.T) {
	ctx := context.Background()
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), categoryID)

	shoppingListUserID := uuid.New()
	_, err := db.DB.Exec(ctx,
		`INSERT INTO users (id, google_id, email) VALUES ($1, $2, $3)`,
		shoppingListUserID, "repo-test-google-"+uuid.NewString(), "repo-test-"+uuid.NewString()+"@example.com",
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, shoppingListUserID)

	shoppingListID := uuid.New()
	_, err = db.DB.Exec(ctx,
		`INSERT INTO shopping_lists (id, user_id) VALUES ($1, $2)`,
		shoppingListID, shoppingListUserID,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM shopping_lists WHERE id = $1`, shoppingListID)

	shoppingListItemID := uuid.New()
	_, err = db.DB.Exec(ctx,
		`INSERT INTO shopping_list_items (id, shopping_list_id, item_id, quantity) VALUES ($1, $2, $3, $4)`,
		shoppingListItemID, shoppingListID, itemID, 1,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM shopping_list_items WHERE id = $1`, shoppingListItemID)

	deleteErr := itemRepo.Delete(ctx, itemID.String())
	assert.ErrorIs(t, deleteErr, models.ErrItemInUse)
}
