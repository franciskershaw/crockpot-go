package main

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	tref     = time.Date(2025, 7, 13, 12, 0, 0, 0, time.UTC)
	uMeatCat = uuid.MustParse("11111111-0000-0000-0000-000000000001")
	uVegCat  = uuid.MustParse("11111111-0000-0000-0000-000000000002")
	uGrams   = uuid.MustParse("22222222-0000-0000-0000-000000000001")
	uTbsp    = uuid.MustParse("22222222-0000-0000-0000-000000000002")
	uMl      = uuid.MustParse("22222222-0000-0000-0000-000000000003")
	uEasyCat = uuid.MustParse("33333333-0000-0000-0000-000000000001")
)

func resolver(byID map[oid]uuid.UUID, dropped ...oid) *refResolver {
	r := &refResolver{byMongoID: byID, dropped: map[oid]bool{}}
	for _, d := range dropped {
		r.dropped[d] = true
	}
	return r
}

func optr(o oid) *oid { return &o }

func mUser(id oid, name, email string) mongoUser {
	n := name
	return mongoUser{ID: id, Name: &n, Email: email, Role: "ADMIN",
		CreatedAt: ejsonDate{tref}, UpdatedAt: ejsonDate{tref}}
}

func mRecipe(id oid, name string, creator *oid, ings []mongoIngredient, cats []oid) mongoRecipe {
	return mongoRecipe{ID: id, Name: name, TimeInMinutes: 30, Serves: 4, Approved: true,
		Instructions: []string{"a"}, CreatedByID: creator,
		CreatedAt: ejsonDate{tref}, UpdatedAt: ejsonDate{tref},
		CategoryIDs: cats, Ingredients: ings}
}

func hasNote(notes []transformNote, kind string) bool {
	for _, n := range notes {
		if n.Kind == kind {
			return true
		}
	}
	return false
}

// --- buildUsers ---

func TestBuildUsersHappy(t *testing.T) {
	users := []mongoUser{
		mUser(realAdmins[0], "Francis Kershaw", "f@x.com"),
		mUser(realAdmins[1], "Zoe Thexton", "z@x.com"),
		mUser("6a9588ea145ec1bf75478873", "", "spam@x.com"),
	}
	subs := map[oid]string{realAdmins[0]: "SUB_F", realAdmins[1]: "SUB_Z"}
	rows, notes, err := buildUsers(users, subs, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 users, got %d", len(rows))
	}
	if len(notes) != 0 {
		t.Fatalf("notes: %+v", notes)
	}
	bySrc := map[oid]userRow{}
	for _, r := range rows {
		if r.Role != "ADMIN" {
			t.Fatalf("%s role = %q", r.Email, r.Role)
		}
		bySrc[r.SourceID] = r
	}
	f := bySrc[realAdmins[0]]
	if f.GoogleID != "SUB_F" || f.ID != objectIDToUUID(realAdmins[0]) {
		t.Fatalf("francis = %+v", f)
	}
	c := bySrc[ghostCreatorID]
	if c.GoogleID != "seed-import" || c.Email != "seed-import@crockpot.local" || c.ID != objectIDToUUID(ghostCreatorID) {
		t.Fatalf("crockpot = %+v", c)
	}
}

func TestBuildUsersMissingAdmin(t *testing.T) {
	users := []mongoUser{mUser(realAdmins[0], "Francis", "f@x.com")}
	if _, _, err := buildUsers(users, map[oid]string{realAdmins[0]: "S"}, false); err == nil {
		t.Fatal("expected an error when Zoe is absent from the export")
	}
}

