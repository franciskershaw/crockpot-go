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

func insertTestUnit(t *testing.T, name, abbreviation string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO units (id, name, abbreviation) VALUES ($1, $2, $3)`,
		id, name, abbreviation,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM units WHERE id = $1`, id)
	return id
}

func TestCreateUnit_CreatesNewUnit(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-unit-" + uuid.NewString()
	abbreviation := "ru-" + uuid.NewString()

	unit, err := unitRepo.Create(ctx, name, abbreviation)
	require.NoError(t, err)
	require.NotNil(t, unit)
	cleanupExec(t, `DELETE FROM units WHERE id = $1`, unit.ID)

	assert.NotEqual(t, uuid.Nil, unit.ID)
	assert.Equal(t, name, unit.Name)
	assert.Equal(t, abbreviation, unit.Abbreviation)
}

func TestCreateUnit_DuplicateName(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-unit-" + uuid.NewString()
	insertTestUnit(t, name, "ru-"+uuid.NewString())

	unit, err := unitRepo.Create(ctx, name, "ru-"+uuid.NewString())
	assert.Nil(t, unit)
	assert.ErrorIs(t, err, models.ErrUnitNameTaken)
}

func TestCreateUnit_DuplicateAbbreviation(t *testing.T) {
	ctx := context.Background()
	abbreviation := "ru-" + uuid.NewString()
	insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), abbreviation)

	unit, err := unitRepo.Create(ctx, "repo-test-unit-"+uuid.NewString(), abbreviation)
	assert.Nil(t, unit)
	assert.ErrorIs(t, err, models.ErrUnitAbbreviationTaken)
}

func TestListUnits_OrderedByName(t *testing.T) {
	ctx := context.Background()
	prefix := "repo-test-unit-list-" + uuid.NewString() + "-"
	insertTestUnit(t, prefix+"Zed", "ru-"+uuid.NewString())
	insertTestUnit(t, prefix+"Alpha", "ru-"+uuid.NewString())
	insertTestUnit(t, prefix+"Mid", "ru-"+uuid.NewString())

	units, err := unitRepo.List(ctx)
	require.NoError(t, err)

	var gotNames []string
	for _, u := range units {
		if len(u.Name) >= len(prefix) && u.Name[:len(prefix)] == prefix {
			gotNames = append(gotNames, u.Name)
		}
	}
	assert.Equal(t, []string{prefix + "Alpha", prefix + "Mid", prefix + "Zed"}, gotNames)
}

func TestUpdateUnit_PartialName(t *testing.T) {
	ctx := context.Background()
	abbreviation := "ru-" + uuid.NewString()
	id := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), abbreviation)
	newName := "repo-test-unit-" + uuid.NewString()

	updated, err := unitRepo.Update(ctx, id.String(), newName, "")
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, newName, updated.Name)
	assert.Equal(t, abbreviation, updated.Abbreviation, "abbreviation should be left unchanged")
}

func TestUpdateUnit_PartialAbbreviation(t *testing.T) {
	ctx := context.Background()
	name := "repo-test-unit-" + uuid.NewString()
	id := insertTestUnit(t, name, "ru-"+uuid.NewString())
	newAbbreviation := "ru-" + uuid.NewString()

	updated, err := unitRepo.Update(ctx, id.String(), "", newAbbreviation)
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, name, updated.Name, "name should be left unchanged")
	assert.Equal(t, newAbbreviation, updated.Abbreviation)
}

func TestUpdateUnit_DuplicateName(t *testing.T) {
	ctx := context.Background()
	takenName := "repo-test-unit-" + uuid.NewString()
	insertTestUnit(t, takenName, "ru-"+uuid.NewString())
	id := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())

	updated, err := unitRepo.Update(ctx, id.String(), takenName, "")
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrUnitNameTaken)
}

func TestUpdateUnit_NotFound(t *testing.T) {
	ctx := context.Background()

	updated, err := unitRepo.Update(ctx, uuid.NewString(), "repo-test-unit-"+uuid.NewString(), "")
	assert.Nil(t, updated)
	assert.ErrorIs(t, err, models.ErrUnitNotFound)
}

func TestDeleteUnit_Success(t *testing.T) {
	ctx := context.Background()
	id := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())

	err := unitRepo.Delete(ctx, id.String())
	require.NoError(t, err)

	row := db.DB.QueryRow(ctx, `SELECT id FROM units WHERE id = $1`, id)
	var found uuid.UUID
	scanErr := row.Scan(&found)
	assert.ErrorIs(t, scanErr, pgx.ErrNoRows, "expected the row to be gone")
}

func TestDeleteUnit_NotFound(t *testing.T) {
	ctx := context.Background()

	err := unitRepo.Delete(ctx, uuid.NewString())
	assert.ErrorIs(t, err, models.ErrUnitNotFound)
}

// Exercises shopping_list_items; recipe_ingredients has the identical ON DELETE RESTRICT + 23001 shape, so this is representative of both.
func TestDeleteUnit_InUse(t *testing.T) {
	ctx := context.Background()
	unitID := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	categoryID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())

	itemID := uuid.New()
	_, err := db.DB.Exec(ctx,
		`INSERT INTO items (id, category_id, name) VALUES ($1, $2, $3)`,
		itemID, categoryID, "repo-test-item-"+uuid.NewString(),
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM items WHERE id = $1`, itemID)

	shoppingListUserID := uuid.New()
	_, err = db.DB.Exec(ctx,
		`INSERT INTO users (id, google_id, email) VALUES ($1, $2, $3)`,
		shoppingListUserID, "repo-test-google-"+uuid.NewString(), "repo-test-"+uuid.NewString()+"@example.com",
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, shoppingListUserID)

	shoppingListID := uuid.New()
	_, err = db.DB.Exec(ctx,
		`INSERT INTO shopping_lists (id, user_id) VALUES ($1, $2)`,
		shoppingListID, shoppingListUserID,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM shopping_lists WHERE id = $1`, shoppingListID)

	shoppingListItemID := uuid.New()
	_, err = db.DB.Exec(ctx,
		`INSERT INTO shopping_list_items (id, shopping_list_id, item_id, unit_id, quantity) VALUES ($1, $2, $3, $4, $5)`,
		shoppingListItemID, shoppingListID, itemID, unitID, 1,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM shopping_list_items WHERE id = $1`, shoppingListItemID)

	deleteErr := unitRepo.Delete(ctx, unitID.String())
	assert.ErrorIs(t, deleteErr, models.ErrUnitInUse)
}
