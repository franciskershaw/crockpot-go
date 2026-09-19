package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// menuHistoryEvents returns the event types recorded for a user's recipe, oldest first.
func menuHistoryEvents(t *testing.T, userID, recipeID uuid.UUID) []string {
	t.Helper()
	rows, err := db.DB.Query(context.Background(), `
		SELECT e.event_type
		FROM menu_history_events e
		JOIN recipe_menus rm ON rm.id = e.recipe_menu_id
		WHERE rm.user_id = $1 AND e.recipe_id = $2
		ORDER BY e.occurred_at, e.id`, userID, recipeID)
	require.NoError(t, err)
	defer rows.Close()

	types := []string{}
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		types = append(types, s)
	}
	require.NoError(t, rows.Err())
	return types
}

func menuHistoryRemoveTimes(t *testing.T, userID uuid.UUID) []time.Time {
	t.Helper()
	rows, err := db.DB.Query(context.Background(), `
		SELECT e.occurred_at
		FROM menu_history_events e
		JOIN recipe_menus rm ON rm.id = e.recipe_menu_id
		WHERE rm.user_id = $1 AND e.event_type = 'remove'`, userID)
	require.NoError(t, err)
	defer rows.Close()

	var times []time.Time
	for rows.Next() {
		var ts time.Time
		require.NoError(t, rows.Scan(&ts))
		times = append(times, ts)
	}
	require.NoError(t, rows.Err())
	return times
}

func TestUpsertEntry_NewEntryWritesAddEvent(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 4, false))

	assert.Equal(t, []string{"add"}, menuHistoryEvents(t, caller, recipeID))
}

func TestUpsertEntry_ServesOnlyChangeWritesNoEvent(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 8, false))

	assert.Equal(t, []string{"add"}, menuHistoryEvents(t, caller, recipeID), "re-posting a recipe already on the menu is not an add")
}

func TestUpdateEntryServes_WritesNoEvent(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpdateEntryServes(context.Background(), caller.String(), recipeID.String(), 6))

	assert.Equal(t, []string{"add"}, menuHistoryEvents(t, caller, recipeID))
}

func TestUpsertEntry_ReAddingAfterRemoveWritesSecondAdd(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	ctx := context.Background()

	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), recipeID.String(), 4, false))
	require.NoError(t, menuRepo.RemoveEntry(ctx, caller.String(), recipeID.String()))
	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), recipeID.String(), 4, false))

	assert.Equal(t, []string{"add", "remove", "add"}, menuHistoryEvents(t, caller, recipeID))
}

func TestRemoveEntry_WritesRemoveEventWhenOnMenu(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	ctx := context.Background()

	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), recipeID.String(), 4, false))
	require.NoError(t, menuRepo.RemoveEntry(ctx, caller.String(), recipeID.String()))

	assert.Equal(t, []string{"add", "remove"}, menuHistoryEvents(t, caller, recipeID))
}

// The three no-op cases below pass before the hook exists; they guard against an implementation that records blindly.
func TestRemoveEntry_WritesNoEventWhenRecipeNotOnMenu(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	onMenu := insertTestRecipeRow(t, owner, true)
	notOnMenu := insertTestRecipeRow(t, owner, true)
	ctx := context.Background()
	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), onMenu.String(), 2, false))

	require.NoError(t, menuRepo.RemoveEntry(ctx, caller.String(), notOnMenu.String()))

	assert.Empty(t, menuHistoryEvents(t, caller, notOnMenu))
}

func TestRemoveEntry_WritesNoEventWhenNoMenuAtAll(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	require.NoError(t, menuRepo.RemoveEntry(context.Background(), caller.String(), recipeID.String()))

	assert.Empty(t, menuHistoryEvents(t, caller, recipeID))
}

func TestClearMenu_WritesNoEventWhenNoMenu(t *testing.T) {
	caller := insertTestUser(t, "Caller")

	require.NoError(t, menuRepo.ClearMenu(context.Background(), caller.String()))

	assert.Empty(t, menuHistoryRemoveTimes(t, caller))
}

