package repository_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"sort"
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

func cardByID(cards []*models.RecipeCard, id uuid.UUID) *models.RecipeCard {
	for _, c := range cards {
		if c.ID == id {
			return c
		}
	}
	return nil
}

func TestListRecipes_IngredientCoverageCounts(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	itemCat := insertTestItemCategory(t, "repo-test-ic-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemA := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	itemB := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	itemC := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)

	recipeID := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true,
		ingredients: []models.Ingredient{
			{ItemID: itemA, Quantity: 1},
			{ItemID: itemB, Quantity: 1},
			{ItemID: itemC, Quantity: 1},
		},
	})

	t.Run("counts matched ingredients against the total on the recipe", func(t *testing.T) {
		cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
			IngredientIDs: []uuid.UUID{itemA, itemB}, Page: 1, Limit: 50,
		})
		require.NoError(t, err)
		card := cardByID(cards, recipeID)
		require.NotNil(t, card)
		assert.Equal(t, 3, card.TotalIngredientCount)
		assert.Equal(t, 2, card.MatchedIngredientCount)
	})

	t.Run("matched count is zero when no ingredients are selected", func(t *testing.T) {
		cat := insertTestRecipeCategory(t, "repo-test-rc-"+uuid.NewString())
		scoped := createTestRecipe(t, recipeOpts{
			createdBy: owner, approved: true, categoryIDs: []uuid.UUID{cat},
			ingredients: []models.Ingredient{{ItemID: itemA, Quantity: 1}, {ItemID: itemB, Quantity: 1}},
		})
		cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
			IncludeCategoryIDs: []uuid.UUID{cat}, Page: 1, Limit: 50,
		})
		require.NoError(t, err)
		card := cardByID(cards, scoped)
		require.NotNil(t, card)
		assert.Equal(t, 2, card.TotalIngredientCount)
		assert.Equal(t, 0, card.MatchedIngredientCount)
	})
}

func TestListRecipes_CategoryCoverageCounts(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	catA := insertTestRecipeCategory(t, "repo-test-rc-a-"+uuid.NewString())
	catB := insertTestRecipeCategory(t, "repo-test-rc-b-"+uuid.NewString())
	catC := insertTestRecipeCategory(t, "repo-test-rc-c-"+uuid.NewString())

	recipeID := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true,
		categoryIDs: []uuid.UUID{catA, catB, catC},
	})

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{catA, catB}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	card := cardByID(cards, recipeID)
	require.NotNil(t, card)
	assert.Equal(t, 2, card.MatchedCategoryCount)
}

func TestListRecipes_Score_BothAxesWeightedBySelectionCount(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	itemCat := insertTestItemCategory(t, "repo-test-ic-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemX := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	itemY := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	other1 := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	other2 := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	catP := insertTestRecipeCategory(t, "repo-test-rc-p-"+uuid.NewString())
	catQ := insertTestRecipeCategory(t, "repo-test-rc-q-"+uuid.NewString())

	// 4 ingredients total; of {itemX, itemY} selected, only itemX is on the recipe -> ingredientCoverage = 1/4.
	// Tagged with catP only; of {catP, catQ} selected, only catP matches -> matchedCategoryCount = 1.
	recipeID := createTestRecipe(t, recipeOpts{
		createdBy:   owner,
		approved:    true,
		categoryIDs: []uuid.UUID{catP},
		ingredients: []models.Ingredient{
			{ItemID: itemX, Quantity: 1}, {ItemID: other1, Quantity: 1}, {ItemID: other2, Quantity: 1},
		},
	})
	// pad to 4 total ingredients
	fourth := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	_, err := db.DB.Exec(ctx, `INSERT INTO recipe_ingredients (recipe_id, item_id, unit_id, quantity) VALUES ($1, $2, NULL, 1)`, recipeID, fourth)
	require.NoError(t, err)
	// registered after the item's own cleanup so it runs first (t.Cleanup is LIFO), clearing the RESTRICT FK before the item delete
	cleanupExec(t, `DELETE FROM recipe_ingredients WHERE recipe_id = $1 AND item_id = $2`, recipeID, fourth)

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IngredientIDs:      []uuid.UUID{itemX, itemY},
		IncludeCategoryIDs: []uuid.UUID{catP, catQ},
		Page:               1, Limit: 50,
	})
	require.NoError(t, err)
	card := cardByID(cards, recipeID)
	require.NotNil(t, card)
	// score = (ingredientCoverage*I + matchedCategoryCount) / (I+C) = (0.25*2 + 1) / 4 = 0.375
	assert.InDelta(t, 0.375, card.Score, 0.0001)
}

