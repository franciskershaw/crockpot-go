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
	return insertTestRecipeRowWithTime(t, createdBy, approved, 30)
}

func insertTestRecipeRowWithTime(t *testing.T, createdBy uuid.UUID, approved bool, timeInMinutes int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.DB.Exec(context.Background(),
		`INSERT INTO recipes (id, name, time_in_minutes, instructions, serves, approved, created_by_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, "repo-test-recipe-"+id.String(), timeInMinutes, []string{"step 1"}, 4, approved, createdBy,
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

// currentApprovedTimeBounds lets a test insert values guaranteed outside it, without
// assuming anything about the shared dev DB's existing (real, migrated) recipe data.
func currentApprovedTimeBounds(t *testing.T) (min, max int) {
	t.Helper()
	require.NoError(t, db.DB.QueryRow(context.Background(),
		`SELECT COALESCE(MIN(time_in_minutes), 0), COALESCE(MAX(time_in_minutes), 120) FROM recipes WHERE approved`,
	).Scan(&min, &max))
	return min, max
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

func TestGetTimeRange_ReflectsOnlyApprovedRecipes(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Time Range Cook")
	baseMin, baseMax := currentApprovedTimeBounds(t)

	insertTestRecipeRowWithTime(t, userID, true, baseMin-1)
	insertTestRecipeRowWithTime(t, userID, true, baseMax+1)
	insertTestRecipeRowWithTime(t, userID, false, baseMin-1000)
	insertTestRecipeRowWithTime(t, userID, false, baseMax+1000)

	got, err := recipeRepo.GetTimeRange(ctx)
	require.NoError(t, err)
	assert.Equal(t, baseMin-1, got.MinTime)
	assert.Equal(t, baseMax+1, got.MaxTime)
}

func TestCreateRecipe_MinimalPersistsAllParts(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Jane Cook")
	catName := "repo-test-category-" + uuid.NewString()
	catID := insertTestItemCategory(t, catName, "repo-test-icon-"+uuid.NewString())
	itemName := "repo-test-item-" + uuid.NewString()
	itemID := insertTestItem(t, itemName, catID)
	recipeCatName := "repo-test-recipe-category-" + uuid.NewString()
	recipeCatID := insertTestRecipeCategory(t, recipeCatName)

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
	assert.Nil(t, recipe.Description)
	assert.Equal(t, userID, recipe.CreatedByID)
	require.NotNil(t, recipe.CreatedByName)
	assert.Equal(t, "Jane Cook", *recipe.CreatedByName)
	require.Len(t, recipe.Categories, 1)
	assert.Equal(t, recipeCatID, recipe.Categories[0].ID)
	assert.Equal(t, recipeCatName, recipe.Categories[0].Name)
	require.Len(t, recipe.Ingredients, 1)
	assert.Equal(t, itemID, recipe.Ingredients[0].ItemID)
	assert.Equal(t, itemName, recipe.Ingredients[0].ItemName)
	assert.Equal(t, catID, recipe.Ingredients[0].ItemCategoryID)
	assert.Equal(t, catName, recipe.Ingredients[0].ItemCategoryName)
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
	unitAbbr := "ru-" + uuid.NewString()
	unitID := insertTestUnit(t, "repo-test-unit-"+uuid.NewString(), unitAbbr)
	recipeCat1Name := "repo-test-recipe-category-" + uuid.NewString()
	recipeCat1 := insertTestRecipeCategory(t, recipeCat1Name)
	recipeCat2Name := "repo-test-recipe-category-" + uuid.NewString()
	recipeCat2 := insertTestRecipeCategory(t, recipeCat2Name)

	url := "https://cdn.example.com/pic.jpg"
	filename := "pic_abc123"
	description := "a hearty stew"
	input := models.CreateRecipeInput{
		Name:          "repo-test-recipe-" + uuid.NewString(),
		Description:   &description,
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
	require.NotNil(t, recipe.Description)
	assert.Equal(t, description, *recipe.Description)
	assert.Equal(t, []string{"freezes well", "double the garlic"}, recipe.Notes)
	require.Len(t, recipe.Categories, 2)
	assert.ElementsMatch(t,
		[]models.CategoryRef{{ID: recipeCat1, Name: recipeCat1Name}, {ID: recipeCat2, Name: recipeCat2Name}},
		recipe.Categories,
	)
	require.Len(t, recipe.Ingredients, 1)
	require.NotNil(t, recipe.Ingredients[0].UnitID)
	assert.Equal(t, unitID, *recipe.Ingredients[0].UnitID)
	require.NotNil(t, recipe.Ingredients[0].UnitAbbreviation)
	assert.Equal(t, unitAbbr, *recipe.Ingredients[0].UnitAbbreviation)
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

func insertTestNonIngredientItem(t *testing.T) uuid.UUID {
	t.Helper()
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	_, err := db.DB.Exec(context.Background(), `UPDATE item_categories SET is_ingredient = false WHERE id = $1`, catID)
	require.NoError(t, err)
	return insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
}

func TestCreateRecipe_RejectsNonIngredientItemAndRollsBack(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	householdID := insertTestNonIngredientItem(t)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	input := baseRecipeInput(userID, itemID, recipeCatID)
	input.Ingredients = []models.Ingredient{
		{ItemID: itemID, Quantity: 1},
		{ItemID: householdID, Quantity: 1},
	}
	cleanupExec(t, `DELETE FROM recipes WHERE name = $1`, input.Name)

	txErr := transactor.WithinTx(ctx, func(ctx context.Context) error {
		_, err := recipeRepo.Create(ctx, input)
		return err
	})
	assert.ErrorIs(t, txErr, models.ErrRecipeNonIngredientItem)

	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipes WHERE name = $1`, input.Name),
		"no recipe row should survive the rolled-back transaction")
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

