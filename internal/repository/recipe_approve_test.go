package repository_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seenUpdatedAt returns the recipe's updatedAt as a client sends it back: read as an admin, then through JSON.
func seenUpdatedAt(t *testing.T, recipeID uuid.UUID) time.Time {
	t.Helper()
	detail, err := recipeRepo.GetByID(context.Background(), recipeID.String(), nil, true)
	require.NoError(t, err)
	body, err := json.Marshal(detail)
	require.NoError(t, err)
	var seen struct {
		UpdatedAt time.Time `json:"updatedAt"`
	}
	require.NoError(t, json.Unmarshal(body, &seen))
	return seen.UpdatedAt
}

// createPendingRecipe creates a pending recipe through the repo with a real ingredient and category, so the owner can Update it.
func createPendingRecipe(t *testing.T, owner uuid.UUID) (uuid.UUID, models.CreateRecipeInput) {
	t.Helper()
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	input := baseRecipeInput(owner, itemID, recipeCatID)
	created, err := recipeRepo.Create(context.Background(), input)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, created.ID)
	require.False(t, created.Approved)
	return created.ID, input
}

func TestApproveRecipe_PendingWithSeenUpdatedAtApprovesAndKeepsUpdatedAt(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	recipeID := insertTestRecipeRow(t, owner, false)
	seen := seenUpdatedAt(t, recipeID)

	detail, err := recipeRepo.Approve(context.Background(), recipeID.String(), admin.String(), seen)
	require.NoError(t, err)
	require.NotNil(t, detail)

	assert.Equal(t, recipeID, detail.ID)
	assert.True(t, detail.Approved)
	assert.True(t, seen.Equal(detail.UpdatedAt), "approval must not bump updatedAt: saw %s, got %s", seen, detail.UpdatedAt)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1 AND approved AND updated_at = $2`, recipeID, seen))
}

func TestApproveRecipe_OwnerEditedSinceSeen_Changed(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	recipeID, input := createPendingRecipe(t, owner)
	seen := seenUpdatedAt(t, recipeID)

	input.Name = "repo-test-recipe-edited-" + uuid.NewString()
	_, _, err := recipeRepo.Update(ctx, recipeID.String(), input, owner.String(), false)
	require.NoError(t, err)

	detail, err := recipeRepo.Approve(ctx, recipeID.String(), admin.String(), seen)
	assert.ErrorIs(t, err, models.ErrRecipeChanged)
	assert.Nil(t, detail)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1 AND NOT approved`, recipeID),
		"a recipe changed since the admin loaded it stays pending")
}

func TestApproveRecipe_AlreadyApprovedIsNoOpWhateverUpdatedAt(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	recipeID := insertTestRecipeRow(t, owner, true)
	stored := seenUpdatedAt(t, recipeID)

	detail, err := recipeRepo.Approve(context.Background(), recipeID.String(), admin.String(), stored.Add(-time.Hour))
	require.NoError(t, err)
	require.NotNil(t, detail)

	assert.Equal(t, recipeID, detail.ID)
	assert.True(t, detail.Approved)
	assert.True(t, stored.Equal(detail.UpdatedAt), "a no-op approve must not bump updatedAt: had %s, got %s", stored, detail.UpdatedAt)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1 AND approved AND updated_at = $2`, recipeID, stored))
}

func TestApproveRecipe_UnknownRecipe_NotFound(t *testing.T) {
	admin := insertTestUser(t, "Admin")

	detail, err := recipeRepo.Approve(context.Background(), uuid.NewString(), admin.String(), time.Now())
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
	assert.Nil(t, detail)
}

func TestApproveRecipe_WaitsForInFlightOwnerEditThenReportsChanged(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	recipeID, input := createPendingRecipe(t, owner)
	seen := seenUpdatedAt(t, recipeID)

	held := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	editDone := make(chan error, 1)
	go func() {
		editDone <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			input.Name = "repo-test-recipe-edited-" + uuid.NewString()
			if _, _, err := recipeRepo.Update(ctx, recipeID.String(), input, owner.String(), false); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-editDone:
		t.Fatalf("owner edit finished before holding its transaction open: %v", err)
	}

	approveDone := make(chan error, 1)
	go func() {
		approveDone <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			_, err := recipeRepo.Approve(ctx, recipeID.String(), admin.String(), seen)
			return err
		})
	}()

	waitForLockWait(t)
	releaseOnce(release)

	require.NoError(t, <-editDone)
	assert.ErrorIs(t, <-approveDone, models.ErrRecipeChanged,
		"an approve that waited on an owner's edit must see the edit, not approve the version it loaded")
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1 AND NOT approved`, recipeID))
}
