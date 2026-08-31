package main

import "github.com/google/uuid"

// migrationNamespace seeds every UUIDv5 the import derives from a Mongo _id.
var migrationNamespace = uuid.MustParse("a7c9e3d1-5f24-4b8a-9e60-2c1d7f4a8b03")

// ghostCreatorID is the bulk seed-import account: it owns 189 recipes and is
// absent from the User export. Its recipes are re-attributed to Crockpot.
const ghostCreatorID oid = "68740e93cd88665fea847576"

// realAdmins are the two genuine accounts promoted to ADMIN with a real Google
// login. Every other User doc is left behind this pass.
var realAdmins = []oid{
	"68e93f253533a3d30146ba07", // franciskershaw.dev@gmail.com
	"68eb87d8929571824b1516bd", // zoethexton.me@gmail.com
}

type crockpotUser struct {
	SourceID oid
	Email    string
	Name     string
	GoogleID string
}

// syntheticCrockpotUser owns the ghost seed-import recipes.
var syntheticCrockpotUser = crockpotUser{
	SourceID: ghostCreatorID,
	Email:    "seed-import@crockpot.local",
	Name:     "Crockpot",
	GoogleID: "seed-import",
}

// allowedUnitAdditions widens item allowed-unit sets to cover units that real
// recipes used (founder-reviewed). Item name -> units to add.
var allowedUnitAdditions = map[string][]string{
	"Mayonnaise":           {"milliliters"},
	"Tomatoes (Passata)":   {"cans"},
	"Tomatoes":             {"cans"},
	"Tomatoes (Cherry)":    {"cans"},
	"Stock Cube (Chicken)": {"milliliters"},
	"Mustard (Yellow)":     {"milliliters"},
	"Almonds":              {"tablespoons"},
	"Lemon":                {"tablespoons"},
	"Lime":                 {"tablespoons"},
	"Thyme (Fresh)":        {"teaspoons"},
	"Hoisin Sauce":         {"grams"},
}
