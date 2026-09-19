package main

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestReconcile(t *testing.T) {
	// one recipe skipped whole (3 ings, 2 cats), one surviving with a dropped
	// dupe ingredient and a dropped category link.
	src := &source{
		Items: make([]mongoItem, 5),
		Recipes: []mongoRecipe{
			{Ingredients: make([]mongoIngredient, 3), CategoryIDs: make([]oid, 2)},
			{Ingredients: make([]mongoIngredient, 4), CategoryIDs: make([]oid, 3)},
		},
	}
	res := &transformResult{
		Items:                make([]itemRow, 4),
		Recipes:              []recipeRow{{Ingredients: make([]ingredientRow, 3), CategoryIDs: make([]uuid.UUID, 2)}},
		SkippedIngredients:   3,
		SkippedCategoryLinks: 2,
		Notes: []transformNote{
			{Kind: noteItemSkipped},
			{Kind: noteRecipeSkipped},
			{Kind: noteDuplicateIngredient},
			{Kind: noteCategoryLinkDropped},
		},
	}
	persisted := map[string]int{
		"items": 4, "recipes": 1, "recipe_ingredients": 3, "recipe_categories_recipes": 2,
	}

	by := map[string]reconciliation{}
	for _, r := range reconcile(res, src, persisted) {
		by[r.Entity] = r
		if r.Source-r.Skipped != r.Built {
			t.Fatalf("%s: source-skipped (%d) != built (%d)", r.Entity, r.Source-r.Skipped, r.Built)
		}
		if r.Persisted != r.Built {
			t.Fatalf("%s: persisted %d != built %d", r.Entity, r.Persisted, r.Built)
		}
	}

	if g := by["items"]; g.Source != 5 || g.Skipped != 1 || g.Built != 4 {
		t.Fatalf("items = %+v", g)
	}
	if g := by["recipes"]; g.Source != 2 || g.Skipped != 1 || g.Built != 1 {
		t.Fatalf("recipes = %+v", g)
	}
	// 7 source ings; 3 in the skipped recipe + 1 dropped dupe = 4 skipped; 3 built
	if g := by["recipe_ingredients"]; g.Source != 7 || g.Skipped != 4 || g.Built != 3 {
		t.Fatalf("recipe_ingredients = %+v", g)
	}
	// 5 source cats; 2 in the skipped recipe + 1 dropped link = 3 skipped; 2 built
	if g := by["recipe_categories_recipes"]; g.Source != 5 || g.Skipped != 3 || g.Built != 2 {
		t.Fatalf("recipe_categories_recipes = %+v", g)
	}

	if !reconcileOK(res, src, persisted) {
		t.Fatal("reconcileOK should be true for a consistent result")
	}
}

func TestReconcileDetectsPersistedMismatch(t *testing.T) {
	src := &source{Items: make([]mongoItem, 3)}
	res := &transformResult{Items: make([]itemRow, 3)}
	if reconcileOK(res, src, map[string]int{"items": 2}) {
		t.Fatal("reconcileOK should be false when the DB has fewer rows than were built")
	}
}

func TestReconcileNilPersisted(t *testing.T) {
	src := &source{Items: make([]mongoItem, 3)}
	res := &transformResult{Items: make([]itemRow, 3)}
	for _, r := range reconcile(res, src, nil) {
		if r.Persisted != -1 {
			t.Fatalf("%s: Persisted should be -1 without a DB check, got %d", r.Entity, r.Persisted)
		}
	}
	if !reconcileOK(res, src, nil) {
		t.Fatal("reconcileOK should hold on the source/built identity alone")
	}
}

func TestNoteCounts(t *testing.T) {
	res := &transformResult{Notes: []transformNote{
		{Kind: noteDuplicateIngredient}, {Kind: noteDuplicateIngredient}, {Kind: noteUnitNulled},
	}}
	c := noteCounts(res)
	if c[noteDuplicateIngredient] != 2 || c[noteUnitNulled] != 1 {
		t.Fatalf("counts = %v", c)
	}
	if _, ok := c[noteRecipeSkipped]; ok {
		t.Fatalf("absent kinds should not appear: %v", c)
	}
}

