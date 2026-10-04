package repository_test

import (
	"context"
	"testing"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testRegularsLimit = 50

// newRegularItem inserts a catalog item; regulars pointing at it are removed before it is, since items restrict deletion.
func newRegularItem(t *testing.T, categoryID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := insertTestItem(t, name+"-"+uuid.NewString(), categoryID)
	cleanupExec(t, `DELETE FROM regular_items WHERE item_id = $1`, id)
	return id
}

func newRegularUnit(t *testing.T) uuid.UUID {
	t.Helper()
	id := insertTestUnit(t, "repo-test-reg-unit-"+uuid.NewString(), "rr-"+uuid.NewString())
	cleanupExec(t, `DELETE FROM regular_items WHERE unit_id = $1`, id)
	return id
}

func newRegularCategory(t *testing.T, name string) uuid.UUID {
	t.Helper()
	return insertTestItemCategory(t, name+"-"+uuid.NewString(), "repo-test-reg-icon-"+uuid.NewString())
}

func TestRegularList_EmptyForNewUser(t *testing.T) {
	user := createTestUser(t)

	regulars, err := regularRepo.List(context.Background(), user.String())

	require.NoError(t, err)
	assert.NotNil(t, regulars, "an empty list must serialise as [], not null")
	assert.Empty(t, regulars)
}

func TestRegularList_HydratedAndOrderedByCategoryThenItem(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	catA := newRegularCategory(t, "repo-test-reg-a")
	catB := newRegularCategory(t, "repo-test-reg-b")
	unit := newRegularUnit(t)
	zebra := newRegularItem(t, catA, "repo-test-reg-zebra")
	apple := newRegularItem(t, catA, "repo-test-reg-apple")
	bread := newRegularItem(t, catB, "repo-test-reg-bread")

	for _, id := range []uuid.UUID{bread, zebra} {
		_, err := regularRepo.Create(ctx, user.String(), id.String(), nil, 1, testRegularsLimit)
		require.NoError(t, err)
	}
	_, err := regularRepo.Create(ctx, user.String(), apple.String(), strPtr(unit), 2.5, testRegularsLimit)
	require.NoError(t, err)

	regulars, err := regularRepo.List(ctx, user.String())
	require.NoError(t, err)
	require.Len(t, regulars, 3)

	assert.Equal(t, []uuid.UUID{apple, zebra, bread},
		[]uuid.UUID{regulars[0].ItemID, regulars[1].ItemID, regulars[2].ItemID})

	first := regulars[0]
	assert.Contains(t, first.ItemName, "repo-test-reg-apple")
	assert.Equal(t, catA, first.CategoryID)
	assert.Contains(t, first.CategoryName, "repo-test-reg-a")
	require.NotNil(t, first.UnitID)
	assert.Equal(t, unit, *first.UnitID)
	require.NotNil(t, first.UnitAbbreviation)
	assert.Contains(t, *first.UnitAbbreviation, "rr-")
	assert.Equal(t, 2.5, first.Quantity)
	assert.Nil(t, regulars[1].UnitID)
	assert.Nil(t, regulars[1].UnitAbbreviation)
}

func TestRegularList_OnlyCallersRegulars(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	other := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	_, err := regularRepo.Create(ctx, other.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)

	regulars, err := regularRepo.List(ctx, user.String())

	require.NoError(t, err)
	assert.Empty(t, regulars)
}

func TestRegularCreate_ReturnsHydratedRegular(t *testing.T) {
	user := createTestUser(t)
	cat := newRegularCategory(t, "repo-test-reg")
	item := newRegularItem(t, cat, "repo-test-reg-item")
	unit := newRegularUnit(t)

	got, err := regularRepo.Create(context.Background(), user.String(), item.String(), strPtr(unit), 9, testRegularsLimit)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.NotEqual(t, uuid.Nil, got.ID)
	assert.Equal(t, item, got.ItemID)
	assert.Equal(t, cat, got.CategoryID)
	require.NotNil(t, got.UnitID)
	assert.Equal(t, unit, *got.UnitID)
	assert.NotNil(t, got.UnitAbbreviation)
	assert.Equal(t, 9.0, got.Quantity)
}

func TestRegularCreate_DuplicateItemIsRejected(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	_, err := regularRepo.Create(ctx, user.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)

	_, err = regularRepo.Create(ctx, user.String(), item.String(), nil, 2, testRegularsLimit)

	assert.ErrorIs(t, err, models.ErrRegularExists)
}

func TestRegularCreate_AtLimitIsRejected(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	cat := newRegularCategory(t, "repo-test-reg")
	for i := 0; i < 2; i++ {
		_, err := regularRepo.Create(ctx, user.String(), newRegularItem(t, cat, "repo-test-reg-item").String(), nil, 1, 2)
		require.NoError(t, err)
	}

	_, err := regularRepo.Create(ctx, user.String(), newRegularItem(t, cat, "repo-test-reg-item").String(), nil, 1, 2)

	assert.ErrorIs(t, err, models.ErrRegularsLimitReached)
	assert.Equal(t, 2, rowCount(t, `SELECT count(*) FROM regular_items WHERE user_id = $1`, user))
}

func TestRegularCreate_LimitIsPerUser(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	other := createTestUser(t)
	cat := newRegularCategory(t, "repo-test-reg")
	_, err := regularRepo.Create(ctx, other.String(), newRegularItem(t, cat, "repo-test-reg-item").String(), nil, 1, 1)
	require.NoError(t, err)

	_, err = regularRepo.Create(ctx, user.String(), newRegularItem(t, cat, "repo-test-reg-item").String(), nil, 1, 1)

	assert.NoError(t, err)
}

func TestRegularCreate_UnknownItem(t *testing.T) {
	_, err := regularRepo.Create(context.Background(), createTestUser(t).String(), uuid.NewString(), nil, 1, testRegularsLimit)

	assert.ErrorIs(t, err, models.ErrRegularInvalidItem)
}

func TestRegularCreate_UnknownUnit(t *testing.T) {
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")

	_, err := regularRepo.Create(context.Background(), createTestUser(t).String(), item.String(), strPtr(uuid.New()), 1, testRegularsLimit)

	assert.ErrorIs(t, err, models.ErrRegularInvalidUnit)
}

func TestRegularCreate_UnitNotAllowedForItem(t *testing.T) {
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	insertTestItemAllowedUnit(t, item, newRegularUnit(t))

	_, err := regularRepo.Create(context.Background(), createTestUser(t).String(), item.String(), strPtr(newRegularUnit(t)), 1, testRegularsLimit)

	assert.ErrorIs(t, err, models.ErrIngredientUnitNotAllowed)
}

func TestRegularUpdate_ReplacesQuantityAndUnit(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	unit := newRegularUnit(t)
	created, err := regularRepo.Create(ctx, user.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)
	require.NotNil(t, created)

	got, err := regularRepo.Update(ctx, user.String(), created.ID.String(), strPtr(unit), 4)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, created.ID, got.ID)
	require.NotNil(t, got.UnitID)
	assert.Equal(t, unit, *got.UnitID)
	assert.Equal(t, 4.0, got.Quantity)
}

