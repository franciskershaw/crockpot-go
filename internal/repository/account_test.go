package repository_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recipeExists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var n int
	require.NoError(t, db.DB.QueryRow(context.Background(), `SELECT count(*) FROM recipes WHERE id = $1`, id).Scan(&n))
	return n > 0
}

func setRecipeImage(t *testing.T, id uuid.UUID, publicID string) {
	t.Helper()
	_, err := db.DB.Exec(context.Background(), `UPDATE recipes SET image_url = 'https://example.com/x.jpg', image_filename = $2 WHERE id = $1`, id, publicID)
	require.NoError(t, err)
}

func countUserRows(t *testing.T, table string, userID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.DB.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE user_id = $1`, userID).Scan(&n))
	return n
}

func deleteUnapproved(t *testing.T, creator uuid.UUID) (holders, orphans []string) {
	t.Helper()
	err := transactor.WithinTx(context.Background(), func(ctx context.Context) error {
		var err error
		holders, orphans, err = recipeRepo.DeleteUnapprovedByCreator(ctx, creator.String())
		return err
	})
	require.NoError(t, err)
	return holders, orphans
}

func TestDeleteAccount_DeletesOnlyTheCreatorsUnapprovedRecipes(t *testing.T) {
	owner := insertTestUser(t, "Leaving")
	other := insertTestUser(t, "Staying")
	draft := insertTestRecipeRow(t, owner, false)
	published := insertTestRecipeRow(t, owner, true)
	othersDraft := insertTestRecipeRow(t, other, false)

	deleteUnapproved(t, owner)

	assert.False(t, recipeExists(t, draft), "the leaving user's draft is deleted")
	assert.True(t, recipeExists(t, published), "an approved recipe belongs to the community and stays")
	assert.True(t, recipeExists(t, othersDraft))
}

func TestDeleteAccount_ReturnsEachMenuHolderOnce(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Leaving")
	admin := insertTestUser(t, "Admin Holder")
	draftA := insertTestRecipeRow(t, owner, false)
	draftB := insertTestRecipeRow(t, owner, false)
	require.NoError(t, menuRepo.UpsertEntry(ctx, admin.String(), draftA.String(), 2, true, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(ctx, admin.String(), draftB.String(), 2, true, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(ctx, owner.String(), draftA.String(), 2, false, noMenuLimit))

	holders, _ := deleteUnapproved(t, owner)

	want := []string{admin.String(), owner.String()}
	sort.Strings(want)
	assert.Equal(t, want, holders)
}

func TestDeleteAccount_ReportsOnlyPhotosNoRecipeStillUses(t *testing.T) {
	owner := insertTestUser(t, "Leaving")
	other := insertTestUser(t, "Staying")
	soloImg := "repo-test-solo-" + uuid.NewString()
	sharedImg := "repo-test-shared-" + uuid.NewString()
	twinImg := "repo-test-twin-" + uuid.NewString()

	setRecipeImage(t, insertTestRecipeRow(t, owner, false), soloImg)
	setRecipeImage(t, insertTestRecipeRow(t, owner, false), sharedImg)
	setRecipeImage(t, insertTestRecipeRow(t, other, true), sharedImg)
	setRecipeImage(t, insertTestRecipeRow(t, owner, false), twinImg)
	setRecipeImage(t, insertTestRecipeRow(t, owner, false), twinImg)

	_, orphans := deleteUnapproved(t, owner)

	assert.ElementsMatch(t, []string{soloImg, twinImg}, orphans,
		"a photo another recipe still uses is not orphaned; one shared by two deleted drafts is reported once")
}

func TestDeleteAccount_FullDeleteCascadesAndKeepsCommunityRecipes(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Leaving")
	admin := insertTestUser(t, "Admin Holder")
	fan := insertTestUser(t, "Fan")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	draftItem := insertTestItem(t, "repo-test-draft-item-"+uuid.NewString(), catID)
	keptItem := insertTestItem(t, "repo-test-kept-item-"+uuid.NewString(), catID)
	draft := createTestRecipe(t, recipeOpts{createdBy: owner, ingredients: []models.Ingredient{{ItemID: draftItem, Quantity: 1}}})
	published := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, ingredients: []models.Ingredient{{ItemID: keptItem, Quantity: 1}}})

	_, err := refreshTokenRepo.CreateFamily(ctx, uuid.NewString(), owner.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	_, err = emailVerificationTokenRepo.Create(ctx, owner.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	_, err = passwordResetTokenRepo.Create(ctx, owner.String(), "repo-test-hash-"+uuid.NewString(), time.Now().Add(time.Hour))
	require.NoError(t, err)
	_, err = db.DB.Exec(ctx, `INSERT INTO recipe_favourites (user_id, recipe_id) VALUES ($1, $2), ($3, $2)`, owner, published, fan)
	require.NoError(t, err)
	require.NoError(t, menuRepo.UpsertEntry(ctx, owner.String(), published.String(), 2, false, noMenuLimit))
	require.NoError(t, shoppingListRepo.Regenerate(ctx, owner.String()))
	_, err = regularRepo.Create(ctx, owner.String(), keptItem.String(), nil, 1, 100)
	require.NoError(t, err)
	require.NoError(t, menuRepo.UpsertEntry(ctx, admin.String(), draft.String(), 2, true, noMenuLimit))
	require.NoError(t, menuRepo.UpsertEntry(ctx, admin.String(), published.String(), 2, true, noMenuLimit))
	require.NoError(t, shoppingListRepo.Regenerate(ctx, admin.String()))
	require.NoError(t, menuRepo.UpsertEntry(ctx, fan.String(), published.String(), 2, false, noMenuLimit))
	// Items are RESTRICT from lists and regulars, so those rows must go before the item cleanups run.
	cleanupExec(t, `DELETE FROM shopping_lists WHERE user_id IN ($1, $2)`, owner, admin)
	cleanupExec(t, `DELETE FROM regular_items WHERE user_id = $1`, owner)

	err = transactor.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := userRepo.FindByIDForUpdate(ctx, owner.String()); err != nil {
			return err
		}
		holders, _, err := recipeRepo.DeleteUnapprovedByCreator(ctx, owner.String())
		if err != nil {
			return err
		}
		for _, h := range holders {
			if h == owner.String() {
				continue
			}
			if err := shoppingListRepo.Regenerate(ctx, h); err != nil {
				return err
			}
		}
		return userRepo.Delete(ctx, owner.String())
	})
	require.NoError(t, err)

	var users int
	require.NoError(t, db.DB.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, owner).Scan(&users))
	assert.Zero(t, users, "the users row is gone")
	for _, table := range []string{"refresh_tokens", "email_verification_tokens", "password_reset_tokens", "recipe_favourites", "recipe_menus", "shopping_lists", "regular_items"} {
		assert.Zero(t, countUserRows(t, table, owner), "%s still has rows for the deleted user", table)
	}

	assert.False(t, recipeExists(t, draft))
	got, err := recipeRepo.GetByID(ctx, published.String(), nil, false)
	require.NoError(t, err)
	assert.Nil(t, got.CreatedByID, "the community recipe stays with no creator")
	assert.Nil(t, got.CreatedByName)

	var adminDraftItems, adminKeptItems int
	require.NoError(t, db.DB.QueryRow(ctx, `SELECT count(*) FILTER (WHERE sli.item_id = $2), count(*) FILTER (WHERE sli.item_id = $3)
		FROM shopping_list_items sli JOIN shopping_lists sl ON sl.id = sli.shopping_list_id WHERE sl.user_id = $1`,
		admin, draftItem, keptItem).Scan(&adminDraftItems, &adminKeptItems))
	assert.Zero(t, adminDraftItems, "the admin's list is rebuilt without the deleted draft")
	assert.Equal(t, 1, adminKeptItems)

	assert.Equal(t, 1, countUserRows(t, "recipe_favourites", fan), "other users' favourites of the community recipe are untouched")
	var fanEntries int
	require.NoError(t, db.DB.QueryRow(ctx, `SELECT count(*) FROM recipe_menu_entries rme JOIN recipe_menus rm ON rm.id = rme.recipe_menu_id WHERE rm.user_id = $1 AND rme.recipe_id = $2`, fan, published).Scan(&fanEntries))
	assert.Equal(t, 1, fanEntries, "other users' menu entries for the community recipe are untouched")
}

func TestDeleteAccount_RecipeCreateWaitingOnTheLockFailsAfterCommit(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Leaving")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	input := baseRecipeInput(owner, itemID, recipeCatID)
	input.Name = "repo-test-late-recipe-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = db.DB.Exec(context.Background(), `DELETE FROM recipes WHERE name = $1`, input.Name)
	})

	locked := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	deleteErr := make(chan error, 1)
	go func() {
		deleteErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			if _, err := userRepo.FindByIDForUpdate(ctx, owner.String()); err != nil {
				return err
			}
			close(locked)
			<-release
			if _, _, err := recipeRepo.DeleteUnapprovedByCreator(ctx, owner.String()); err != nil {
				return err
			}
			return userRepo.Delete(ctx, owner.String())
		})
	}()
	select {
	case <-locked:
	case err := <-deleteErr:
		t.Fatalf("delete failed before holding the user lock: %v", err)
	}

	createErr := make(chan error, 1)
	go func() {
		createErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			_, err := recipeRepo.Create(ctx, input)
			return err
		})
	}()

	waitForLockWait(t)
	releaseOnce(release)

	require.NoError(t, <-deleteErr)
	assert.Error(t, <-createErr, "a recipe insert that waited on the deleted user must fail its foreign-key check")
	var n int
	require.NoError(t, db.DB.QueryRow(ctx, `SELECT count(*) FROM recipes WHERE name = $1`, input.Name).Scan(&n))
	assert.Zero(t, n, "no recipe is left behind for the deleted user")
}

func TestDeleteUser_ReturnsErrUserNotFound(t *testing.T) {
	err := userRepo.Delete(context.Background(), uuid.NewString())
	assert.ErrorIs(t, err, models.ErrUserNotFound)
}