func TestExitCode(t *testing.T) {
	if exitCode(&transformResult{}) != 0 {
		t.Fatal("clean result should exit 0")
	}
	if exitCode(&transformResult{Notes: []transformNote{{Kind: noteRecipeSkipped}}}) != 1 {
		t.Fatal("a recipe skip should exit 1")
	}
	if exitCode(&transformResult{Notes: []transformNote{{Kind: noteItemSkipped}}}) != 1 {
		t.Fatal("an item skip should exit 1")
	}
	if exitCode(&transformResult{Notes: []transformNote{{Kind: noteDuplicateIngredient}}}) != 0 {
		t.Fatal("a non-skip adjustment should still exit 0")
	}
}

func TestSummary(t *testing.T) {
	src := &source{
		Items: make([]mongoItem, 2), Users: make([]mongoUser, 42),
		Recipes: []mongoRecipe{{Ingredients: make([]mongoIngredient, 1), CategoryIDs: make([]oid, 1)}},
	}
	res := &transformResult{
		Users: make([]userRow, 3), Items: make([]itemRow, 2),
		Recipes: []recipeRow{{Ingredients: make([]ingredientRow, 1), CategoryIDs: make([]uuid.UUID, 1)}},
		Notes:   []transformNote{{Kind: noteAllowedUnitWidened, Entity: "Mayonnaise", Detail: "+milliliters"}},
	}
	s := summary(res, src, map[string]int{
		"items": 2, "recipes": 1, "recipe_ingredients": 1, "recipe_categories_recipes": 1,
	})
	for _, want := range []string{"users", "items", "recipes", "recipe_ingredients", "allowed-unit-widened", "Mayonnaise", "in-db"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary missing %q:\n%s", want, s)
		}
	}
}

func TestInflatedBaselineCount(t *testing.T) {
	day := 24 * time.Hour
	base := time.Date(2025, 9, 1, 10, 0, 0, 0, time.UTC)
	row := func(times int, span time.Duration) baselineRow {
		return baselineRow{TimesAdded: times, FirstAdded: base, LastAdded: base.Add(span)}
	}
	rows := []baselineRow{
		row(3, 2*time.Hour),     // several adds inside one day: inflated
		row(2, day-time.Minute), // just inside a day: inflated
		row(5, 40*day),          // spread out: plausible
		row(1, 0),               // added once: plausible
		row(2, day),             // exactly a day apart: plausible
	}
	if got := inflatedBaselineCount(rows); got != 2 {
		t.Fatalf("inflatedBaselineCount = %d, want 2", got)
	}
}

func TestReconcileIncludesMenuHistory(t *testing.T) {
	res := &transformResult{
		MenuHistory:   make([]baselineRow, 3),
		HistorySource: 5,
		Notes:         []transformNote{{Kind: noteHistoryRecipeMissing}, {Kind: noteHistoryDuplicate}},
	}
	var got *reconciliation
	for _, r := range reconcile(res, &source{}, map[string]int{"menu_history_baseline": 3}) {
		if r.Entity == "menu_history_baseline" {
			r := r
			got = &r
		}
	}
	if got == nil {
		t.Fatal("reconcile has no menu_history_baseline row")
	}
	if got.Source != 5 || got.Skipped != 2 || got.Built != 3 || got.Persisted != 3 {
		t.Fatalf("row = %+v, want source 5, skipped 2, built 3, persisted 3", *got)
	}
}

func TestSummaryReportsBaselineDiagnostic(t *testing.T) {
	base := time.Date(2025, 9, 1, 10, 0, 0, 0, time.UTC)
	res := &transformResult{
		MenuHistory: []baselineRow{
			{TimesAdded: 3, FirstAdded: base, LastAdded: base.Add(time.Hour)},
			{TimesAdded: 1, FirstAdded: base, LastAdded: base},
		},
		HistoryLeftBehind: 4,
	}
	out := summary(res, &source{}, nil)
	for _, want := range []string{"menu history baseline: 2 rows", "1 with times_added > 1 inside one day", "4 left behind"} {
		if !strings.Contains(out, want) {
			t.Fatalf("summary is missing %q:\n%s", want, out)
		}
	}
}

// Guards the informational status: a history skip must not turn the exit code red.
func TestHistorySkipsDoNotFailTheRun(t *testing.T) {
	res := &transformResult{Notes: []transformNote{{Kind: noteHistoryRecipeMissing}, {Kind: noteHistoryDuplicate}}}
	if code := exitCode(res); code != 0 {
		t.Fatalf("exitCode = %d, want 0", code)
	}
}
