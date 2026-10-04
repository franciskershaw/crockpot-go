package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// restockFixture creates its user after the catalog fixtures, so the user's cascade (list rows, regulars) runs before their RESTRICT cleanups.
type restockFixture struct {
	cat  uuid.UUID
	unit uuid.UUID
}

func newRestockFixture(t *testing.T) restockFixture {
	t.Helper()
	return restockFixture{
		cat:  newRegularCategory(t, "repo-test-restock"),
		unit: newRegularUnit(t),
	}
}

func (f restockFixture) item(t *testing.T) uuid.UUID {
	t.Helper()
	return newRegularItem(t, f.cat, "repo-test-restock-item")
}

func addRegular(t *testing.T, userID, itemID uuid.UUID, unitID *uuid.UUID, quantity float64) string {
	t.Helper()
	var unit *string
	if unitID != nil {
		unit = strPtr(*unitID)
	}
	regular, err := regularRepo.Create(context.Background(), userID.String(), itemID.String(), unit, quantity, testRegularsLimit)
	require.NoError(t, err)
	return regular.ID.String()
}

func TestRestock_InsertsMissingRegularAsUntickedManualRow(t *testing.T) {
	f := newRestockFixture(t)
	item := f.item(t)
	user := createTestUser(t)
	regular := addRegular(t, user, item, &f.unit, 9)

	require.NoError(t, shoppingListRepo.Restock(context.Background(), user.String(), []string{regular}))

	items := getShoppingListItemsByUser(t, user)
	require.Len(t, items, 1)
	row, ok := findShoppingListItem(items, item, &f.unit)
	require.True(t, ok)
	assert.Equal(t, 9.0, row.quantity)
	assert.False(t, row.obtained)
	assert.True(t, row.isManual)
}

func TestRestock_LeavesUnboughtRowsUntouched(t *testing.T) {
	f := newRestockFixture(t)
	manualItem := f.item(t)
	recipeItem := f.item(t)
	user := createTestUser(t)
	manualRow := insertShoppingListItemRow(t, user, manualItem, &f.unit, 3, false, true)
	recipeRow := insertShoppingListItemRow(t, user, recipeItem, &f.unit, 5, false, false)
	before := getShoppingListItemsByUser(t, user)

	err := shoppingListRepo.Restock(context.Background(), user.String(), []string{
		addRegular(t, user, manualItem, &f.unit, 9),
		addRegular(t, user, recipeItem, &f.unit, 9),
	})

	require.NoError(t, err)
	after := getShoppingListItemsByUser(t, user)
	assert.Equal(t, before, after, "a regular already on the list unbought must be skipped, never added to")
	require.Len(t, after, 2)
	assert.ElementsMatch(t, []uuid.UUID{manualRow, recipeRow}, []uuid.UUID{after[0].id, after[1].id})
}

func TestRestock_ResetsBoughtManualRow(t *testing.T) {
	f := newRestockFixture(t)
	item := f.item(t)
	user := createTestUser(t)
	rowID := insertShoppingListItemRow(t, user, item, &f.unit, 3, true, true)

	require.NoError(t, shoppingListRepo.Restock(context.Background(), user.String(), []string{addRegular(t, user, item, &f.unit, 9)}))

	items := getShoppingListItemsByUser(t, user)
	require.Len(t, items, 1)
	assert.Equal(t, rowID, items[0].id)
	assert.False(t, items[0].obtained)
	assert.Equal(t, 9.0, items[0].quantity, "reset to the regular's quantity, not added to the old one")
	assert.True(t, items[0].isManual)
}

func TestRestock_ResetsBoughtRecipeRowAndKeepsItRecipeDriven(t *testing.T) {
	f := newRestockFixture(t)
	item := f.item(t)
	user := createTestUser(t)
	rowID := insertShoppingListItemRow(t, user, item, &f.unit, 3, true, false)

	require.NoError(t, shoppingListRepo.Restock(context.Background(), user.String(), []string{addRegular(t, user, item, &f.unit, 9)}))

	items := getShoppingListItemsByUser(t, user)
	require.Len(t, items, 1)
	assert.Equal(t, rowID, items[0].id)
	assert.False(t, items[0].obtained)
	assert.Equal(t, 9.0, items[0].quantity)
	assert.False(t, items[0].isManual)
}

func TestRestock_PrefersManualRowWhenManualAndRecipeRowsAreBothBought(t *testing.T) {
	f := newRestockFixture(t)
	item := f.item(t)
	user := createTestUser(t)
	manualRow := insertShoppingListItemRow(t, user, item, &f.unit, 3, true, true)
	recipeRow := insertShoppingListItemRow(t, user, item, &f.unit, 4, true, false)

	require.NoError(t, shoppingListRepo.Restock(context.Background(), user.String(), []string{addRegular(t, user, item, &f.unit, 9)}))

	byID := map[uuid.UUID]shoppingListItemRow{}
	for _, r := range getShoppingListItemsByUser(t, user) {
		byID[r.id] = r
	}
	require.Len(t, byID, 2)
	assert.False(t, byID[manualRow].obtained)
	assert.Equal(t, 9.0, byID[manualRow].quantity)
	assert.True(t, byID[recipeRow].obtained, "only one row is reset")
	assert.Equal(t, 4.0, byID[recipeRow].quantity)
}