func TestListRecipes_Score_IngredientOnlyIsPlainCoverage(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	itemCat := insertTestItemCategory(t, "repo-test-ic-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	itemA := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	itemB := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	itemC := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	itemD := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)

	recipeID := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true,
		ingredients: []models.Ingredient{
			{ItemID: itemA, Quantity: 1}, {ItemID: itemB, Quantity: 1},
			{ItemID: itemC, Quantity: 1}, {ItemID: itemD, Quantity: 1},
		},
	})

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IngredientIDs: []uuid.UUID{itemA, itemB, itemC}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	card := cardByID(cards, recipeID)
	require.NotNil(t, card)
	assert.InDelta(t, 0.75, card.Score, 0.0001)
}

func TestListRecipes_Score_CategoryOnlyIsPlainCoverage(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	cat1 := insertTestRecipeCategory(t, "repo-test-rc-1-"+uuid.NewString())
	cat2 := insertTestRecipeCategory(t, "repo-test-rc-2-"+uuid.NewString())
	cat3 := insertTestRecipeCategory(t, "repo-test-rc-3-"+uuid.NewString())
	cat4 := insertTestRecipeCategory(t, "repo-test-rc-4-"+uuid.NewString())

	recipeID := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true,
		categoryIDs: []uuid.UUID{cat1, cat2, cat3, cat4},
	})

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IncludeCategoryIDs: []uuid.UUID{cat1, cat2, cat3, cat4}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	card := cardByID(cards, recipeID)
	require.NotNil(t, card)
	assert.InDelta(t, 1.0, card.Score, 0.0001)
}

func TestListRecipes_Score_NoFiltersIsZeroWithNoTier(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	marker := "Zqx" + uuid.NewString()[:8]
	recipeID := createTestRecipe(t, recipeOpts{name: "Score Marker " + marker, createdBy: owner, approved: true})

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{Query: marker, Page: 1, Limit: 50})
	require.NoError(t, err)
	card := cardByID(cards, recipeID)
	require.NotNil(t, card)
	assert.InDelta(t, 0.0, card.Score, 0.0001)
	assert.Nil(t, card.Tier)
}

func TestListRecipes_Tier_Thresholds(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")

	// newScoredRecipe tags a recipe with `matched` categories out of `total` distinct selected ones,
	// so filtering by all `total` yields a categoryCoverage of matched/total.
	newScoredRecipe := func(total, matched int) (uuid.UUID, []uuid.UUID) {
		var selected, tagged []uuid.UUID
		for i := 0; i < total; i++ {
			c := insertTestRecipeCategory(t, "repo-test-rc-tier-"+uuid.NewString())
			selected = append(selected, c)
			if i < matched {
				tagged = append(tagged, c)
			}
		}
		id := createTestRecipe(t, recipeOpts{createdBy: owner, approved: true, categoryIDs: tagged})
		return id, selected
	}

	t.Run("score of exactly 0.8 is best", func(t *testing.T) {
		recipeID, selected := newScoredRecipe(5, 4)
		cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{IncludeCategoryIDs: selected, Page: 1, Limit: 50})
		require.NoError(t, err)
		card := cardByID(cards, recipeID)
		require.NotNil(t, card)
		require.NotNil(t, card.Tier)
		assert.Equal(t, "best", *card.Tier)
	})

	t.Run("score of exactly 0.5 is good", func(t *testing.T) {
		recipeID, selected := newScoredRecipe(2, 1)
		cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{IncludeCategoryIDs: selected, Page: 1, Limit: 50})
		require.NoError(t, err)
		card := cardByID(cards, recipeID)
		require.NotNil(t, card)
		require.NotNil(t, card.Tier)
		assert.Equal(t, "good", *card.Tier)
	})

	t.Run("score just below 0.5 has no tier", func(t *testing.T) {
		recipeID, selected := newScoredRecipe(3, 1)
		cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{IncludeCategoryIDs: selected, Page: 1, Limit: 50})
		require.NoError(t, err)
		card := cardByID(cards, recipeID)
		require.NotNil(t, card)
		assert.Nil(t, card.Tier)
	})
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

// seededOrder mirrors the SQL ordering expression (md5(id::text || seed) ascending) in Go.
func seededOrder(ids []uuid.UUID, seed string) []uuid.UUID {
	type keyed struct {
		id   uuid.UUID
		hash string
	}
	pairs := make([]keyed, len(ids))
	for i, id := range ids {
		sum := md5.Sum([]byte(id.String() + seed))
		pairs[i] = keyed{id, hex.EncodeToString(sum[:])}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].hash < pairs[j].hash })
	out := make([]uuid.UUID, len(pairs))
	for i, p := range pairs {
		out[i] = p.id
	}
	return out
}

func indexOfID(ids []uuid.UUID, target uuid.UUID) int {
	for i, id := range ids {
		if id == target {
			return i
		}
	}
	return -1
}