func TestRegularUpdate_NilUnitClearsIt(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	created, err := regularRepo.Create(ctx, user.String(), item.String(), strPtr(newRegularUnit(t)), 1, testRegularsLimit)
	require.NoError(t, err)
	require.NotNil(t, created)

	got, err := regularRepo.Update(ctx, user.String(), created.ID.String(), nil, 1)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Nil(t, got.UnitID)
	assert.Nil(t, got.UnitAbbreviation)
}

func TestRegularUpdate_UnitNotAllowedForItem(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	insertTestItemAllowedUnit(t, item, newRegularUnit(t))
	created, err := regularRepo.Create(ctx, user.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)
	require.NotNil(t, created)

	_, err = regularRepo.Update(ctx, user.String(), created.ID.String(), strPtr(newRegularUnit(t)), 1)

	assert.ErrorIs(t, err, models.ErrIngredientUnitNotAllowed)
}

func TestRegularUpdate_UnknownUnit(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	created, err := regularRepo.Create(ctx, user.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)
	require.NotNil(t, created)

	_, err = regularRepo.Update(ctx, user.String(), created.ID.String(), strPtr(uuid.New()), 1)

	assert.ErrorIs(t, err, models.ErrRegularInvalidUnit)
}

func TestRegularUpdate_AnotherUsersRegularIsNotFound(t *testing.T) {
	ctx := context.Background()
	owner := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	created, err := regularRepo.Create(ctx, owner.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)
	require.NotNil(t, created)

	_, err = regularRepo.Update(ctx, createTestUser(t).String(), created.ID.String(), nil, 7)

	assert.ErrorIs(t, err, models.ErrRegularNotFound)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM regular_items WHERE id = $1 AND quantity = 1`, created.ID))
}

func TestRegularUpdate_UnknownIDIsNotFound(t *testing.T) {
	_, err := regularRepo.Update(context.Background(), createTestUser(t).String(), uuid.NewString(), nil, 1)

	assert.ErrorIs(t, err, models.ErrRegularNotFound)
}

func TestRegularDelete_RemovesIt(t *testing.T) {
	ctx := context.Background()
	user := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	created, err := regularRepo.Create(ctx, user.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)
	require.NotNil(t, created)

	err = regularRepo.Delete(ctx, user.String(), created.ID.String())

	require.NoError(t, err)
	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM regular_items WHERE id = $1`, created.ID))
}

func TestRegularDelete_AnotherUsersRegularIsNotFound(t *testing.T) {
	ctx := context.Background()
	owner := createTestUser(t)
	item := newRegularItem(t, newRegularCategory(t, "repo-test-reg"), "repo-test-reg-item")
	created, err := regularRepo.Create(ctx, owner.String(), item.String(), nil, 1, testRegularsLimit)
	require.NoError(t, err)
	require.NotNil(t, created)

	err = regularRepo.Delete(ctx, createTestUser(t).String(), created.ID.String())

	assert.ErrorIs(t, err, models.ErrRegularNotFound)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM regular_items WHERE id = $1`, created.ID))
}