func TestUpdateRecipe_OwnerReplacesAllFieldsAndWholesaleIngredientsCategories(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	oldItem := insertTestItem(t, "repo-test-item-old-"+uuid.NewString(), catID)
	newItemName := "repo-test-item-new-" + uuid.NewString()
	newItem := insertTestItem(t, newItemName, catID)
	oldRecipeCat := insertTestRecipeCategory(t, "repo-test-recipe-category-old-"+uuid.NewString())
	newRecipeCatName := "repo-test-recipe-category-new-" + uuid.NewString()
	newRecipeCat := insertTestRecipeCategory(t, newRecipeCatName)

	created, err := recipeRepo.Create(ctx, baseRecipeInput(userID, oldItem, oldRecipeCat))
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, created.ID)

	description := "updated description"
	update := models.CreateRecipeInput{
		Name:          "repo-test-recipe-updated-" + uuid.NewString(),
		Description:   &description,
		TimeInMinutes: 99,
		Serves:        8,
		Instructions:  []string{"new step"},
		Notes:         []string{"new note"},
		CategoryIDs:   []uuid.UUID{newRecipeCat},
		Ingredients:   []models.Ingredient{{ItemID: newItem, Quantity: 5}},
	}

	updated, _, err := recipeRepo.Update(ctx, created.ID.String(), update, userID.String(), false)
	require.NoError(t, err)
	require.NotNil(t, updated)

	assert.Equal(t, update.Name, updated.Name)
	assert.False(t, updated.Approved, "an owner's edit of a pending recipe stays pending")
	require.NotNil(t, updated.Description)
	assert.Equal(t, description, *updated.Description)
	assert.Equal(t, 99, updated.TimeInMinutes)
	assert.Equal(t, 8, updated.Serves)
	assert.Equal(t, []string{"new step"}, updated.Instructions)
	assert.Equal(t, []string{"new note"}, updated.Notes)
	require.Len(t, updated.Categories, 1)
	assert.Equal(t, newRecipeCat, updated.Categories[0].ID)
	assert.Equal(t, newRecipeCatName, updated.Categories[0].Name)
	require.Len(t, updated.Ingredients, 1)
	assert.Equal(t, newItem, updated.Ingredients[0].ItemID)
	assert.Equal(t, newItemName, updated.Ingredients[0].ItemName)

	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipe_ingredients WHERE recipe_id = $1 AND item_id = $2`, created.ID, oldItem),
		"old ingredient link must be gone after wholesale replace")
	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipe_categories_recipes WHERE recipe_id = $1 AND category_id = $2`, created.ID, oldRecipeCat),
		"old category link must be gone after wholesale replace")
}

