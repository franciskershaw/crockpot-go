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

type recipeOpts struct {
	name        string
	createdBy   uuid.UUID
	approved    bool
	timeMinutes int
	categoryIDs []uuid.UUID
	ingredients []models.Ingredient
}

func createTestRecipe(t *testing.T, o recipeOpts) uuid.UUID {
	t.Helper()
	if o.name == "" {
		o.name = "repo-test-list-" + uuid.NewString()
	}
	if o.timeMinutes == 0 {
		o.timeMinutes = 30
	}
	rec, err := recipeRepo.Create(context.Background(), models.CreateRecipeInput{
		Name:          o.name,
		TimeInMinutes: o.timeMinutes,
		Serves:        4,
		Instructions:  []string{"step one"},
		Notes:         []string{},
		CategoryIDs:   o.categoryIDs,
		Ingredients:   o.ingredients,
		CreatedByID:   o.createdBy,
		Approved:      o.approved,
	})
	require.NoError(t, err)
	cleanupExec(t, `DELETE FROM recipes WHERE id = $1`, rec.ID)
	return rec.ID
}

func strptr(s string) *string { return &s }

func cardIDs(cards []*models.RecipeCard) []uuid.UUID {
	out := make([]uuid.UUID, len(cards))
	for i, c := range cards {
		out[i] = c.ID
	}
	return out
}

func TestListRecipes_VisibilityMatrix(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner Cook")
	other := insertTestUser(t, "Other Cook")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())

	approvedID := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
	unapprovedID := createTestRecipe(t, recipeOpts{createdBy: owner, approved: false, categoryIDs: []uuid.UUID{cat}})

	base := models.RecipeListFilter{IncludeCategoryIDs: []uuid.UUID{cat}, Page: 1, Limit: 50}

	t.Run("anonymous sees only approved", func(t *testing.T) {
		cards, total, err := recipeRepo.List(ctx, base)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, []uuid.UUID{approvedID}, cardIDs(cards))
	})

	t.Run("owner also sees their own unapproved", func(t *testing.T) {
		f := base
		f.CallerID = strptr(owner.String())
		cards, total, err := recipeRepo.List(ctx, f)
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		assert.ElementsMatch(t, []uuid.UUID{approvedID, unapprovedID}, cardIDs(cards))
	})

	t.Run("another user does not see the owner's unapproved", func(t *testing.T) {
		f := base
		f.CallerID = strptr(other.String())
		cards, total, err := recipeRepo.List(ctx, f)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Equal(t, []uuid.UUID{approvedID}, cardIDs(cards))
	})

	t.Run("admin sees all", func(t *testing.T) {
		f := base
		f.CallerID = strptr(other.String())
		f.CallerIsAdmin = true
		cards, total, err := recipeRepo.List(ctx, f)
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		assert.ElementsMatch(t, []uuid.UUID{approvedID, unapprovedID}, cardIDs(cards))
	})
}

func TestListRecipes_MineFilter(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Mine Cook")
	other := insertTestUser(t, "Not Mine Cook")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())

	mineApproved := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
	mineUnapproved := createTestRecipe(t, recipeOpts{createdBy: owner, approved: false, categoryIDs: []uuid.UUID{cat}})
	_ = createTestRecipe(t, recipeOpts{createdBy: other, approved: true, categoryIDs: []uuid.UUID{cat}})

	f := models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{cat},
		Mine:               true,
		CallerID:           strptr(owner.String()),
		Page:               1, Limit: 50,
	}
	cards, total, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.ElementsMatch(t, []uuid.UUID{mineApproved, mineUnapproved}, cardIDs(cards))

	f.CallerID = nil // anonymous ?mine=true
	cards, total, err = recipeRepo.List(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, cards)
}

