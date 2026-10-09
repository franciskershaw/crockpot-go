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

// noMenuLimit keeps the menu cap out of tests that aren't about it.
const noMenuLimit = 1000

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

func TestGetMenu_CardsCarryCallersFavouriteState(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	other := insertTestUser(t, "Other")
	favourited := insertTestRecipeRow(t, owner, true)
	notFavourited := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), favourited.String(), 4, false, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), notFavourited.String(), 4, false, noMenuLimit))
	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), favourited.String(), false))
	require.NoError(t, recipeRepo.AddFavourite(ctx, other.String(), notFavourited.String(), false))

	menu, err := menuRepo.GetMenu(ctx, caller.String())
	require.NoError(t, err)

	got := make(map[uuid.UUID]bool, len(menu.Entries))
	for _, e := range menu.Entries {
		got[e.RecipeID] = e.Recipe.IsFavourite
	}
	assert.Equal(t, map[uuid.UUID]bool{favourited: true, notFavourited: false}, got)
}

func TestUpsertEntry_FirstCallCreatesMenuAndEntry(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	err := menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 6, false, noMenuLimit)
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

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 8, false, noMenuLimit))

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

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false, noMenuLimit))
	original := time.Now().Add(-5 * time.Hour)
	setMenuEntryCreatedAt(t, caller, recipeID, original)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 8, false, noMenuLimit))

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

	err := menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 4, false, noMenuLimit)
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
			errs[i] = menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), i+1, false, noMenuLimit)
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, 1, menuEntryRowCount(t, caller, recipeID), "concurrent upserts must never produce two rows")
}

// fillMenu puts n fresh approved recipes on userID's menu and returns them in insert order.
func fillMenu(t *testing.T, userID, ownerID uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = insertTestRecipeRow(t, ownerID, true)
		require.NoError(t, menuRepo.UpsertEntry(context.Background(), userID.String(), ids[i].String(), 2, false, noMenuLimit))
	}
	return ids
}

func menuSize(t *testing.T, userID uuid.UUID) int {
	t.Helper()
	return rowCount(t, `
		SELECT count(*) FROM recipe_menu_entries rme
		JOIN recipe_menus rm ON rm.id = rme.recipe_menu_id
		WHERE rm.user_id = $1`, userID)
}

func TestUpsertEntry_NewRecipeOnFullMenuReturnsLimitReached(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	fillMenu(t, caller, owner, 3)
	extra := insertTestRecipeRow(t, owner, true)

	err := menuRepo.UpsertEntry(context.Background(), caller.String(), extra.String(), 2, false, 3)
	assert.ErrorIs(t, err, models.ErrMenuLimitReached)
	assert.Equal(t, 0, menuEntryRowCount(t, caller, extra))
	assert.Empty(t, menuHistoryEvents(t, caller, extra), "a refused add must not record a history event")
}

func TestUpsertEntry_ServesChangeOnFullMenuSucceeds(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	ids := fillMenu(t, caller, owner, 3)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), ids[0].String(), 9, false, 3))

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	serves := make(map[uuid.UUID]int, len(menu.Entries))
	for _, e := range menu.Entries {
		serves[e.RecipeID] = e.Serves
	}
	assert.Equal(t, 9, serves[ids[0]], "a serves change on a full menu must still apply")
}

func TestUpsertEntry_EntryReachingLimitSucceeds(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	fillMenu(t, caller, owner, 2)
	last := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), last.String(), 2, false, 3))
	assert.Equal(t, 3, menuSize(t, caller))
}

func TestUpsertEntry_HiddenRecipeOnFullMenuReturnsNotFound(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	fillMenu(t, caller, owner, 3)
	hidden := insertTestRecipeRow(t, owner, false)

	err := menuRepo.UpsertEntry(context.Background(), caller.String(), hidden.String(), 2, false, 3)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
}

func TestUpsertEntry_ConcurrentAddsAtLimitAdmitExactlyOne(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	fillMenu(t, caller, owner, 1)
	first := insertTestRecipeRow(t, owner, true)
	second := insertTestRecipeRow(t, owner, true)

	added := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			if err := menuRepo.UpsertEntry(ctx, caller.String(), first.String(), 2, false, 2); err != nil {
				return err
			}
			close(added)
			<-release
			return nil
		})
	}()
	<-added

	secondErr := make(chan error, 1)
	go func() {
		secondErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			return menuRepo.UpsertEntry(ctx, caller.String(), second.String(), 2, false, 2)
		})
	}()

	waitForLockWait(t)
	releaseOnce(release)

	require.NoError(t, <-firstErr)
	assert.ErrorIs(t, <-secondErr, models.ErrMenuLimitReached, "the add that waited must see the committed entry and be refused")
	assert.Equal(t, 2, menuSize(t, caller))
}

func TestUpdateEntryServes_UpdatesExistingEntry(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false, noMenuLimit))

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
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), onMenu.String(), 2, false, noMenuLimit))

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
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false, noMenuLimit))

	err := menuRepo.RemoveEntry(context.Background(), caller.String(), recipeID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, menuEntryRowCount(t, caller, recipeID))
}

func TestRemoveEntry_IdempotentWhenRecipeNotOnMenu(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	onMenu := insertTestRecipeRow(t, owner, true)
	notOnMenu := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), onMenu.String(), 2, false, noMenuLimit))

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

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), a.String(), 2, false, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), b.String(), 2, false, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), c.String(), 2, false, noMenuLimit))

	now := time.Now()
	setMenuEntryCreatedAt(t, caller, a, now.Add(-1*time.Hour))
	setMenuEntryCreatedAt(t, caller, b, now.Add(-3*time.Hour))
	setMenuEntryCreatedAt(t, caller, c, now.Add(-2*time.Hour))

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a, c, b}, menuEntryIDs(menu), "most recently added first")
}

func TestClearMenu_RemovesAllEntries(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	a := insertTestRecipeRow(t, owner, true)
	b := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), a.String(), 2, false, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), b.String(), 2, false, noMenuLimit))

	err := menuRepo.ClearMenu(context.Background(), caller.String())
	require.NoError(t, err)

	menu, err := menuRepo.GetMenu(context.Background(), caller.String())
	require.NoError(t, err)
	assert.Empty(t, menu.Entries)
}

func TestClearMenu_NoExistingMenu_NoOp(t *testing.T) {
	caller := insertTestUser(t, "Caller")

	err := menuRepo.ClearMenu(context.Background(), caller.String())
	require.NoError(t, err)

	var count int
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT count(*) FROM recipe_menus WHERE user_id = $1`, caller).Scan(&count))
	assert.Equal(t, 0, count, "clearing a menu that never existed must not create one")
}

func TestClearMenu_OnlyClearsCallingUsersMenu(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	clearedCaller := insertTestUser(t, "Cleared Caller")
	otherCaller := insertTestUser(t, "Other Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), clearedCaller.String(), recipeID.String(), 2, false, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), otherCaller.String(), recipeID.String(), 2, false, noMenuLimit))

	require.NoError(t, menuRepo.ClearMenu(context.Background(), clearedCaller.String()))

	clearedMenu, err := menuRepo.GetMenu(context.Background(), clearedCaller.String())
	require.NoError(t, err)
	assert.Empty(t, clearedMenu.Entries)

	otherMenu, err := menuRepo.GetMenu(context.Background(), otherCaller.String())
	require.NoError(t, err)
	assert.Len(t, otherMenu.Entries, 1, "another user's menu must be untouched")
}

func TestGetMenu_EntriesHydratedWithRecipeCardAndCategories(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
	recipeID := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 3, false, noMenuLimit))

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
