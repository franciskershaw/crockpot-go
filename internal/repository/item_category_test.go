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

func insertTestItemCategory(t *testing.T, name, icon string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO item_categories (id, name, icon) VALUES ($1, $2, $3)`,
		id, name, icon,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM item_categories WHERE id = $1`, id)
	return id
}

func TestCreateItemCategory_CreatesNewCategory(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-category-" + uuid.NewString()
	icon := "repo-test-icon-" + uuid.NewString()

	category, err := itemCategoryRepo.Create(ctx, name, icon)
	require.NoError(t, err)
	require.NotNil(t, category)
	cleanupExec(t, `DELETE FROM item_categories WHERE id = $1`, category.ID)

	assert.NotEqual(t, uuid.Nil, category.ID)
	assert.Equal(t, name, category.Name)
	assert.Equal(t, icon, category.Icon)
}

func TestCreateItemCategory_DuplicateName(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-category-" + uuid.NewString()
	insertTestItemCategory(t, name, "repo-test-icon-"+uuid.NewString())

	category, err := itemCategoryRepo.Create(ctx, name, "repo-test-icon-"+uuid.NewString())
	assert.Nil(t, category)
	assert.ErrorIs(t, err, models.ErrItemCategoryNameTaken)
}

func TestCreateItemCategory_DuplicateIcon(t *testing.T) {
	ctx := context.Background()
	icon := "repo-test-icon-" + uuid.NewString()
	insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), icon)

	category, err := itemCategoryRepo.Create(ctx, "repo-test-category-"+uuid.NewString(), icon)
	assert.Nil(t, category)
	assert.ErrorIs(t, err, models.ErrItemCategoryIconTaken)
}

func TestListItemCategories_OrderedByName(t *testing.T) {
	ctx := context.Background()
	prefix := "repo-test-list-" + uuid.NewString() + "-"
	insertTestItemCategory(t, prefix+"Zed", "repo-test-icon-"+uuid.NewString())
	insertTestItemCategory(t, prefix+"Alpha", "repo-test-icon-"+uuid.NewString())
	insertTestItemCategory(t, prefix+"Mid", "repo-test-icon-"+uuid.NewString())

	categories, err := itemCategoryRepo.List(ctx)
	require.NoError(t, err)

	var gotNames []string
	for _, c := range categories {
		if len(c.Name) >= len(prefix) && c.Name[:len(prefix)] == prefix {
			gotNames = append(gotNames, c.Name)
		}
	}
	assert.Equal(t, []string{prefix + "Alpha", prefix + "Mid", prefix + "Zed"}, gotNames)
}

func TestUpdateItemCategory_PartialName(t *testing.T) {
	ctx := context.Background()
	icon := "repo-test-icon-" + uuid.NewString()
	id := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), icon)
	newName := "repo-test-category-" + uuid.NewString()

	updated, err := itemCategoryRepo.Update(ctx, id.String(), newName, "")
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, newName, updated.Name)
	assert.Equal(t, icon, updated.Icon, "icon should be left unchanged")
}

func TestUpdateItemCategory_PartialIcon(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-category-" + uuid.NewString()
	id := insertTestItemCategory(t, name, "repo-test-icon-"+uuid.NewString())
	newIcon := "repo-test-icon-" + uuid.NewString()

	updated, err := itemCategoryRepo.Update(ctx, id.String(), "", newIcon)
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, name, updated.Name, "name should be left unchanged")
	assert.Equal(t, newIcon, updated.Icon)
}

func TestUpdateItemCategory_DuplicateName(t *testing.T) {
	ctx := context.Background()
	takenName := "repo-test-category-" + uuid.NewString()
	insertTestItemCategory(t, takenName, "repo-test-icon-"+uuid.NewString())
	id := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())

	updated, err := itemCategoryRepo.Update(ctx, id.String(), takenName, "")
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrItemCategoryNameTaken)
}

func TestUpdateItemCategory_NotFound(t *testing.T) {
	ctx := context.Background()

	updated, err := itemCategoryRepo.Update(ctx, uuid.NewString(), "repo-test-category-"+uuid.NewString(), "")
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrItemCategoryNotFound)
}

func TestDeleteItemCategory_Success(t *testing.T) {
	ctx := context.Background()
	id := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())

	err := itemCategoryRepo.Delete(ctx, id.String())
	require.NoError(t, err)

	row := db.DB.QueryRow(ctx, `SELECT id FROM item_categories WHERE id = $1`, id)
	var found uuid.UUID
	scanErr := row.Scan(&found)
	assert.ErrorIs(t, scanErr, pgx.ErrNoRows, "expected the row to be gone")
}

func TestDeleteItemCategory_NotFound(t *testing.T) {
	ctx := context.Background()

	err := itemCategoryRepo.Delete(ctx, uuid.NewString())
	assert.ErrorIs(t, err, models.ErrItemCategoryNotFound)
}

func TestDeleteItemCategory_InUse(t *testing.T) {
	ctx := context.Background()
	id := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := uuid.New()
	_, err := db.DB.Exec(ctx,
		`INSERT INTO items (id, category_id, name) VALUES ($1, $2, $3)`,
		itemID, id, "repo-test-item-"+uuid.NewString(),
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM items WHERE id = $1`, itemID)

	deleteErr := itemCategoryRepo.Delete(ctx, id.String())
	assert.ErrorIs(t, deleteErr, models.ErrItemCategoryInUse)
}