func TestListRecipes_NameQueryCaseInsensitivePartial(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
	marker := "Zqx" + uuid.NewString()[:8]
	match := createTestRecipe(t, recipeOpts{name: "Slow Cooker " + marker + " Stew", createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
	_ = createTestRecipe(t, recipeOpts{name: "Unrelated " + uuid.NewString(), createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})

	cards, total, err := recipeRepo.List(ctx, models.RecipeListFilter{
		Query: marker, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, []uuid.UUID{match}, cardIDs(cards))

	cards, _, err = recipeRepo.List(ctx, models.RecipeListFilter{Query: "slow cooker " + marker, Page: 1, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{match}, cardIDs(cards), "match is case-insensitive")
}

func TestListRecipes_CategoryIncludeAndExclude(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	tagged := insertTestRecipeCategory(t, "repo-test-rc-tagged-"+uuid.NewString())
	scope := insertTestRecipeCategory(t, "repo-test-rc-scope-"+uuid.NewString())

	withTag := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{tagged, scope}})
	withoutTag := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{scope}})

	inc, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{tagged}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{withTag}, cardIDs(inc))

	exc, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{scope},
		ExcludeCategoryIDs: []uuid.UUID{tagged},
		Page:               1, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{withoutTag}, cardIDs(exc))
}

func TestListRecipes_IngredientAndUnionSemantics(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	itemCat := insertTestItemCategory(t, "repo-test-ic-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	targetItem := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	otherItem := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	scope := insertTestRecipeCategory(t, "repo-test-rc-scope-"+uuid.NewString())
	byCategory := insertTestRecipeCategory(t, "repo-test-rc-bycat-"+uuid.NewString())

	hasIngredient := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true, categoryIDs: []uuid.UUID{scope},
		ingredients: []models.Ingredient{{ItemID: targetItem, Quantity: 2}},
	})
	hasCategory := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true, categoryIDs: []uuid.UUID{scope, byCategory},
		ingredients: []models.Ingredient{{ItemID: otherItem, Quantity: 1}},
	})
	neither := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true, categoryIDs: []uuid.UUID{scope},
		ingredients: []models.Ingredient{{ItemID: otherItem, Quantity: 1}},
	})

	ing, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IngredientIDs: []uuid.UUID{targetItem}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{hasIngredient}, cardIDs(ing))

	union, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{byCategory},
		IngredientIDs:      []uuid.UUID{targetItem},
		Page:               1, Limit: 50,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []uuid.UUID{hasIngredient, hasCategory}, cardIDs(union))
	assert.NotContains(t, cardIDs(union), neither)
}

func TestListRecipes_TimeRangeIsAHardFilter(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())

	quick := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, timeMinutes: 20, categoryIDs: []uuid.UUID{cat}})
	medium := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, timeMinutes: 60, categoryIDs: []uuid.UUID{cat}})
	slow := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, timeMinutes: 200, categoryIDs: []uuid.UUID{cat}})

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{cat},
		MinTime:            30, MaxTime: 90,
		Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{medium}, cardIDs(cards))
	assert.NotContains(t, cardIDs(cards), quick)
	assert.NotContains(t, cardIDs(cards), slow)
}

func TestListRecipes_OrderNewestFirst(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())

	a := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
	b := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
	c := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})

	now := time.Now()
	setCreatedAt(t, a, now.Add(-1*time.Hour))
	setCreatedAt(t, b, now.Add(-3*time.Hour))
	setCreatedAt(t, c, now.Add(-2*time.Hour))

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{cat}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a, c, b}, cardIDs(cards))
}

func setCreatedAt(t *testing.T, id uuid.UUID, ts time.Time) {
	t.Helper()
	_, err := db.DB.Exec(context.Background(), `UPDATE recipes SET created_at = $1 WHERE id = $2`, ts, id)
	require.NoError(t, err)
}

func TestListRecipes_Pagination(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())

	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
		setCreatedAt(t, ids[i], time.Now().Add(time.Duration(-i)*time.Hour))
	}

	f := models.RecipeListFilter{IncludeCategoryIDs: []uuid.UUID{cat}, Page: 1, Limit: 2}
	p1, total, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Equal(t, []uuid.UUID{ids[0], ids[1]}, cardIDs(p1))

	f.Page = 3
	p3, total, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Equal(t, []uuid.UUID{ids[4]}, cardIDs(p3))

	f.Page = 99
	pOut, total, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Empty(t, pOut)
}

