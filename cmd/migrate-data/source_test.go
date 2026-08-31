package main

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func fixture(name string) string { return filepath.Join("testdata", name) }

func TestLoadJSON(t *testing.T) {
	items, err := loadJSON[mongoItem](fixture("crockpotV3.Item.json"))
	if err != nil {
		t.Fatalf("loadJSON: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Name != "Bacon" || items[1].Name != "Mayonnaise" {
		t.Fatalf("names = %q, %q", items[0].Name, items[1].Name)
	}
	if items[0].CategoryID != "6310a880b61a0ace3a1281da" {
		t.Fatalf("Bacon.CategoryID = %q", items[0].CategoryID)
	}
	if len(items[0].AllowedUnitIDs) != 1 || items[0].AllowedUnitIDs[0] != "68738ad4d5730ccdb15ca142" {
		t.Fatalf("Bacon.AllowedUnitIDs = %v", items[0].AllowedUnitIDs)
	}
	if len(items[1].AllowedUnitIDs) != 0 {
		t.Fatalf("Mayonnaise.AllowedUnitIDs = %v, want empty", items[1].AllowedUnitIDs)
	}
}

func TestLoadJSONMissingFile(t *testing.T) {
	if _, err := loadJSON[mongoItem](fixture("nope.json")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestLoadSource(t *testing.T) {
	src, err := loadSource("testdata")
	if err != nil {
		t.Fatalf("loadSource: %v", err)
	}
	if len(src.ItemCategories) != 2 || len(src.Units) != 3 || len(src.RecipeCategories) != 2 {
		t.Fatalf("reference counts: %d/%d/%d", len(src.ItemCategories), len(src.Units), len(src.RecipeCategories))
	}
	if len(src.Users) != 3 || len(src.Accounts) != 3 || len(src.Items) != 2 || len(src.Recipes) != 1 {
		t.Fatalf("counts: users %d accounts %d items %d recipes %d",
			len(src.Users), len(src.Accounts), len(src.Items), len(src.Recipes))
	}
	r := src.Recipes[0]
	if r.TimeInMinutes != 30 || r.Serves != 4 {
		t.Fatalf("recipe scalars: time %d serves %d", r.TimeInMinutes, r.Serves)
	}
	if r.CreatedByID == nil || *r.CreatedByID != ghostCreatorID {
		t.Fatalf("recipe CreatedByID = %v", r.CreatedByID)
	}
	if len(r.Ingredients) != 1 || r.Ingredients[0].Quantity != 2 {
		t.Fatalf("recipe ingredients = %+v", r.Ingredients)
	}
	if r.Image == nil || r.Image.URL == nil || *r.Image.URL != "https://res.cloudinary.com/x/y.jpg" {
		t.Fatalf("recipe image = %+v", r.Image)
	}
	if !r.CreatedAt.Equal(r.UpdatedAt.Time) || r.CreatedAt.Year() != 2025 {
		t.Fatalf("recipe timestamps = %v / %v", r.CreatedAt.Time, r.UpdatedAt.Time)
	}
}

func TestLoadSourceMissingDir(t *testing.T) {
	if _, err := loadSource(filepath.Join("testdata", "does-not-exist")); err == nil {
		t.Fatal("expected an error for a missing source dir")
	}
}

func TestObjectIDToUUID(t *testing.T) {
	a := objectIDToUUID("6310ad7242687f4a1cf7f226")
	b := objectIDToUUID("6310ad7242687f4a1cf7f226")
	c := objectIDToUUID("6310ad7242687f4a1cf7f240")
	if a != b {
		t.Fatalf("not deterministic: %v != %v", a, b)
	}
	if a == c {
		t.Fatalf("distinct inputs collided: %v", a)
	}
	if a.Version() != 5 {
		t.Fatalf("want a v5 UUID, got version %d", a.Version())
	}
}

func TestBuildRefResolverMatches(t *testing.T) {
	seeded := map[string]uuid.UUID{
		"Meat": uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		"Veg":  uuid.MustParse("22222222-2222-2222-2222-222222222222"),
	}
	rows := []refRow{
		{ID: "6310a880b61a0ace3a1281da", Name: "Meat"},
		{ID: "6310a880b61a0ace3a1281db", Name: "Veg"},
	}
	res, misses := buildRefResolver("item category", rows, seeded)
	if len(misses) != 0 {
		t.Fatalf("misses = %+v", misses)
	}
	if res.byMongoID["6310a880b61a0ace3a1281da"] != seeded["Meat"] {
		t.Fatalf("Meat resolved to %v", res.byMongoID["6310a880b61a0ace3a1281da"])
	}
}

func TestBuildRefResolverEmptyNameDropped(t *testing.T) {
	seeded := map[string]uuid.UUID{"grams": uuid.MustParse("33333333-3333-3333-3333-333333333333")}
	rows := []refRow{
		{ID: "68738ad4d5730ccdb15ca142", Name: "grams"},
		{ID: "0000000000000000deadbeef", Name: ""},
	}
	res, misses := buildRefResolver("unit", rows, seeded)
	if len(misses) != 0 {
		t.Fatalf("an empty-name row must not be a miss: %+v", misses)
	}
	if !res.dropped["0000000000000000deadbeef"] {
		t.Fatal("empty-name row should be recorded as dropped")
	}
	if _, ok := res.byMongoID["0000000000000000deadbeef"]; ok {
		t.Fatal("empty-name row should not be in byMongoID")
	}
}

func TestBuildRefResolverCollectsAllMisses(t *testing.T) {
	seeded := map[string]uuid.UUID{"Easy": uuid.MustParse("44444444-4444-4444-4444-444444444444")}
	rows := []refRow{
		{ID: "a", Name: "Easy"},
		{ID: "b", Name: "Nonexistent Category"},
		{ID: "c", Name: "Also Missing"},
	}
	_, misses := buildRefResolver("recipe category", rows, seeded)
	if len(misses) != 2 {
		t.Fatalf("want 2 misses, got %d: %+v", len(misses), misses)
	}
	names := map[string]bool{}
	for _, m := range misses {
		names[m.Name] = true
		if m.Kind != "recipe category" {
			t.Fatalf("miss.Kind = %q", m.Kind)
		}
		if len(m.SeededNames) == 0 {
			t.Fatal("miss should carry the seeded names for context")
		}
	}
	if !names["Nonexistent Category"] || !names["Also Missing"] {
		t.Fatalf("missed names = %v", names)
	}
}

func TestAccountSubs(t *testing.T) {
	src, err := loadSource("testdata")
	if err != nil {
		t.Fatalf("loadSource: %v", err)
	}
	subs := accountSubs(src.Accounts)
	if subs["68e93f253533a3d30146ba07"] != "106417760881312091926" {
		t.Fatalf("Francis sub = %q", subs["68e93f253533a3d30146ba07"])
	}
	if subs["68eb87d8929571824b1516bd"] != "103025882889186592233" {
		t.Fatalf("Zoe sub = %q", subs["68eb87d8929571824b1516bd"])
	}
	if _, ok := subs["6a9588ea145ec1bf75478873"]; ok {
		t.Fatal("a non-google account must not appear in the sub map")
	}
}

func TestFixups(t *testing.T) {
	if !isObjectID(string(ghostCreatorID)) {
		t.Fatalf("ghostCreatorID %q is not a valid object id", ghostCreatorID)
	}
	if migrationNamespace == uuid.Nil {
		t.Fatal("migrationNamespace must not be the nil UUID")
	}
	if len(realAdmins) != 2 {
		t.Fatalf("realAdmins = %v", realAdmins)
	}
	if len(allowedUnitAdditions) != 11 {
		t.Fatalf("allowedUnitAdditions has %d entries, want 11", len(allowedUnitAdditions))
	}
	if got := allowedUnitAdditions["Mayonnaise"]; len(got) != 1 || got[0] != "milliliters" {
		t.Fatalf("Mayonnaise additions = %v", got)
	}
	if syntheticCrockpotUser.Email != "seed-import@crockpot.local" || syntheticCrockpotUser.GoogleID != "seed-import" {
		t.Fatalf("syntheticCrockpotUser = %+v", syntheticCrockpotUser)
	}
	if syntheticCrockpotUser.Name != "Crockpot" {
		t.Fatalf("syntheticCrockpotUser.Name = %q", syntheticCrockpotUser.Name)
	}
}
