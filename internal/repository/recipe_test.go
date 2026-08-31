package repository_test

import (
	"context"
	"testing"

	"github.com/franciskershaw/crockpot-go/db"
	"github.com/franciskershaw/crockpot-go/internal/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertTestUser(t *testing.T, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO users (id, google_id, email, name) VALUES ($1, $2, $3, $4)`,
		id, "repo-test-google-"+id.String(), "repo-test-"+id.String()+"@example.com", name,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM users WHERE id = $1`, id)
	return id
}

func insertTestRecipeRow(t *testing.T, createdBy uuid.UUID, approved bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO recipes (id, name, time_in_minutes, instructions, serves, approved, created_by_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, "repo-test-recipe-"+id.String(), 30, []string{"step 1"}, 4, approved, createdBy,
	)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, id)
	return id
}

func baseRecipeInput(createdBy, itemID, recipeCategoryID uuid.UUID) models.CreateRecipeInput {
	return models.CreateRecipeInput{
		Name:          "repo-test-recipe-" + uuid.NewString(),
		TimeInMinutes: 30,
		Serves:        4,
		Instructions:  []string{"step one"},
		Notes:         []string{},
		CategoryIDs:   []uuid.UUID{recipeCategoryID},
		Ingredients:   []models.Ingredient{{ItemID: itemID, Quantity: 3}},
		CreatedByID:   createdBy,
		Approved:      false,
	}
}

func rowCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.DB.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

func TestCountRecipesByCreator_CountsApprovedAndUnapproved(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Repo Test Cook")
	insertTestRecipeRow(t, userID, true)
	insertTestRecipeRow(t, userID, false)
	insertTestRecipeRow(t, userID, false)

	count, err := recipeRepo.CountByCreator(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestCountRecipesByCreator_ZeroWhenNone(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Empty Cook")

	count, err := recipeRepo.CountByCreator(ctx, userID.String())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCreateRecipe_MinimalPersistsAllParts(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Jane Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	input := models.CreateRecipeInput{
		Name:          "repo-test-recipe-" + uuid.NewString(),
		TimeInMinutes: 45,
		Serves:        6,
		Instructions:  []string{"brown the beef", "add stock"},
		Notes:         []string{},
		CategoryIDs:   []uuid.UUID{recipeCatID},
		Ingredients:   []models.Ingredient{{ItemID: itemID, Quantity: 3}},
		CreatedByID:   userID,
		Approved:      false,
	}

	recipe, err := recipeRepo.Create(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, recipe)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)

	assert.NotEqual(t, uuid.Nil, recipe.ID)
	assert.Equal(t, input.Name, recipe.Name)
	assert.Equal(t, 45, recipe.TimeInMinutes)
	assert.Equal(t, 6, recipe.Serves)
	assert.Equal(t, []string{"brown the beef", "add stock"}, recipe.Instructions)
	assert.Empty(t, recipe.Notes)
	assert.False(t, recipe.Approved)
	assert.Nil(t, recipe.ImageURL)
	assert.Nil(t, recipe.ImageFilename)
	assert.Equal(t, userID, recipe.CreatedByID)
	require.NotNil(t, recipe.CreatedByName)
	assert.Equal(t, "Jane Cook", *recipe.CreatedByName)
	assert.Equal(t, []uuid.UUID{recipeCatID}, recipe.CategoryIDs)
	require.Len(t, recipe.Ingredients, 1)
	assert.Equal(t, itemID, recipe.Ingredients[0].ItemID)
	assert.Nil(t, recipe.Ingredients[0].UnitID)
	assert.Equal(t, 3.0, recipe.Ingredients[0].Quantity)
	assert.False(t, recipe.CreatedAt.IsZero())

	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipe_ingredients WHERE recipe_id = $1`, recipe.ID))
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipe_categories_recipes WHERE recipe_id = $1`, recipe.ID))
}

func TestCreateRecipe_FullWithImageUnitNotesAndApproved(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Admin Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	unitID := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	recipeCat1 := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeCat2 := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	url := "https://cdn.example.com/pic.jpg"
	filename := "pic_abc123"
	input := models.CreateRecipeInput{
		Name:          "repo-test-recipe-" + uuid.NewString(),
		TimeInMinutes: 360,
		Serves:        4,
		Instructions:  []string{"step one"},
		Notes:         []string{"freezes well", "double the garlic"},
		CategoryIDs:   []uuid.UUID{recipeCat1, recipeCat2},
		Ingredients:   []models.Ingredient{{ItemID: itemID, UnitID: &unitID, Quantity: 800}},
		ImageURL:      &url,
		ImageFilename: &filename,
		CreatedByID:   userID,
		Approved:      true,
	}

	recipe, err := recipeRepo.Create(ctx, input)
	require.NoError(t, err)
	require.NotNil(t, recipe)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)

	assert.True(t, recipe.Approved)
	require.NotNil(t, recipe.ImageURL)
	assert.Equal(t, url, *recipe.ImageURL)
	require.NotNil(t, recipe.ImageFilename)
	assert.Equal(t, filename, *recipe.ImageFilename)
	assert.Equal(t, []string{"freezes well", "double the garlic"}, recipe.Notes)
	assert.Equal(t, []uuid.UUID{recipeCat1, recipeCat2}, recipe.CategoryIDs, "category ids echo back in submit order")
	require.Len(t, recipe.Ingredients, 1)
	require.NotNil(t, recipe.Ingredients[0].UnitID)
	assert.Equal(t, unitID, *recipe.Ingredients[0].UnitID)
	assert.Equal(t, 800.0, recipe.Ingredients[0].Quantity)
}

func TestCreateRecipe_PopulatesCreatedByNameFromUsersRow(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Distinctive Name 12345")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	recipe, err := recipeRepo.Create(ctx, baseRecipeInput(userID, itemID, recipeCatID))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)

	require.NotNil(t, recipe.CreatedByName)
	assert.Equal(t, "Distinctive Name 12345", *recipe.CreatedByName)

	var dbName string
	require.NoError(t, db.DB.QueryRow(ctx, `SELECT created_by_name FROM recipes WHERE id = $1`, recipe.ID).Scan(&dbName))
	assert.Equal(t, "Distinctive Name 12345", dbName)
}

func TestCreateRecipe_PreservesIngredientOrder(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemC := insertTestItem(t, "repo-test-item-c-"+uuid.NewString(), catID)
	itemA := insertTestItem(t, "repo-test-item-a-"+uuid.NewString(), catID)
	itemB := insertTestItem(t, "repo-test-item-b-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	input := baseRecipeInput(userID, itemC, recipeCatID)
	input.Ingredients = []models.Ingredient{
		{ItemID: itemC, Quantity: 1},
		{ItemID: itemA, Quantity: 2},
		{ItemID: itemB, Quantity: 3},
	}

	recipe, err := recipeRepo.Create(ctx, input)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)

	require.Len(t, recipe.Ingredients, 3)
	assert.Equal(t, []uuid.UUID{itemC, itemA, itemB},
		[]uuid.UUID{recipe.Ingredients[0].ItemID, recipe.Ingredients[1].ItemID, recipe.Ingredients[2].ItemID},
		"ingredients must come back in submit order, not sorted by item_id")
}

func TestCreateRecipe_DuplicateIngredientItemID(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{
		{ItemID: itemID, Quantity: 1},
		{ItemID: itemID, Quantity: 2},
	}
	cleanupExec(t, `DELETE FROM recipes WHERE name = $1`, input.Name)

	recipe, err := recipeRepo.Create(ctx, input)
	assert.Nil(t, recipe)
	assert.ErrorIs(t, err, models.ErrRecipeDuplicateIngredient)
}

func TestCreateRecipe_QuantityRoundsToColumnScale(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{{ItemID: itemID, Quantity: 0.333}}

	recipe, err := recipeRepo.Create(ctx, input)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)

	require.Len(t, recipe.Ingredients, 1)
	assert.InDelta(t, 0.33, recipe.Ingredients[0].Quantity, 1e-9)
}

func TestCreateRecipe_InvalidItemID(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	input := baseRecipeInput(userID, uuid.New(), recipeCatID)
	cleanupExec(t, `DELETE FROM recipes WHERE name = $1`, input.Name)

	recipe, err := recipeRepo.Create(ctx, input)
	assert.Nil(t, recipe)
	assert.ErrorIs(t, err, models.ErrRecipeInvalidItem)
}

func TestCreateRecipe_InvalidUnitID(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	badUnit := uuid.New()
	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{{ItemID: itemID, UnitID: &badUnit, Quantity: 1}}
	cleanupExec(t, `DELETE FROM recipes WHERE name = $1`, input.Name)

	recipe, err := recipeRepo.Create(ctx, input)
	assert.Nil(t, recipe)
	assert.ErrorIs(t, err, models.ErrRecipeInvalidUnit)
}

func TestCreateRecipe_InvalidCategoryID(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)

	input := baseRecipeInput(userID, itemID, uuid.New())
	cleanupExec(t, `DELETE FROM recipes WHERE name = $1`, input.Name)

	recipe, err := recipeRepo.Create(ctx, input)
	assert.Nil(t, recipe)
	assert.ErrorIs(t, err, models.ErrRecipeInvalidCategory)
}

func TestCreateRecipe_UnitNotInItemAllowedSet(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	allowedUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	otherUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	insertTestItemAllowedUnit(t, itemID, allowedUnit)

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{{ItemID: itemID, UnitID: &otherUnit, Quantity: 1}}
	cleanupExec(t, `DELETE FROM recipes WHERE name = $1`, input.Name)

	recipe, err := recipeRepo.Create(ctx, input)
	assert.Nil(t, recipe)
	assert.ErrorIs(t, err, models.ErrIngredientUnitNotAllowed)
}

func TestCreateRecipe_UnitInItemAllowedSet(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	allowedUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	insertTestItemAllowedUnit(t, itemID, allowedUnit)

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{{ItemID: itemID, UnitID: &allowedUnit, Quantity: 2}}

	recipe, err := recipeRepo.Create(ctx, input)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)

	require.Len(t, recipe.Ingredients, 1)
	require.NotNil(t, recipe.Ingredients[0].UnitID)
	assert.Equal(t, allowedUnit, *recipe.Ingredients[0].UnitID)
}

func TestCreateRecipe_EmptyAllowedSetAcceptsAnyUnit(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	someUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{{ItemID: itemID, UnitID: &someUnit, Quantity: 1}}

	recipe, err := recipeRepo.Create(ctx, input)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)
}

func TestCreateRecipe_NullUnitBypassesAllowedCheck(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	allowedUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	insertTestItemAllowedUnit(t, itemID, allowedUnit)

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{{ItemID: itemID, Quantity: 5}}

	recipe, err := recipeRepo.Create(ctx, input)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, recipe.ID)

	require.Len(t, recipe.Ingredients, 1)
	assert.Nil(t, recipe.Ingredients[0].UnitID)
}

func TestCreateRecipe_RollsBackOnInvalidIngredient(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	goodItem := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	input := baseRecipeInput(userID, goodItem, recipeCatID)
	input.Ingredients = []models.Ingredient{
		{ItemID: goodItem, Quantity: 1},
		{ItemID: uuid.New(), Quantity: 2},
	}

	txErr := transactor.WithinTx(ctx, func(ctx context.Context) error {
		_, err := recipeRepo.Create(ctx, input)
		return err
	})
	assert.ErrorIs(t, txErr, models.ErrRecipeInvalidItem)

	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipes WHERE name = $1`, input.Name),
		"no recipe row should survive the rolled-back transaction")
}

func TestCreateRecipe_RollsBackOnDisallowedUnit(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	allowedUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	otherUnit := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), "ru-"+uuid.NewString())
	insertTestItemAllowedUnit(t, itemID, allowedUnit)

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{{ItemID: itemID, UnitID: &otherUnit, Quantity: 1}}

	txErr := transactor.WithinTx(ctx, func(ctx context.Context) error {
		_, err := recipeRepo.Create(ctx, input)
		return err
	})
	assert.ErrorIs(t, txErr, models.ErrIngredientUnitNotAllowed)

	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipes WHERE name = $1`, input.Name),
		"no recipe row should survive the rolled-back transaction")
}