func TestUpdateRecipe_OwnerEditingApproved_Locked(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeID := insertTestRecipeRow(t, userID, true)

	_, _, err := recipeRepo.Update(ctx, recipeID.String(), baseRecipeInput(userID, itemID, recipeCatID), userID.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeApprovedLocked)

	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1 AND approved AND name = $2`, recipeID, "repo-test-recipe-"+recipeID.String()),
		"a locked recipe keeps its name and approval")
	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipe_ingredients WHERE recipe_id = $1`, recipeID))
}

func TestUpdateRecipe_RejectsNonIngredientItem(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	householdID := insertTestNonIngredientItem(t)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeID := insertTestRecipeRow(t, userID, false)

	txErr := transactor.WithinTx(ctx, func(ctx context.Context) error {
		_, _, err := recipeRepo.Update(ctx, recipeID.String(), baseRecipeInput(userID, householdID, recipeCatID), userID.String(), false)
		return err
	})
	assert.ErrorIs(t, txErr, models.ErrRecipeNonIngredientItem)

	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipe_ingredients WHERE recipe_id = $1`, recipeID),
		"a rejected update must not write the non-ingredient item")
}

func TestUpdateRecipe_AdminEditingAnothersApprovedStaysApproved(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeID := insertTestRecipeRow(t, owner, true)

	updated, _, err := recipeRepo.Update(ctx, recipeID.String(), baseRecipeInput(owner, itemID, recipeCatID), admin.String(), true)
	require.NoError(t, err)
	assert.True(t, updated.Approved)
}

func TestUpdateRecipe_AdminEditingAnothersUnapprovedStaysUnapproved(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeID := insertTestRecipeRow(t, owner, false)

	updated, _, err := recipeRepo.Update(ctx, recipeID.String(), baseRecipeInput(owner, itemID, recipeCatID), admin.String(), true)
	require.NoError(t, err)
	assert.False(t, updated.Approved)
}

func TestUpdateRecipe_NonOwnerNonAdminOnApproved_Forbidden(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	other := insertTestUser(t, "Other")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeID := insertTestRecipeRow(t, owner, true)

	_, _, err := recipeRepo.Update(ctx, recipeID.String(), baseRecipeInput(owner, itemID, recipeCatID), other.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeForbidden)
}

func TestUpdateRecipe_NonOwnerNonAdminOnUnapproved_NotFound(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	other := insertTestUser(t, "Other")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeID := insertTestRecipeRow(t, owner, false)

	_, _, err := recipeRepo.Update(ctx, recipeID.String(), baseRecipeInput(owner, itemID, recipeCatID), other.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
}

func TestUpdateRecipe_NonexistentRecipe_NotFound(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemID := insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID)
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())

	_, _, err := recipeRepo.Update(ctx, uuid.NewString(), baseRecipeInput(userID, itemID, recipeCatID), userID.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
}

type imageFixture struct {
	userID, itemID, recipeCatID uuid.UUID
}

func newImageFixture(t *testing.T) imageFixture {
	t.Helper()
	catID := insertTestItemCategory(t, "repo-test-category-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	return imageFixture{
		userID:      insertTestUser(t, "Cook"),
		itemID:      insertTestItem(t, "repo-test-item-"+uuid.NewString(), catID),
		recipeCatID: insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString()),
	}
}

// createWithImage creates a recipe whose image public id is publicID ("" for no image).
func (f imageFixture) createWithImage(t *testing.T, publicID string) *models.RecipeDetail {
	t.Helper()
	input := baseRecipeInput(f.userID, f.itemID, f.recipeCatID)
	if publicID != "" {
		url := "https://res.cloudinary.com/test/image/upload/v1/" + publicID + ".jpg"
		input.ImageURL = &url
		input.ImageFilename = &publicID
	}
	created, err := recipeRepo.Create(context.Background(), input)
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, created.ID)
	return created
}

func (f imageFixture) input(image models.ImageUpdate, publicID string) models.CreateRecipeInput {
	input := baseRecipeInput(f.userID, f.itemID, f.recipeCatID)
	input.Image = image
	if publicID != "" {
		url := "https://res.cloudinary.com/test/image/upload/v1/" + publicID + ".jpg"
		input.ImageURL = &url
		input.ImageFilename = &publicID
	}
	return input
}

func testImageID() string { return "repo-test-img/" + uuid.NewString() }

func TestUpdateRecipe_ImageKeptByDefault(t *testing.T) {
	f := newImageFixture(t)
	old := testImageID()
	created := f.createWithImage(t, old)

	updated, orphan, err := recipeRepo.Update(context.Background(), created.ID.String(), f.input(models.ImageKeep, ""), f.userID.String(), false)
	require.NoError(t, err)
	require.NotNil(t, updated.ImageFilename)
	assert.Equal(t, old, *updated.ImageFilename)
	assert.Equal(t, created.ImageURL, updated.ImageURL)
	assert.Nil(t, orphan)
}

func TestUpdateRecipe_ImageReplaceReturnsOldForCleanup(t *testing.T) {
	f := newImageFixture(t)
	old, replacement := testImageID(), testImageID()
	created := f.createWithImage(t, old)

	updated, orphan, err := recipeRepo.Update(context.Background(), created.ID.String(), f.input(models.ImageReplace, replacement), f.userID.String(), false)
	require.NoError(t, err)
	require.NotNil(t, updated.ImageFilename)
	assert.Equal(t, replacement, *updated.ImageFilename)
	require.NotNil(t, orphan)
	assert.Equal(t, old, *orphan)
}

func TestUpdateRecipe_ImageRemoveReturnsOldForCleanup(t *testing.T) {
	f := newImageFixture(t)
	old := testImageID()
	created := f.createWithImage(t, old)

	updated, orphan, err := recipeRepo.Update(context.Background(), created.ID.String(), f.input(models.ImageRemove, ""), f.userID.String(), false)
	require.NoError(t, err)
	assert.Nil(t, updated.ImageURL)
	assert.Nil(t, updated.ImageFilename)
	require.NotNil(t, orphan)
	assert.Equal(t, old, *orphan)
}

func TestUpdateRecipe_ImageReplaceWithNoPreviousImage_NoOrphan(t *testing.T) {
	f := newImageFixture(t)
	created := f.createWithImage(t, "")
	replacement := testImageID()

	updated, orphan, err := recipeRepo.Update(context.Background(), created.ID.String(), f.input(models.ImageReplace, replacement), f.userID.String(), false)
	require.NoError(t, err)
	require.NotNil(t, updated.ImageFilename)
	assert.Equal(t, replacement, *updated.ImageFilename)
	assert.Nil(t, orphan)
}

func TestUpdateRecipe_ImageReplaceKeepsOldStillUsedElsewhere(t *testing.T) {
	f := newImageFixture(t)
	shared := testImageID()
	created := f.createWithImage(t, shared)
	f.createWithImage(t, shared)

	_, orphan, err := recipeRepo.Update(context.Background(), created.ID.String(), f.input(models.ImageReplace, testImageID()), f.userID.String(), false)
	require.NoError(t, err)
	assert.Nil(t, orphan)
}

func TestUpdateRecipe_ImageRemoveWithNoImage_NoOrphan(t *testing.T) {
	f := newImageFixture(t)
	created := f.createWithImage(t, "")

	_, orphan, err := recipeRepo.Update(context.Background(), created.ID.String(), f.input(models.ImageRemove, ""), f.userID.String(), false)
	require.NoError(t, err)
	assert.Nil(t, orphan)
}

func TestDeleteRecipe_ReturnsImageForCleanup(t *testing.T) {
	f := newImageFixture(t)
	old := testImageID()
	created := f.createWithImage(t, old)

	_, orphan, err := recipeRepo.Delete(context.Background(), created.ID.String(), f.userID.String(), false)
	require.NoError(t, err)
	require.NotNil(t, orphan)
	assert.Equal(t, old, *orphan)
}

func TestDeleteRecipe_NoImage_NoOrphan(t *testing.T) {
	f := newImageFixture(t)
	created := f.createWithImage(t, "")

	_, orphan, err := recipeRepo.Delete(context.Background(), created.ID.String(), f.userID.String(), false)
	require.NoError(t, err)
	assert.Nil(t, orphan)
}

func TestDeleteRecipe_ImageStillUsedElsewhere_NoOrphan(t *testing.T) {
	f := newImageFixture(t)
	shared := testImageID()
	created := f.createWithImage(t, shared)
	f.createWithImage(t, shared)

	_, orphan, err := recipeRepo.Delete(context.Background(), created.ID.String(), f.userID.String(), false)
	require.NoError(t, err)
	assert.Nil(t, orphan)
}

func TestUpdateRecipe_InvalidItemID(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	recipeCatID := insertTestRecipeCategory(t, "repo-test-recipe-category-"+uuid.NewString())
	recipeID := insertTestRecipeRow(t, userID, false)

	input := models.CreateRecipeInput{
		Name:          "repo-test-recipe-" + uuid.NewString(),
		TimeInMinutes: 10,
		Serves:        2,
		Instructions:  []string{"step"},
		Notes:         []string{},
		CategoryIDs:   []uuid.UUID{recipeCatID},
		Ingredients:   []models.Ingredient{{ItemID: uuid.New(), Quantity: 1}},
	}

	_, _, err := recipeRepo.Update(ctx, recipeID.String(), input, userID.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeInvalidItem)
}

func TestImageInUse(t *testing.T) {
	f := newImageFixture(t)
	used := testImageID()
	f.createWithImage(t, used)

	inUse, err := recipeRepo.ImageInUse(context.Background(), used)
	require.NoError(t, err)
	assert.True(t, inUse)

	inUse, err = recipeRepo.ImageInUse(context.Background(), testImageID())
	require.NoError(t, err)
	assert.False(t, inUse)
}

func TestDeleteRecipe_SharedImageDroppedConcurrently_LastOneReportsOrphan(t *testing.T) {
	ctx := context.Background()
	f := newImageFixture(t)
	shared := testImageID()
	first := f.createWithImage(t, shared)
	second := f.createWithImage(t, shared)

	held := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	type result struct {
		orphan *string
		err    error
	}
	firstDone := make(chan result, 1)
	go func() {
		var orphan *string
		err := transactor.WithinTx(ctx, func(ctx context.Context) error {
			var err error
			_, orphan, err = recipeRepo.Delete(ctx, first.ID.String(), f.userID.String(), false)
			if err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
		firstDone <- result{orphan, err}
	}()
	select {
	case <-held:
	case r := <-firstDone:
		t.Fatalf("first delete finished before holding its transaction open: %v", r.err)
	}

	secondDone := make(chan result, 1)
	go func() {
		var orphan *string
		err := transactor.WithinTx(ctx, func(ctx context.Context) error {
			var err error
			_, orphan, err = recipeRepo.Delete(ctx, second.ID.String(), f.userID.String(), false)
			return err
		})
		secondDone <- result{orphan, err}
	}()

	waitForLockWait(t)
	releaseOnce(release)

	r1 := <-firstDone
	require.NoError(t, r1.err)
	assert.Nil(t, r1.orphan, "the second recipe still used the image when the first was deleted")
	r2 := <-secondDone
	require.NoError(t, r2.err)
	require.NotNil(t, r2.orphan, "the last recipe to drop a shared image must report it, or it leaks")
	assert.Equal(t, shared, *r2.orphan)
}

func TestCheckWritable(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	other := insertTestUser(t, "Other")
	unapproved := insertTestRecipeRow(t, owner, false)
	approved := insertTestRecipeRow(t, owner, true)

	cases := []struct {
		name     string
		recipeID string
		caller   uuid.UUID
		isAdmin  bool
		wantErr  error
	}{
		{"owner on own unapproved", unapproved.String(), owner, false, nil},
		{"owner on own approved is locked", approved.String(), owner, false, models.ErrRecipeApprovedLocked},
		{"non-owner on approved is forbidden", approved.String(), other, false, models.ErrRecipeForbidden},
		{"non-owner on unapproved is hidden", unapproved.String(), other, false, models.ErrRecipeNotFound},
		{"admin on another's approved", approved.String(), other, true, nil},
		{"nonexistent recipe", uuid.NewString(), owner, false, models.ErrRecipeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := recipeRepo.CheckWritable(ctx, tc.recipeID, tc.caller.String(), tc.isAdmin)
			if tc.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestDeleteRecipe_OwnerSucceeds(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	recipeID := insertTestRecipeRow(t, userID, false)

	_, _, err := recipeRepo.Delete(ctx, recipeID.String(), userID.String(), false)
	require.NoError(t, err)
	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1`, recipeID))
}