func TestListRecipes_CardCategoriesHydratedAndSorted(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	scope := insertTestRecipeCategory(t, "repo-test-rc-scope-"+uuid.NewString())
	zeta := insertTestRecipeCategory(t, "repo-test-rc-zeta-"+uuid.NewString())
	alpha := insertTestRecipeCategory(t, "repo-test-rc-alpha-"+uuid.NewString())

	// name the two so alpha sorts before zeta
	renameRecipeCategory(t, zeta, "ZZZ-"+uuid.NewString())
	renameRecipeCategory(t, alpha, "AAA-"+uuid.NewString())

	id := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{scope, zeta, alpha}})

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{scope}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	require.Len(t, cards, 1)
	require.Equal(t, id, cards[0].ID)
	require.Len(t, cards[0].Categories, 3)

	names := []string{cards[0].Categories[0].Name, cards[0].Categories[1].Name, cards[0].Categories[2].Name}
	assert.True(t, names[0] < names[1] && names[1] < names[2], "categories sorted by name, got %v", names)
	assert.NotEqual(t, uuid.Nil, cards[0].Categories[0].ID)
}

func renameRecipeCategory(t *testing.T, id uuid.UUID, name string) {
	t.Helper()
	_, err := db.DB.Exec(context.Background(), `UPDATE recipe_categories SET name = $1 WHERE id = $2`, name, id)
	require.NoError(t, err)
}

func TestListRecipes_CountMatchesListLength(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
	for i := 0; i < 3; i++ {
		createTestRecipe(t, recipeOpts{createdBy: owner, approved: i%2 == 0, categoryIDs: []uuid.UUID{cat}})
	}

	filters := []models.RecipeListFilter{
		{Page: 1, Limit: 100000},
		{IncludeCategoryIDs: []uuid.UUID{cat}, CallerID: strptr(owner.String()), Page: 1, Limit: 100000},
		{IncludeCategoryIDs: []uuid.UUID{cat}, Page: 1, Limit: 100000},
		{Query: "e", Page: 1, Limit: 100000},
	}
	for i, f := range filters {
		cards, total, err := recipeRepo.List(ctx, f)
		require.NoError(t, err)
		assert.Equal(t, total, len(cards), "filter %d: Count and List disagree", i)
	}
}

func TestGetRecipeForReader_HydratesFully(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Jamie M.")
	itemCat := insertTestItemCategory(t, "Fruit & Veg "+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemCatMeat := insertTestItemCategory(t, "Meat & Fish "+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	onions := insertTestItem(t, "onions "+uuid.NewString(), itemCat)
	beef := insertTestItem(t, "beef shin "+uuid.NewString(), itemCatMeat)
	grams := insertTestUnit(t, "grams "+uuid.NewString(), "g-"+uuid.NewString())
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())

	id := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true, timeMinutes: 360, categoryIDs: []uuid.UUID{cat},
		ingredients: []models.Ingredient{
			{ItemID: beef, UnitID: &grams, Quantity: 800},
			{ItemID: onions, Quantity: 3},
		},
	})

	got, err := recipeRepo.GetByID(ctx, id.String(), nil, false)
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, id, got.ID)
	assert.Equal(t, 360, got.TimeInMinutes)
	assert.True(t, got.Approved)
	assert.Nil(t, got.Description)
	require.NotNil(t, got.CreatedByName)
	assert.Equal(t, "Jamie M.", *got.CreatedByName)
	assert.Equal(t, owner, got.CreatedByID)
	assert.False(t, got.UpdatedAt.IsZero())
	assert.Equal(t, []string{"step one"}, got.Instructions)
	assert.Empty(t, got.Notes)

	require.Len(t, got.Categories, 1)
	assert.Equal(t, cat, got.Categories[0].ID)

	require.Len(t, got.Ingredients, 2)
	assert.Equal(t, beef, got.Ingredients[0].ItemID, "ingredients in position order")
	assert.Contains(t, got.Ingredients[0].ItemName, "beef shin")
	assert.Contains(t, got.Ingredients[0].ItemCategoryName, "Meat & Fish")
	assert.Equal(t, itemCatMeat, got.Ingredients[0].ItemCategoryID)
	require.NotNil(t, got.Ingredients[0].UnitID)
	assert.Equal(t, grams, *got.Ingredients[0].UnitID)
	require.NotNil(t, got.Ingredients[0].UnitAbbreviation)
	assert.Contains(t, *got.Ingredients[0].UnitAbbreviation, "g-")
	assert.Equal(t, 800.0, got.Ingredients[0].Quantity)

	assert.Equal(t, onions, got.Ingredients[1].ItemID)
	assert.Nil(t, got.Ingredients[1].UnitID)
	assert.Nil(t, got.Ingredients[1].UnitAbbreviation)
	assert.Equal(t, 3.0, got.Ingredients[1].Quantity)
}

