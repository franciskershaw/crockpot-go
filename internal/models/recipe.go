package models

import (
	"time"

	"github.com/google/uuid"
)

type Recipe struct {
	ID            uuid.UUID    `json:"id"`
	Name          string       `json:"name"`
	TimeInMinutes int          `json:"timeInMinutes"`
	Serves        int          `json:"serves"`
	Instructions  []string     `json:"instructions"`
	Notes         []string     `json:"notes"`
	ImageURL      *string      `json:"imageUrl"`
	ImageFilename *string      `json:"imageFilename"`
	Approved      bool         `json:"approved"`
	CategoryIDs   []uuid.UUID  `json:"categoryIds"`
	Ingredients   []Ingredient `json:"ingredients"`
	CreatedByID   uuid.UUID    `json:"createdById"`
	CreatedByName *string      `json:"createdByName"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"-"`
}

type Ingredient struct {
	ItemID   uuid.UUID  `json:"itemId"`
	UnitID   *uuid.UUID `json:"unitId"`
	Quantity float64    `json:"quantity"`
}

type CategoryRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type RecipeCard struct {
	ID            uuid.UUID     `json:"id"`
	Name          string        `json:"name"`
	ImageURL      *string       `json:"imageUrl"`
	ImageFilename *string       `json:"imageFilename"`
	TimeInMinutes int           `json:"timeInMinutes"`
	Serves        int           `json:"serves"`
	Approved      bool          `json:"approved"`
	Categories    []CategoryRef `json:"categories"`
	CreatedAt     time.Time     `json:"createdAt"`
	IsFavourite   bool          `json:"isFavourite"`
}

type HydratedIngredient struct {
	ItemID           uuid.UUID  `json:"itemId"`
	ItemName         string     `json:"itemName"`
	ItemCategoryID   uuid.UUID  `json:"itemCategoryId"`
	ItemCategoryName string     `json:"itemCategoryName"`
	UnitID           *uuid.UUID `json:"unitId"`
	UnitAbbreviation *string    `json:"unitAbbreviation"`
	Quantity         float64    `json:"quantity"`
}

type RecipeDetail struct {
	RecipeCard
	Description   *string              `json:"description"`
	Instructions  []string             `json:"instructions"`
	Notes         []string             `json:"notes"`
	Ingredients   []HydratedIngredient `json:"ingredients"`
	CreatedByID   uuid.UUID            `json:"createdById"`
	CreatedByName *string              `json:"createdByName"`
	UpdatedAt     time.Time            `json:"updatedAt"`
}

// RecipeListFilter is the validated GET /recipes query, handler → repository.
type RecipeTimeRange struct {
	MinTime int `json:"minTime"`
	MaxTime int `json:"maxTime"`
}

type RecipeListFilter struct {
	Query              string
	IncludeCategoryIDs []uuid.UUID
	ExcludeCategoryIDs []uuid.UUID
	IngredientIDs      []uuid.UUID
	MinTime            int
	MaxTime            int
	Mine               bool
	CallerID           *string
	CallerIsAdmin      bool
	Page               int
	Limit              int
}

// CreateRecipeInput is the validated payload the handler hands the repository, kept in models so neither package imports the other.
type CreateRecipeInput struct {
	Name          string
	TimeInMinutes int
	Serves        int
	Instructions  []string
	Notes         []string
	CategoryIDs   []uuid.UUID
	Ingredients   []Ingredient
	ImageURL      *string
	ImageFilename *string
	CreatedByID   uuid.UUID
	Approved      bool
}
