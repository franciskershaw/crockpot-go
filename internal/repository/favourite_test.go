package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func favouriteRowCount(t *testing.T, userID, recipeID uuid.UUID) int {
	t.Helper()
	return rowCount(t, `SELECT count(*) FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2`, userID, recipeID)
}

func setFavouritedAt(t *testing.T, userID, recipeID uuid.UUID, ts time.Time) {
	t.Helper()
	_, err := db.DB.Exec(context.Background(),
		`UPDATE recipe_favourites SET created_at = $1 WHERE user_id = $2 AND recipe_id = $3`, ts, userID, recipeID)
	require.NoError(t, err)
}

func TestAddFavourite_FirstCallInsertsRow(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	cleanupExec(t, `DELETE FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2`, caller, recipeID)

	err := recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, favouriteRowCount(t, caller, recipeID))
}

func TestAddFavourite_IdempotentOnSecondCall(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	cleanupExec(t, `DELETE FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2`, caller, recipeID)

	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false))
	err := recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, favouriteRowCount(t, caller, recipeID), "second call must not duplicate the row")
}

func TestAddFavourite_HiddenRecipeReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, false) // unapproved, caller isn't the owner

	err := recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
	assert.Equal(t, 0, favouriteRowCount(t, caller, recipeID), "no row inserted for a hidden recipe")
}

func TestAddFavourite_OwnerCanFavouriteOwnUnapprovedRecipe(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	recipeID := insertTestRecipeRow(t, owner, false)
	cleanupExec(t, `DELETE FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2`, owner, recipeID)

	err := recipeRepo.AddFavourite(ctx, owner.String(), recipeID.String(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, favouriteRowCount(t, owner, recipeID))
}

func TestAddFavourite_AdminCanFavouriteHiddenRecipe(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	recipeID := insertTestRecipeRow(t, owner, false)
	cleanupExec(t, `DELETE FROM recipe_favourites WHERE user_id = $1 AND recipe_id = $2`, admin, recipeID)

	err := recipeRepo.AddFavourite(ctx, admin.String(), recipeID.String(), true)
	require.NoError(t, err)
	assert.Equal(t, 1, favouriteRowCount(t, admin, recipeID))
}

func TestAddFavourite_NonexistentRecipeReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	caller := insertTestUser(t, "Caller")

	err := recipeRepo.AddFavourite(ctx, caller.String(), uuid.NewString(), false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
}

func TestRemoveFavourite_DeletesExistingFavourite(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false))

	err := recipeRepo.RemoveFavourite(ctx, caller.String(), recipeID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, favouriteRowCount(t, caller, recipeID))
}

func TestRemoveFavourite_IdempotentWhenNotFavourited(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)

	err := recipeRepo.RemoveFavourite(ctx, caller.String(), recipeID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, favouriteRowCount(t, caller, recipeID))
}

// RemoveFavourite never re-checks visibility, so removing a favourite on a hidden recipe succeeds rather than 404ing.
func TestRemoveFavourite_NoVisibilityCheckOnHiddenRecipe(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, false)

	err := recipeRepo.RemoveFavourite(ctx, caller.String(), recipeID.String())
	assert.NoError(t, err)
}

func TestFavouriteCascadesOnRecipeDelete(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false))
	require.Equal(t, 1, favouriteRowCount(t, caller, recipeID))

	_, err := db.DB.Exec(ctx, `DELETE FROM recipes WHERE id = $1`, recipeID)
	require.NoError(t, err)

	assert.Equal(t, 0, favouriteRowCount(t, caller, recipeID), "ON DELETE CASCADE should remove the favourite row")
}

func TestListFavourites_ReturnsOnlyCallersFavourites(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	other := insertTestUser(t, "Other")
	mine := insertTestRecipeRow(t, owner, true)
	notMine := insertTestRecipeRow(t, owner, true)

	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), mine.String(), false))
	require.NoError(t, recipeRepo.AddFavourite(ctx, other.String(), notMine.String(), false))

	cards, total, err := recipeRepo.ListFavourites(ctx, caller.String(), 1, 50)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, []uuid.UUID{mine}, cardIDs(cards))
}

func TestListFavourites_EmptyForUserWithNone(t *testing.T) {
	ctx := context.Background()
	caller := insertTestUser(t, "Caller")

	cards, total, err := recipeRepo.ListFavourites(ctx, caller.String(), 1, 50)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, cards)
}

func TestListFavourites_OrderedByFavouritedAtDescending(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	a := insertTestRecipeRow(t, owner, true)
	b := insertTestRecipeRow(t, owner, true)
	c := insertTestRecipeRow(t, owner, true)

	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), a.String(), false))
	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), b.String(), false))
	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), c.String(), false))

	now := time.Now()
	setFavouritedAt(t, caller, a, now.Add(-1*time.Hour))
	setFavouritedAt(t, caller, b, now.Add(-3*time.Hour))
	setFavouritedAt(t, caller, c, now.Add(-2*time.Hour))

	cards, _, err := recipeRepo.ListFavourites(ctx, caller.String(), 1, 50)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a, c, b}, cardIDs(cards), "most recently favourited first")
}

func TestListFavourites_Pagination(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")

	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = insertTestRecipeRow(t, owner, true)
		require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), ids[i].String(), false))
		setFavouritedAt(t, caller, ids[i], time.Now().Add(time.Duration(-i)*time.Hour))
	}

	p1, total, err := recipeRepo.ListFavourites(ctx, caller.String(), 1, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Equal(t, []uuid.UUID{ids[0], ids[1]}, cardIDs(p1))

	p3, total, err := recipeRepo.ListFavourites(ctx, caller.String(), 3, 2)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Equal(t, []uuid.UUID{ids[4]}, cardIDs(p3))
}

func TestListFavourites_CardsHaveIsFavouriteTrueAndCategoriesHydrated(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
	recipeID := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})

	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false))

	cards, _, err := recipeRepo.ListFavourites(ctx, caller.String(), 1, 50)
	require.NoError(t, err)
	require.Len(t, cards, 1)
	assert.True(t, cards[0].IsFavourite)
	require.Len(t, cards[0].Categories, 1)
	assert.Equal(t, cat, cards[0].Categories[0].ID)
}

func TestListFavourites_TotalMatchesLength(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	for i := 0; i < 3; i++ {
		id := insertTestRecipeRow(t, owner, true)
		require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), id.String(), false))
	}

	cards, total, err := recipeRepo.ListFavourites(ctx, caller.String(), 1, 100000)
	require.NoError(t, err)
	assert.Equal(t, total, len(cards))
}