func TestRestock_MatchesOnExactUnitWithNullMatchingNull(t *testing.T) {
	f := newRestockFixture(t)
	unitlessRegularItem := f.item(t)
	unitRegularItem := f.item(t)
	nullUnitItem := f.item(t)
	user := createTestUser(t)
	insertShoppingListItemRow(t, user, unitlessRegularItem, &f.unit, 3, false, true)
	insertShoppingListItemRow(t, user, unitRegularItem, nil, 3, false, true)
	nullRow := insertShoppingListItemRow(t, user, nullUnitItem, nil, 3, false, true)

	err := shoppingListRepo.Restock(context.Background(), user.String(), []string{
		addRegular(t, user, unitlessRegularItem, nil, 9),
		addRegular(t, user, unitRegularItem, &f.unit, 9),
		addRegular(t, user, nullUnitItem, nil, 9),
	})

	require.NoError(t, err)
	items := getShoppingListItemsByUser(t, user)
	assert.Len(t, items, 5)
	inserted, ok := findShoppingListItem(items, unitlessRegularItem, nil)
	require.True(t, ok, "a unitless regular doesn't match a row with a unit")
	assert.Equal(t, 9.0, inserted.quantity)
	inserted, ok = findShoppingListItem(items, unitRegularItem, &f.unit)
	require.True(t, ok, "a regular with a unit doesn't match a unitless row")
	assert.Equal(t, 9.0, inserted.quantity)
	skipped, ok := findShoppingListItem(items, nullUnitItem, nil)
	require.True(t, ok)
	assert.Equal(t, nullRow, skipped.id)
	assert.Equal(t, 3.0, skipped.quantity, "null unit matches null unit, so the unbought row is skipped")
}

func TestRestock_SecondRunChangesNothing(t *testing.T) {
	f := newRestockFixture(t)
	missing := f.item(t)
	bought := f.item(t)
	user := createTestUser(t)
	insertShoppingListItemRow(t, user, bought, &f.unit, 3, true, true)
	regulars := []string{addRegular(t, user, missing, &f.unit, 9), addRegular(t, user, bought, &f.unit, 9)}

	require.NoError(t, shoppingListRepo.Restock(context.Background(), user.String(), regulars))
	first := getShoppingListItemsByUser(t, user)
	require.NoError(t, shoppingListRepo.Restock(context.Background(), user.String(), regulars))

	assert.Len(t, first, 2)
	assert.Equal(t, first, getShoppingListItemsByUser(t, user))
}

func TestRestock_OnlyRestocksSelectedRegulars(t *testing.T) {
	f := newRestockFixture(t)
	picked := f.item(t)
	notPicked := f.item(t)
	user := createTestUser(t)
	regular := addRegular(t, user, picked, &f.unit, 9)
	addRegular(t, user, notPicked, &f.unit, 9)

	require.NoError(t, shoppingListRepo.Restock(context.Background(), user.String(), []string{regular}))

	items := getShoppingListItemsByUser(t, user)
	require.Len(t, items, 1)
	assert.Equal(t, picked, items[0].itemID)
}

func TestRestock_IgnoresOtherUsersAndUnknownIDs(t *testing.T) {
	f := newRestockFixture(t)
	own := f.item(t)
	theirs := f.item(t)
	user := createTestUser(t)
	other := createTestUser(t)
	ownRegular := addRegular(t, user, own, &f.unit, 9)
	otherRegular := addRegular(t, other, theirs, &f.unit, 9)

	err := shoppingListRepo.Restock(context.Background(), user.String(), []string{ownRegular, otherRegular, uuid.NewString()})

	require.NoError(t, err)
	items := getShoppingListItemsByUser(t, user)
	require.Len(t, items, 1)
	assert.Equal(t, own, items[0].itemID)
	assert.Empty(t, getShoppingListItemsByUser(t, other))
}

func TestRestock_RollsBackWithTheActiveTransaction(t *testing.T) {
	f := newRestockFixture(t)
	missing := f.item(t)
	bought := f.item(t)
	user := createTestUser(t)
	insertShoppingListItemRow(t, user, bought, &f.unit, 3, true, true)
	regulars := []string{addRegular(t, user, missing, &f.unit, 9), addRegular(t, user, bought, &f.unit, 9)}
	before := getShoppingListItemsByUser(t, user)
	boom := errors.New("boom")

	err := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
		if err := shoppingListRepo.Restock(ctx, user.String(), regulars); err != nil {
			return err
		}
		return boom
	})

	assert.ErrorIs(t, err, boom)
	assert.Equal(t, before, getShoppingListItemsByUser(t, user))
}

func TestRestock_ConcurrentRestockWaitsAndAddsOneRow(t *testing.T) {
	ctx := context.Background()
	f := newRestockFixture(t)
	item := f.item(t)
	user := createTestUser(t)
	getOrCreateShoppingListID(t, user)
	regulars := []string{addRegular(t, user, item, &f.unit, 9)}

	restocked := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			if err := shoppingListRepo.Restock(ctx, user.String(), regulars); err != nil {
				return err
			}
			close(restocked)
			<-release
			return nil
		})
	}()
	select {
	case <-restocked:
	case err := <-firstErr:
		t.Fatalf("first restock failed before holding its transaction open: %v", err)
	}

	secondErr := make(chan error, 1)
	go func() {
		secondErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			return shoppingListRepo.Restock(ctx, user.String(), regulars)
		})
	}()

	waitForLockWait(t)
	releaseOnce(release)

	require.NoError(t, <-firstErr)
	require.NoError(t, <-secondErr)
	items := getShoppingListItemsByUser(t, user)
	assert.Len(t, items, 1, "a restock that waited must see the first one's row and skip, not insert a second")
}