func TestDeleteRecipe_AdminSucceedsOnAnothers(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	admin := insertTestUser(t, "Admin")
	recipeID := insertTestRecipeRow(t, owner, true)

	_, _, err := recipeRepo.Delete(ctx, recipeID.String(), admin.String(), true)
	require.NoError(t, err)
	assert.Equal(t, 0, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1`, recipeID))
}

func TestDeleteRecipe_NonOwnerNonAdminOnApproved_Forbidden(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	other := insertTestUser(t, "Other")
	recipeID := insertTestRecipeRow(t, owner, true)

	_, _, err := recipeRepo.Delete(ctx, recipeID.String(), other.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeForbidden)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1`, recipeID))
}

func TestDeleteRecipe_NonOwnerNonAdminOnUnapproved_NotFound(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	other := insertTestUser(t, "Other")
	recipeID := insertTestRecipeRow(t, owner, false)

	_, _, err := recipeRepo.Delete(ctx, recipeID.String(), other.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1`, recipeID))
}

func TestDeleteRecipe_NonexistentRecipe_NotFound(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")

	_, _, err := recipeRepo.Delete(ctx, uuid.NewString(), userID.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
}

func TestDeleteRecipe_OwnerOnApproved_Locked(t *testing.T) {
	ctx := context.Background()
	userID := insertTestUser(t, "Cook")
	recipeID := insertTestRecipeRow(t, userID, true)

	_, _, err := recipeRepo.Delete(ctx, recipeID.String(), userID.String(), false)
	assert.ErrorIs(t, err, models.ErrRecipeApprovedLocked)
	assert.Equal(t, 1, rowCount(t, `SELECT count(*) FROM recipes WHERE id = $1`, recipeID))
}

func TestMenuUserIDs_ReturnsHoldersOrderedByID(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	first := insertTestUser(t, "First")
	second := insertTestUser(t, "Second")
	bystander := insertTestUser(t, "Bystander")
	recipeID := insertTestRecipeRow(t, owner, true)
	otherRecipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(ctx, first.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(ctx, second.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(ctx, bystander.String(), otherRecipeID.String(), 2, false))

	users, err := recipeRepo.MenuUserIDs(ctx, recipeID.String())
	require.NoError(t, err)

	want := []string{first.String(), second.String()}
	sort.Strings(want)
	assert.Equal(t, want, users)
}

func TestMenuUserIDs_NoHolders_ReturnsEmpty(t *testing.T) {
	owner := insertTestUser(t, "Owner")
	recipeID := insertTestRecipeRow(t, owner, true)

	users, err := recipeRepo.MenuUserIDs(context.Background(), recipeID.String())
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestDeleteRecipe_CascadesFavouriteRows(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	recipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), recipeID.String(), false))
	require.Equal(t, 1, favouriteRowCount(t, caller, recipeID))

	_, _, err := recipeRepo.Delete(ctx, recipeID.String(), owner.String(), true)
	require.NoError(t, err)

	assert.Equal(t, 0, favouriteRowCount(t, caller, recipeID), "ON DELETE CASCADE should remove the favourite row")
}

func TestDeleteRecipe_ReturnsUsersWhoseMenusHeldIt(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	first := insertTestUser(t, "First")
	second := insertTestUser(t, "Second")
	bystander := insertTestUser(t, "Bystander")
	recipeID := insertTestRecipeRow(t, owner, true)
	otherRecipeID := insertTestRecipeRow(t, owner, true)
	require.NoError(t, menuRepo.UpsertEntry(ctx, first.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(ctx, second.String(), recipeID.String(), 2, false))
	require.NoError(t, menuRepo.UpsertEntry(ctx, bystander.String(), otherRecipeID.String(), 2, false))

	users, _, err := recipeRepo.Delete(ctx, recipeID.String(), owner.String(), true)
	require.NoError(t, err)

	want := []string{first.String(), second.String()}
	sort.Strings(want)
	assert.Equal(t, want, users, "affected users, ordered by id so concurrent deletes lock shopping lists in one order")
}

func TestDeleteRecipe_OnNoMenus_ReturnsNoUsers(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	recipeID := insertTestRecipeRow(t, owner, true)

	users, _, err := recipeRepo.Delete(ctx, recipeID.String(), owner.String(), true)
	require.NoError(t, err)
	assert.Empty(t, users)
}

func releaseOnce(release chan struct{}) {
	select {
	case <-release:
	default:
		close(release)
	}
}

// waitForLockWait blocks until some session in this database is waiting on a row lock.
func waitForLockWait(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		var n int
		err := db.DB.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`,
		).Scan(&n)
		return err == nil && n > 0
	}, 15*time.Second, 20*time.Millisecond, "no session ever blocked on a lock")
}