func TestBuildUsersMissingSub(t *testing.T) {
	users := []mongoUser{mUser(realAdmins[0], "F", "f@x.com"), mUser(realAdmins[1], "Z", "z@x.com")}
	subs := map[oid]string{realAdmins[0]: "SUB_F"}

	if _, _, err := buildUsers(users, subs, false); err == nil {
		t.Fatal("expected an error: Zoe has no google sub and allowMissingSub is false")
	}

	rows, notes, err := buildUsers(users, subs, true)
	if err != nil {
		t.Fatalf("with allowMissingSub: %v", err)
	}
	if !hasNote(notes, noteGoogleSubPending) {
		t.Fatalf("expected a pending-sub note, got %+v", notes)
	}
	for _, r := range rows {
		if r.SourceID == realAdmins[1] && r.GoogleID != "pending:z@x.com" {
			t.Fatalf("zoe google_id = %q", r.GoogleID)
		}
	}
}

func TestBuildUsersEmailVerifiedFallback(t *testing.T) {
	f := mUser(realAdmins[0], "F", "f@x.com") // EmailVerified nil
	z := mUser(realAdmins[1], "Z", "z@x.com")
	verified := tref.Add(48 * time.Hour)
	z.EmailVerified = &ejsonDate{verified}
	subs := map[oid]string{realAdmins[0]: "S", realAdmins[1]: "S2"}

	rows, _, err := buildUsers([]mongoUser{f, z}, subs, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		switch r.SourceID {
		case realAdmins[0]:
			if r.EmailVerifiedAt == nil || !r.EmailVerifiedAt.Equal(tref) {
				t.Fatalf("francis verified = %v, want fallback to createdAt", r.EmailVerifiedAt)
			}
		case realAdmins[1]:
			if r.EmailVerifiedAt == nil || !r.EmailVerifiedAt.Equal(verified) {
				t.Fatalf("zoe verified = %v", r.EmailVerifiedAt)
			}
		}
	}
}

// --- buildItems ---

func itemDepsFixture() itemDeps {
	return itemDeps{
		categories:   resolver(map[oid]uuid.UUID{"cat1": uMeatCat}),
		units:        resolver(map[oid]uuid.UUID{"ugrams": uGrams, "utbsp": uTbsp}),
		unitIDByName: map[string]uuid.UUID{"milliliters": uMl, "tablespoons": uTbsp},
	}
}

func mItem(id oid, name, cat string, allowed ...oid) mongoItem {
	return mongoItem{ID: id, Name: name, CategoryID: oid(cat), AllowedUnitIDs: allowed,
		CreatedAt: ejsonDate{tref}, UpdatedAt: ejsonDate{tref}}
}

func TestBuildItemsHappy(t *testing.T) {
	rows, notes := buildItems([]mongoItem{mItem("item1", "Bacon", "cat1", "ugrams")}, itemDepsFixture())
	if len(rows) != 1 || len(notes) != 0 {
		t.Fatalf("rows %d notes %+v", len(rows), notes)
	}
	got := rows[0]
	if got.ID != objectIDToUUID("item1") || got.CategoryID != uMeatCat {
		t.Fatalf("item = %+v", got)
	}
	if len(got.AllowedUnitIDs) != 1 || got.AllowedUnitIDs[0] != uGrams {
		t.Fatalf("allowed = %v", got.AllowedUnitIDs)
	}
}

