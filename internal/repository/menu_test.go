package repository_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func menuEntryRowCount(t *testing.T, userID, recipeID uuid.UUID) int {
	t.Helper()
	return rowCount(t, `
		SELECT count(*) FROM recipe_menu_entries rme
		JOIN recipe_menus rm ON rm.id = rme.recipe_menu_id
		WHERE rm.user_id = $1 AND rme.recipe_id = $2`, userID, recipeID)
}

func setMenuEntryCreatedAt(t *testing.T, userID, recipeID uuid.UUID, ts time.Time) {
	t.Helper()
	_, err := db.DB.Exec(context.Background(), `
		UPDATE recipe_menu_entries rme SET created_at = $1
		FROM recipe_menus rm
		WHERE rm.id = rme.recipe_menu_id AND rm.user_id = $2 AND rme.recipe_id = $3`,
		ts, userID, recipeID)
	require.NoError(t, err)
}

func menuEntryIDs(menu *models.Menu) []uuid.UUID {
	out := make([]uuid.UUID, len(menu.Entries))
	for i, e := range menu.Entries {
		out[i] = e.RecipeID
	}
	return out
}

func TestGetMenu_NoMenuReturnsEmptyEntries(t *testing.T) {
	caller := insertTestUser(t, "Caller")

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	assert.Empty(t, menu.Entries)
}

func TestUpsertEntry_FirstCallCreatesMenuAndEntry(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	err := menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 6, false)
	require.NoError(t, err)
	assert.Equal(t, 1, menuEntryRowCount(t, caller, recipeID))

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	require.Len(t, menu.Entries, 1)
	assert.Equal(t, recipeID, menu.Entries[0].RecipeID)
	assert.Equal(t, 6, menu.Entries[0].Serves)
}

func TestUpsertEntry_SecondCallUpdatesServesNotDuplicate(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 8, false))

	assert.Equal(t, 1, menuEntryRowCount(t, caller, recipeID), "second call must not duplicate the row")

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	require.Len(t, menu.Entries, 1)
	assert.Equal(t, 8, menu.Entries[0].Serves, "second call must update serves")
}

func TestUpsertEntry_SecondCallDoesNotResetCreatedAt(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false))
	original := time.Now().Add(-5 * time.Hour)
	setMenuEntryCreatedAt(t, caller, recipeID, original)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 8, false))

	var createdAt time.Time
	require.NoError(t, db.DB.QueryRow(context.Background(), `
		SELECT rme.created_at FROM recipe_menu_entries rme
		JOIN recipe_menus rm ON rm.id = rme.recipe_menu_id
		WHERE rm.user_id = $1 AND rme.recipe_id = $2`, caller, recipeID).Scan(&createdAt))
	assert.WithinDuration(t, original, createdAt, time.Second, "updating serves must not reshuffle position")
}

func TestUpsertEntry_HiddenRecipeReturnsNotFound(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, false) // unapproved, caller isn't the owner

	err := menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 4, false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
	assert.Equal(t, 0, menuEntryRowCount(t, caller, recipeID))
}

func TestUpsertEntry_ConcurrentCallsNeverDuplicateRow(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), i+1, false)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, menuEntryRowCount(t, caller, recipeID), "concurrent upserts must never produce two rows")
}

func TestUpdateEntryServes_UpdatesExistingEntry(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false))

	err := menuRepo.UpdateEntryServes(context.Background(), caller.String(), recipeID.String(), 10)
	require.NoError(t, err)

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	require.Len(t, menu.Entries, 1)
	assert.Equal(t, 10, menu.Entries[0].Serves)
}

func TestUpdateEntryServes_NotFoundWhenRecipeNotOnMenu(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	onMenu := insertTestRecipeRow(t, owner, true)
	notOnMenu := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), onMenu.String(), 2, false))

	err := menuRepo.UpdateEntryServes(context.Background(), caller.String(), notOnMenu.String(), 10)
	assert.ErrorIs(t, err, models.ErrMenuEntryNotFound)
}

func TestUpdateEntryServes_NotFoundWhenNoMenuAtAll(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	err := menuRepo.UpdateEntryServes(context.Background(), caller.String(), recipeID.String(), 10)
	assert.ErrorIs(t, err, models.ErrMenuEntryNotFound)
}

func TestRemoveEntry_DeletesExistingEntry(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false))

	err := menuRepo.RemoveEntry(context.Background(), caller.String(), recipeID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, menuEntryRowCount(t, caller, recipeID))
}

func TestRemoveEntry_IdempotentWhenRecipeNotOnMenu(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	onMenu := insertTestRecipeRow(t, owner, true)
	notOnMenu := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), onMenu.String(), 2, false))

	err := menuRepo.RemoveEntry(context.Background(), caller.String(), notOnMenu.String())
	assert.NoError(t, err)
}

func TestRemoveEntry_IdempotentWhenNoMenuAtAll(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	err := menuRepo.RemoveEntry(context.Background(), caller.String(), recipeID.String())
	assert.NoError(t, err)
}

func TestGetMenu_OrderedByCreatedAtDescending(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	a := insertTestRecipeRow(t, owner, true)
	b := insertTestRecipeRow(t, owner, true)
	c := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), a.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), b.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), c.String(), 2, false))

	now := time.Now()
	setMenuEntryCreatedAt(t, caller, a, now.Add(-1*time.Hour))
	setMenuEntryCreatedAt(t, caller, b, now.Add(-3*time.Hour))
	setMenuEntryCreatedAt(t, caller, c, now.Add(-2*time.Hour))

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a, c, b}, menuEntryIDs(menu), "most recently added first")
}

func TestGetMenu_EntriesHydratedWithRecipeCardAndCategories(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
	recipeID := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 3, false))

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	require.Len(t, menu.Entries, 1)
	entry := menu.Entries[0]
	require.NotNil(t, entry.Recipe)
	assert.Equal(t, recipeID, entry.Recipe.ID)
	assert.Equal(t, 3, entry.Serves, "entry serves, not the recipe's own default serves")
	require.Len(t, entry.Recipe.Categories, 1)
	assert.Equal(t, cat, entry.Recipe.Categories[0].ID)
}
