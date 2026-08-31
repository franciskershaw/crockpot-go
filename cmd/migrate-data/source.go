package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type mongoItemCategory struct {
	ID   oid    `json:"_id"`
	Name string `json:"name"`
}

type mongoUnit struct {
	ID           oid    `json:"_id"`
	Name         string `json:"name"`
	Abbreviation string `json:"abbreviation"`
}

type mongoRecipeCategory struct {
	ID   oid    `json:"_id"`
	Name string `json:"name"`
}

type mongoUser struct {
	ID            oid        `json:"_id"`
	Name          *string    `json:"name"`
	Email         string     `json:"email"`
	Image         *string    `json:"image"`
	Role          string     `json:"role"`
	EmailVerified *ejsonDate `json:"emailVerified"`
	CreatedAt     ejsonDate  `json:"createdAt"`
	UpdatedAt     ejsonDate  `json:"updatedAt"`
}

type mongoAccount struct {
	UserID            oid    `json:"userId"`
	Provider          string `json:"provider"`
	ProviderAccountID string `json:"providerAccountId"`
}

type mongoItem struct {
	ID             oid       `json:"_id"`
	Name           string    `json:"name"`
	CategoryID     oid       `json:"categoryId"`
	AllowedUnitIDs []oid     `json:"allowedUnitIds"`
	CreatedAt      ejsonDate `json:"createdAt"`
	UpdatedAt      ejsonDate `json:"updatedAt"`
}

type mongoImage struct {
	URL      *string `json:"url"`
	Filename *string `json:"filename"`
}

type mongoIngredient struct {
	ItemID   oid        `json:"itemId"`
	UnitID   *oid       `json:"unitId"`
	Quantity ejsonFloat `json:"quantity"`
}

type mongoRecipe struct {
	ID            oid               `json:"_id"`
	Name          string            `json:"name"`
	TimeInMinutes ejsonInt          `json:"timeInMinutes"`
	Instructions  []string          `json:"instructions"`
	Notes         []string          `json:"notes"`
	Approved      bool              `json:"approved"`
	Serves        ejsonInt          `json:"serves"`
	CreatedByID   *oid              `json:"createdById"`
	CreatedAt     ejsonDate         `json:"createdAt"`
	UpdatedAt     ejsonDate         `json:"updatedAt"`
	CategoryIDs   []oid             `json:"categoryIds"`
	Image         *mongoImage       `json:"image"`
	Ingredients   []mongoIngredient `json:"ingredients"`
}

type source struct {
	ItemCategories   []mongoItemCategory
	Units            []mongoUnit
	RecipeCategories []mongoRecipeCategory
	Users            []mongoUser
	Accounts         []mongoAccount
	Items            []mongoItem
	Recipes          []mongoRecipe
}

func loadJSON[T any](path string) ([]T, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return out, nil
}

func loadSource(dir string) (*source, error) {
	var s source
	var err error
	if s.ItemCategories, err = loadJSON[mongoItemCategory](filepath.Join(dir, "crockpotV3.ItemCategory.json")); err != nil {
		return nil, err
	}
	if s.Units, err = loadJSON[mongoUnit](filepath.Join(dir, "crockpotV3.Unit.json")); err != nil {
		return nil, err
	}
	if s.RecipeCategories, err = loadJSON[mongoRecipeCategory](filepath.Join(dir, "crockpotV3.RecipeCategory.json")); err != nil {
		return nil, err
	}
	if s.Users, err = loadJSON[mongoUser](filepath.Join(dir, "crockpotV3.User.json")); err != nil {
		return nil, err
	}
	if s.Accounts, err = loadJSON[mongoAccount](filepath.Join(dir, "crockpotV3.Account.json")); err != nil {
		return nil, err
	}
	if s.Items, err = loadJSON[mongoItem](filepath.Join(dir, "crockpotV3.Item.json")); err != nil {
		return nil, err
	}
	if s.Recipes, err = loadJSON[mongoRecipe](filepath.Join(dir, "crockpotV3.Recipe.json")); err != nil {
		return nil, err
	}
	return &s, nil
}