func TestBuildItemsUnresolvedCategorySkips(t *testing.T) {
	rows, notes := buildItems([]mongoItem{mItem("i", "Orphan", "NOPE", "ugrams")}, itemDepsFixture())
	if len(rows) != 0 {
		t.Fatalf("want the item skipped, got %+v", rows)
	}
	if !hasNote(notes, noteItemSkipped) || notes[0].Entity != "Orphan" {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildItemsAllowedSetDroppedOnUnresolvableEntry(t *testing.T) {
	rows, notes := buildItems([]mongoItem{mItem("i", "X", "cat1", "ugrams", "UNKNOWN")}, itemDepsFixture())
	if len(rows) != 1 || len(rows[0].AllowedUnitIDs) != 0 {
		t.Fatalf("want the whole set dropped, got %v", rows[0].AllowedUnitIDs)
	}
	if !hasNote(notes, noteAllowedSetDropped) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildItemsIgnoreAllowed(t *testing.T) {
	d := itemDepsFixture()
	d.ignoreAllowed = true
	rows, _ := buildItems([]mongoItem{mItem("i", "Bacon", "cat1", "ugrams")}, d)
	if len(rows[0].AllowedUnitIDs) != 0 {
		t.Fatalf("ignoreAllowed should empty every set, got %v", rows[0].AllowedUnitIDs)
	}
}

func TestBuildItemsAdditionsWidenNonEmptySet(t *testing.T) {
	rows, notes := buildItems([]mongoItem{mItem("i", "Mayonnaise", "cat1", "ugrams")}, itemDepsFixture())
	got := rows[0].AllowedUnitIDs
	if !containsUUID(got, uGrams) || !containsUUID(got, uMl) {
		t.Fatalf("want grams+ml, got %v", got)
	}
	if !hasNote(notes, noteAllowedUnitWidened) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildItemsAdditionsSkippedForUnconstrainedSet(t *testing.T) {
	rows, notes := buildItems([]mongoItem{mItem("i", "Mayonnaise", "cat1")}, itemDepsFixture())
	if len(rows[0].AllowedUnitIDs) != 0 {
		t.Fatalf("an unconstrained item must stay unconstrained, got %v", rows[0].AllowedUnitIDs)
	}
	if !hasNote(notes, noteAllowedUnitSkipped) {
		t.Fatalf("notes = %+v", notes)
	}
}

// --- buildRecipes ---

func recipeDepsFixture() recipeDeps {
	return recipeDeps{
		itemUUID: map[oid]uuid.UUID{
			"bacon": objectIDToUUID("aaaaaaaaaaaaaaaaaaaaaaaa"),
			"mayo":  objectIDToUUID("bbbbbbbbbbbbbbbbbbbbbbbb"),
		},
		itemAllowed: map[oid][]uuid.UUID{"bacon": {uGrams}},
		categories:  resolver(map[oid]uuid.UUID{"easy": uEasyCat}),
		units:       resolver(map[oid]uuid.UUID{"ugrams": uGrams, "utbsp": uTbsp}, "ujunk"),
		creators: map[oid]resolvedCreator{
			ghostCreatorID: {ID: objectIDToUUID(ghostCreatorID), Name: "Crockpot"},
			realAdmins[0]:  {ID: objectIDToUUID(realAdmins[0]), Name: "Francis Kershaw"},
		},
	}
}

func baconIng() mongoIngredient {
	return mongoIngredient{ItemID: "bacon", UnitID: optr("ugrams"), Quantity: 2}
}

func TestBuildRecipesGhostCreator(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "Tacos", &g, []mongoIngredient{baconIng()}, []oid{"easy"})
	rows, _, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if len(rows) != 1 {
		t.Fatalf("got %d recipes", len(rows))
	}
	if rows[0].CreatedByID == nil || *rows[0].CreatedByID != objectIDToUUID(ghostCreatorID) {
		t.Fatalf("createdByID = %v", rows[0].CreatedByID)
	}
	if rows[0].CreatedByName == nil || *rows[0].CreatedByName != "Crockpot" {
		t.Fatalf("createdByName = %v", rows[0].CreatedByName)
	}
}

func TestBuildRecipesRealCreator(t *testing.T) {
	r := mRecipe("r1", "X", optr(realAdmins[0]), []mongoIngredient{baconIng()}, []oid{"easy"})
	rows, _, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if rows[0].CreatedByName == nil || *rows[0].CreatedByName != "Francis Kershaw" {
		t.Fatalf("createdByName = %v", rows[0].CreatedByName)
	}
}

func TestBuildRecipesUnknownCreator(t *testing.T) {
	r := mRecipe("r1", "X", optr("cccccccccccccccccccccccc"), []mongoIngredient{baconIng()}, []oid{"easy"})
	rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if rows[0].CreatedByID != nil || rows[0].CreatedByName != nil {
		t.Fatalf("want nil creator, got %v / %v", rows[0].CreatedByID, rows[0].CreatedByName)
	}
	if !hasNote(notes, noteCreatorUnresolved) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildRecipesUnresolvedItemSkipsRecipe(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "Broken", &g, []mongoIngredient{{ItemID: "NOPE", Quantity: 1}}, []oid{"easy"})
	rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if len(rows) != 0 {
		t.Fatalf("want recipe skipped, got %+v", rows)
	}
	if !hasNote(notes, noteRecipeSkipped) || notes[0].Entity != "Broken" {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildRecipesDuplicateIngredient(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "X", &g, []mongoIngredient{
		{ItemID: "bacon", UnitID: optr("ugrams"), Quantity: 2},
		{ItemID: "bacon", UnitID: optr("ugrams"), Quantity: 5},
		{ItemID: "mayo", Quantity: 1},
	}, []oid{"easy"})
	rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	ings := rows[0].Ingredients
	if len(ings) != 2 || ings[0].Quantity != 2 || ings[0].Position != 0 || ings[1].Position != 1 {
		t.Fatalf("ingredients = %+v", ings)
	}
	if !hasNote(notes, noteDuplicateIngredient) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildRecipesUnitHandling(t *testing.T) {
	g := ghostCreatorID
	cases := []struct {
		unit oid
		note string
	}{
		{"ujunk", noteUnitBlank},       // the dropped blank unit -> quiet
		{"uNOTSEEDED", noteUnitNulled}, // genuinely unresolvable -> loud
	}
	for _, tc := range cases {
		r := mRecipe("r1", "X", &g, []mongoIngredient{{ItemID: "mayo", UnitID: optr(tc.unit), Quantity: 1}}, []oid{"easy"})
		rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
		if len(rows) != 1 || len(rows[0].Ingredients) != 1 || rows[0].Ingredients[0].UnitID != nil {
			t.Fatalf("unit %s: want one ingredient with a nil unit, got %+v", tc.unit, rows)
		}
		if !hasNote(notes, tc.note) {
			t.Fatalf("unit %s: want note %s, got %+v", tc.unit, tc.note, notes)
		}
	}
}

func TestBuildRecipesUnitNotAllowedSkipsRecipe(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "X", &g, []mongoIngredient{{ItemID: "bacon", UnitID: optr("utbsp"), Quantity: 1}}, []oid{"easy"})
	rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if len(rows) != 0 {
		t.Fatalf("bacon allows only grams; tbsp should skip the recipe, got %+v", rows)
	}
	if !hasNote(notes, noteRecipeSkipped) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildRecipesCreatedAtFallback(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "X", &g, []mongoIngredient{baconIng()}, []oid{"easy"})
	upd := tref.Add(72 * time.Hour)
	r.CreatedAt = ejsonDate{}
	r.UpdatedAt = ejsonDate{upd}
	rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if !rows[0].CreatedAt.Equal(upd) {
		t.Fatalf("createdAt = %v, want fallback to updatedAt", rows[0].CreatedAt)
	}
	if !hasNote(notes, noteCreatedAtFallback) {
		t.Fatalf("notes = %+v", notes)
	}

	r.UpdatedAt = ejsonDate{}
	rows, _, _ = buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if rows[0].CreatedAt.IsZero() {
		t.Fatal("both timestamps zero: createdAt should fall back to now, not stay zero")
	}
}

func TestBuildRecipesInstructionsNeverNil(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "X", &g, []mongoIngredient{baconIng()}, []oid{"easy"})
	r.Instructions = nil
	r.Notes = nil
	rows, _, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if rows[0].Instructions == nil || rows[0].Notes == nil {
		t.Fatalf("instructions/notes must be non-nil: %+v / %+v", rows[0].Instructions, rows[0].Notes)
	}
}

func TestBuildRecipesCategoryLinkDropped(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "X", &g, []mongoIngredient{baconIng()}, []oid{"easy", "MISSINGCAT"})
	rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if len(rows[0].CategoryIDs) != 1 || rows[0].CategoryIDs[0] != uEasyCat {
		t.Fatalf("categoryIDs = %v", rows[0].CategoryIDs)
	}
	if !hasNote(notes, noteCategoryLinkDropped) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildRecipesDuplicateCategory(t *testing.T) {
	g := ghostCreatorID
	r := mRecipe("r1", "X", &g, []mongoIngredient{baconIng()}, []oid{"easy", "easy"})
	rows, notes, _ := buildRecipes([]mongoRecipe{r}, recipeDepsFixture())
	if len(rows[0].CategoryIDs) != 1 {
		t.Fatalf("a repeated categoryId must be de-duped, got %v", rows[0].CategoryIDs)
	}
	if !hasNote(notes, noteDuplicateCategory) {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestBuildRecipesSkipTally(t *testing.T) {
	g := ghostCreatorID
	skipped := mRecipe("r1", "Broken", &g,
		[]mongoIngredient{{ItemID: "NOPE", Quantity: 1}, {ItemID: "mayo", Quantity: 1}},
		[]oid{"easy", "easy2"})
	kept := mRecipe("r2", "Fine", &g, []mongoIngredient{baconIng()}, []oid{"easy"})
	_, _, skips := buildRecipes([]mongoRecipe{skipped, kept}, recipeDepsFixture())
	if skips.ingredients != 2 || skips.categoryLinks != 2 {
		t.Fatalf("skip tally = %+v, want {2 2}", skips)
	}
}

// --- transform (integration, no DB) ---

func TestTransformAgainstFixture(t *testing.T) {
	src, err := loadSource("testdata")
	if err != nil {
		t.Fatalf("loadSource: %v", err)
	}
	in := transformInput{
		src: src,
		itemCategories: resolver(map[oid]uuid.UUID{
			"6310a880b61a0ace3a1281da": uMeatCat,
			"6310a880b61a0ace3a1281db": uVegCat,
		}),
		units: resolver(map[oid]uuid.UUID{
			"68738ad4d5730ccdb15ca142": uGrams,
			"68738ad4d5730ccdb15ca13f": uTbsp,
		}, "0000000000000000deadbeef"),
		recipeCategs: resolver(map[oid]uuid.UUID{"65d0a7ac9c6a8fd7b7a9aef3": uEasyCat}),
		unitIDByName: map[string]uuid.UUID{"milliliters": uMl, "tablespoons": uTbsp},
		accountSubs: map[oid]string{
			"68e93f253533a3d30146ba07": "SUB_F",
			"68eb87d8929571824b1516bd": "SUB_Z",
		},
	}
	res, err := transform(in)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if len(res.Users) != 3 || len(res.Items) != 2 || len(res.Recipes) != 1 {
		t.Fatalf("counts: users %d items %d recipes %d", len(res.Users), len(res.Items), len(res.Recipes))
	}
	r := res.Recipes[0]
	if r.Name != "Bean and Halloumi Tacos" {
		t.Fatalf("recipe name = %q", r.Name)
	}
	if r.CreatedByName == nil || *r.CreatedByName != "Crockpot" {
		t.Fatalf("createdByName = %v", r.CreatedByName)
	}
	if len(r.Ingredients) != 1 || r.Ingredients[0].UnitID == nil || *r.Ingredients[0].UnitID != uGrams {
		t.Fatalf("ingredient = %+v", r.Ingredients)
	}
	if !r.CreatedAt.Equal(time.Date(2025, 7, 13, 19, 52, 51, 841_000_000, time.UTC)) {
		t.Fatalf("createdAt = %v", r.CreatedAt)
	}
	if res.skipped() {
		t.Fatalf("clean fixture should not skip anything: %+v", res.Notes)
	}
}