func TestClearMenu_WritesOneRemoveEventPerRecipeAtOneTimestamp(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	a := insertTestRecipeRow(t, owner, true)
	b := insertTestRecipeRow(t, owner, true)
	ctx := context.Background()

	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), a.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), b.String(), 2, false))
	require.NoError(t, menuRepo.ClearMenu(ctx, caller.String()))

	assert.Equal(t, []string{"add", "remove"}, menuHistoryEvents(t, caller, a))
	assert.Equal(t, []string{"add", "remove"}, menuHistoryEvents(t, caller, b))

	removes := menuHistoryRemoveTimes(t, caller)
	require.Len(t, removes, 2)
	assert.True(t, removes[0].Equal(removes[1]), "one clear-menu is one moment: %v vs %v", removes[0], removes[1])
}

func TestClearMenu_RecordsOnlyRecipesActuallyOnTheMenu(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	onMenu := insertTestRecipeRow(t, owner, true)
	removedEarlier := insertTestRecipeRow(t, owner, true)
	ctx := context.Background()

	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), onMenu.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(ctx, caller.String(), removedEarlier.String(), 2, false))
	require.NoError(t, menuRepo.RemoveEntry(ctx, caller.String(), removedEarlier.String()))
	require.NoError(t, menuRepo.ClearMenu(ctx, caller.String()))

	assert.Equal(t, []string{"add", "remove"}, menuHistoryEvents(t, caller, onMenu))
	assert.Equal(t, []string{"add", "remove"}, menuHistoryEvents(t, caller, removedEarlier), "clear must not add a second remove for a recipe already off the menu")
}

func TestClearMenu_OnlyRecordsCallingUsersHistory(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	clearedCaller := insertTestUser(t, "Cleared Caller")
	otherCaller := insertTestUser(t, "Other Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	ctx := context.Background()

	require.NoError(t, menuRepo.UpsertEntry(ctx, clearedCaller.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(ctx, otherCaller.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.ClearMenu(ctx, clearedCaller.String()))

	assert.Equal(t, []string{"add", "remove"}, menuHistoryEvents(t, clearedCaller, recipeID))
	assert.Equal(t, []string{"add"}, menuHistoryEvents(t, otherCaller, recipeID), "another user's history must be untouched")
}

// History is written through the caller's transaction, so it commits or rolls back with the menu write.
func TestMenuHistory_JoinsCallersTransaction(t *testing.T) {
	owner := insertTestUser(t, "Owner")

	t.Run("commit keeps the event", func(t *testing.T) {
		caller := insertTestUser(t, "Caller")
		recipeID := insertTestRecipeRow(t, owner, true)

		err := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
			return menuRepo.UpsertEntry(ctx, caller.String(), recipeID.String(), 4, false)
		})
		require.NoError(t, err)

		assert.Equal(t, []string{"add"}, menuHistoryEvents(t, caller, recipeID))
	})

	// Passes before the hook exists; guards against a history write that escapes the transaction.
	t.Run("rollback discards the event with the entry", func(t *testing.T) {
		caller := insertTestUser(t, "Caller")
		recipeID := insertTestRecipeRow(t, owner, true)

		err := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
			if err := menuRepo.UpsertEntry(ctx, caller.String(), recipeID.String(), 4, false); err != nil {
				return err
			}
			return errors.New("force rollback")
		})
		require.Error(t, err)

		assert.Equal(t, 0, menuEntryRowCount(t, caller, recipeID))
		assert.Empty(t, menuHistoryEvents(t, caller, recipeID))
	})
}

func TestUpsertEntry_ConcurrentCallsWriteExactlyOneAdd(t *testing.T) {
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
	assert.Equal(t, []string{"add"}, menuHistoryEvents(t, caller, recipeID), "only the request that created the entry may record an add")
}

func TestRemoveEntry_ConcurrentCallsWriteExactlyOneRemove(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(context.Background(), caller.String(), recipeID.String(), 4, false))

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			errs[i] = menuRepo.RemoveEntry(context.Background(), caller.String(), recipeID.String())
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"add", "remove"}, menuHistoryEvents(t, caller, recipeID), "only the request that deleted the entry may record a remove")
}
