package repository_test

import (
	"context"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertTestRecipeCategory(t *testing.T, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO recipe_categories (id, name) VALUES ($1, $2)`,
		id, name,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipe_categories WHERE id = $1`, id)
	return id
}

func TestCreateRecipeCategory_CreatesNewCategory(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-recipe-category-" + uuid.NewString()

	category, err := recipeCategoryRepo.Create(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, category)
	cleanupExec(t, `DELETE FROM recipe_categories WHERE id = $1`, category.ID)

	assert.NotEqual(t, uuid.Nil, category.ID)
	assert.Equal(t, name, category.Name)
}

func TestCreateRecipeCategory_DuplicateName(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-recipe-category-" + uuid.NewString()
	insertTestRecipeCategory(t, name)

	category, err := recipeCategoryRepo.Create(ctx, name)
	assert.Nil(t, category)
	assert.ErrorIs(t, err, models.ErrRecipeCategoryNameTaken)
}

func TestListRecipeCategories_OrderedByName(t *testing.T) {
	ctx := context.Background()
	prefix := "repo-test-list-" + uuid.NewString() + "-"
	insertTestRecipeCategory(t, prefix+"Zed")
	insertTestRecipeCategory(t, prefix+"Alpha")
	insertTestRecipeCategory(t, prefix+"Mid")

	categories, err := recipeCategoryRepo.List(ctx)
	require.NoError(t, err)

	var gotNames []string
	for _, c := range categories {
		if len(c.Name) >= len(prefix) && c.Name[:len(prefix)] == prefix {
			gotNames = append(gotNames, c.Name)
		}
	}
	assert.Equal(t, []string{prefix + "Alpha", prefix + "Mid", prefix + "Zed"}, gotNames)
}

func TestUpdateRecipeCategory_Success(t *testing.T) {
	ctx := context.Background()
	id := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	newName := "repo-test-recipe-category-" + uuid.NewString()

	updated, err := recipeCategoryRepo.Update(ctx, id.String(), newName)
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, newName, updated.Name)
}

func TestUpdateRecipeCategory_DuplicateName(t *testing.T) {
	ctx := context.Background()
	takenName := "repo-test-recipe-category-" + uuid.NewString()
	insertTestRecipeCategory(t, takenName)
	id := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	updated, err := recipeCategoryRepo.Update(ctx, id.String(), takenName)
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrRecipeCategoryNameTaken)
}

func TestUpdateRecipeCategory_NotFound(t *testing.T) {
	ctx := context.Background()

	updated, err := recipeCategoryRepo.Update(ctx, uuid.NewString(), "repo-test-recipe-category-"+uuid.NewString())
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrRecipeCategoryNotFound)
}

func TestDeleteRecipeCategory_Success(t *testing.T) {
	ctx := context.Background()
	id := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	err := recipeCategoryRepo.Delete(ctx, id.String())
	require.NoError(t, err)

	row := db.DB.QueryRow(ctx, `SELECT id FROM recipe_categories WHERE id = $1`, id)
	var found uuid.UUID
	scanErr := row.Scan(&found)
	assert.ErrorIs(t, scanErr, pgx.ErrNoRows, "expected the row to be gone")
}

func TestDeleteRecipeCategory_NotFound(t *testing.T) {
	ctx := context.Background()

	err := recipeCategoryRepo.Delete(ctx, uuid.NewString())
	assert.ErrorIs(t, err, models.ErrRecipeCategoryNotFound)
}

func TestDeleteRecipeCategory_InUse(t *testing.T) {
	ctx := context.Background()
	id := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	recipeID := uuid.New()
	_, err := db.DB.Exec(ctx,
		`INSERT INTO recipes (id, name, time_in_minutes, instructions, serves) VALUES ($1, $2, $3, $4, $5)`,
		recipeID, "repo-test-recipe-"+uuid.NewString(), 30, []string{"step 1"}, 4,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipeID)

	_, err = db.DB.Exec(ctx,
		`INSERT INTO recipe_categories_recipes (recipe_id, category_id) VALUES ($1, $2)`,
		recipeID, id,
	)
	require.NoError(t, err)

	deleteErr := recipeCategoryRepo.Delete(ctx, id.String())
	assert.ErrorIs(t, deleteErr, models.ErrRecipeCategoryInUse)
}