func TestListRecipes_Ordering_ScoredModeOrdersByScoreDescending(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	itemCat := insertTestItemCategory(t, "repo-test-ic-"+uuid.NewString(), "repo-test-icon-"+uuid.NewString())
	item1 := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	item2 := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	item3 := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	extra1 := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)
	extra2 := insertTestItem(t, "repo-test-item-"+uuid.NewString(), itemCat)

	// high has coverage 1.0 (3/3), low has 1/3 (padded with unmatched extras); high is created first too.
	high := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true,
		ingredients: []models.Ingredient{{ItemID: item1, Quantity: 1}, {ItemID: item2, Quantity: 1}, {ItemID: item3, Quantity: 1}},
	})
	low := createTestRecipe(t, recipeOpts{
		createdBy: owner, approved: true,
		ingredients: []models.Ingredient{{ItemID: item1, Quantity: 1}, {ItemID: extra1, Quantity: 1}, {ItemID: extra2, Quantity: 1}},
	})

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		IngredientIDs: []uuid.UUID{item1, item2, item3}, Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	ids := cardIDs(cards)
	assert.Less(t, indexOfID(ids, high), indexOfID(ids, low), "the fuller ingredient match should rank ahead")
}

func TestListRecipes_Ordering_NameQueryOnlyKeepsPlainDefaultOrder(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	marker := "Zqx" + uuid.NewString()[:8]
	a := createTestRecipe(t, recipeOpts{name: "Plain Order A " + marker, createdBy: owner, approved: true})
	b := createTestRecipe(t, recipeOpts{name: "Plain Order B " + marker, createdBy: owner, approved: true})
	c := createTestRecipe(t, recipeOpts{name: "Plain Order C " + marker, createdBy: owner, approved: true})

	now := time.Now()
	setCreatedAt(t, a, now.Add(-1*time.Hour))
	setCreatedAt(t, b, now.Add(-3*time.Hour))
	setCreatedAt(t, c, now.Add(-2*time.Hour))

	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{
		Query: marker, Seed: "should-be-ignored-in-this-mode", Page: 1, Limit: 50,
	})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a, c, b}, cardIDs(cards))
}

func TestListRecipes_Ordering_NoFiltersUsesSeededRandomOrder(t *testing.T) {
	ctx := context.Background()
	owner := insertTestUser(t, "Cook")
	marker := "Zqx" + uuid.NewString()[:8]
	a := createTestRecipe(t, recipeOpts{name: "Seed Marker A " + marker, createdBy: owner, approved: true})
	b := createTestRecipe(t, recipeOpts{name: "Seed Marker B " + marker, createdBy: owner, approved: true})
	c := createTestRecipe(t, recipeOpts{name: "Seed Marker C " + marker, createdBy: owner, approved: true})

	seed := "test-seed-" + uuid.NewString()
	cards, _, err := recipeRepo.List(ctx, models.RecipeListFilter{Seed: seed, Page: 1, Limit: 1000})
	require.NoError(t, err)

	want := map[uuid.UUID]bool{a: true, b: true, c: true}
	var actual []uuid.UUID
	for _, card := range cards {
		if want[card.ID] {
			actual = append(actual, card.ID)
		}
	}
	expected := seededOrder([]uuid.UUID{a, b, c}, seed)
	assert.Equal(t, expected, actual)
}

func TestListRecipes_Ordering_SameSeedIsStableAcrossCalls(t *testing.T) {
	ctx := context.Background()
	seed := "stable-seed-" + uuid.NewString()
	f := models.RecipeListFilter{Seed: seed, Page: 1, Limit: 50}

	first, _, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)
	second, _, err := recipeRepo.List(ctx, f)
	require.NoError(t, err)
	assert.Equal(t, cardIDs(first), cardIDs(second))
}

func TestListRecipes_Ordering_SeededPaginationHasNoDuplicatesOrGaps(t *testing.T) {
	ctx := context.Background()
	seed := "page-seed-" + uuid.NewString()
	owner := insertTestUser(t, "Cook")
	for range 5 {
		createTestRecipe(t, recipeOpts{createdBy: owner, approved: true})
	}

	full, _, err := recipeRepo.List(ctx, models.RecipeListFilter{Seed: seed, Page: 1, Limit: 1000})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(full), 4, "the recipes seeded above must be listed")

	pageSize := len(full) / 2
	if pageSize > 10 {
		pageSize = 10
	}
	page1, _, err := recipeRepo.List(ctx, models.RecipeListFilter{Seed: seed, Page: 1, Limit: pageSize})
	require.NoError(t, err)
	page2, _, err := recipeRepo.List(ctx, models.RecipeListFilter{Seed: seed, Page: 2, Limit: pageSize})
	require.NoError(t, err)

	fullIDs := cardIDs(full)
	assert.Equal(t, fullIDs[:pageSize], cardIDs(page1))
	assert.Equal(t, fullIDs[pageSize:pageSize*2], cardIDs(page2))
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