func TestGetRecipeForReader_MissingReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	got, err := recipeRepo.GetByID(ctx, uuid.NewString(), nil, false)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound)
}

func TestGetRecipeForReader_MalformedIDReturnsError(t *testing.T) {
	ctx := context.Background()
	got, err := recipeRepo.GetByID(ctx, "not-a-uuid", nil, false)
	assert.Nil(t, got)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, models.ErrRecipeNotFound)
}

func TestListRecipes_IsFavouriteHydration(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	caller := insertTestUser(t, "Caller")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())

	favourited := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
	notFavourited := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})
	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), favourited.String(), false))

	f := models.RecipeListFilter{IncludeCategoryIDs: []uuid.UUID{cat}, CallerID: strptr(caller.String()), Page: 1, Limit: 50}
	cards, _, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)

	byID := make(map[uuid.UUID]bool, len(cards))
	for _, c := range cards {
		byID[c.ID] = c.IsFavourite
	}
	assert.True(t, byID[favourited])
	assert.False(t, byID[notFavourited])

	f.CallerID = nil
	anonCards, _, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)
	for _, c := range anonCards {
		assert.False(t, c.IsFavourite, "anonymous caller never sees isFavourite true")
	}
}

func TestGetRecipeForReader_HiddenRecipe(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	other := insertTestUser(t, "Other")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
	id := createTestRecipe(t, recipeOpts{createdBy: owner, approved: false, categoryIDs: []uuid.UUID{cat}})

	_, err := recipeRepo.GetByID(ctx, id.String(), strptr(other.String()), false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound, "non-owner cannot see an unapproved recipe")

	_, err = recipeRepo.GetByID(ctx, id.String(), nil, false)
	assert.ErrorIs(t, err, models.ErrRecipeNotFound, "anonymous cannot see an unapproved recipe")

	got, err := recipeRepo.GetByID(ctx, id.String(), strptr(owner.String()), false)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)

	got, err = recipeRepo.GetByID(ctx, id.String(), strptr(other.String()), true)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
}

func TestGetRecipeForReader_IsFavouriteReflectsCallerState(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Owner")
	caller := insertTestUser(t, "Caller")
	cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
	id := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat}})

	got, err := recipeRepo.GetByID(ctx, id.String(), nil, false)
	require.NoError(t, err)
	assert.False(t, got.IsFavourite, "anonymous caller never sees isFavourite true")

	got, err = recipeRepo.GetByID(ctx, id.String(), strptr(caller.String()), false)
	require.NoError(t, err)
	assert.False(t, got.IsFavourite, "authenticated caller who hasn't favourited sees false")

	require.NoError(t, recipeRepo.AddFavourite(ctx, caller.String(), id.String(), false))

	got, err = recipeRepo.GetByID(ctx, id.String(), strptr(caller.String()), false)
	require.NoError(t, err)
	assert.True(t, got.IsFavourite)
}