func TestDeleteRecipe_WaitsForInFlightMenuAddAndReturnsItsUser(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	adder := insertTestUser(t, "Adder")
	recipeID := insertTestRecipeRow(t, owner, true)

	added := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	addErr := make(chan error, 1)
	go func() {
		addErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			if err := menuRepo.UpsertEntry(ctx, adder.String(), recipeID.String(), 2, false); err != nil {
				return err
			}
			close(added)
			<-release
			return nil
		})
	}()
	select {
	case <-added:
	case err := <-addErr:
		t.Fatalf("menu add failed before holding its transaction open: %v", err)
	}

	type deleteResult struct {
		users []string
		err   error
	}
	deleted := make(chan deleteResult, 1)
	go func() {
		var users []string
		err := transactor.WithinTx(ctx, func(ctx context.Context) error {
			var err error
			users, _, err = recipeRepo.Delete(ctx, recipeID.String(), owner.String(), true)
			return err
		})
		deleted <- deleteResult{users, err}
	}()

	waitForLockWait(t)
	releaseOnce(release)

	require.NoError(t, <-addErr)
	res := <-deleted
	require.NoError(t, res.err)
	assert.Equal(t, []string{adder.String()}, res.users,
		"a menu add committed while the delete waited must be in the affected users, or its shopping list goes stale")
}

func TestUpsertEntry_RecipeDeletedWhileWaiting_ReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	adder := insertTestUser(t, "Adder")
	recipeID := insertTestRecipeRow(t, owner, true)

	deletedUncommitted := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { releaseOnce(release) })
	deleteErr := make(chan error, 1)
	go func() {
		deleteErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			if _, _, err := recipeRepo.Delete(ctx, recipeID.String(), owner.String(), true); err != nil {
				return err
			}
			close(deletedUncommitted)
			<-release
			return nil
		})
	}()
	select {
	case <-deletedUncommitted:
	case err := <-deleteErr:
		t.Fatalf("delete failed before holding its transaction open: %v", err)
	}

	addErr := make(chan error, 1)
	go func() {
		addErr <- transactor.WithinTx(ctx, func(ctx context.Context) error {
			return menuRepo.UpsertEntry(ctx, adder.String(), recipeID.String(), 2, false)
		})
	}()

	waitForLockWait(t)
	releaseOnce(release)

	require.NoError(t, <-deleteErr)
	assert.ErrorIs(t, <-addErr, models.ErrRecipeNotFound, "the recipe is gone by the time the add's FK check runs")
	assert.Equal(t, 0, menuEntryRowCount(t, adder, recipeID))
}
