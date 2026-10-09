package models

import (
	"time"

	"github.com/google/uuid"
)

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

	MatchedIngredientCount int     `json:"matchedIngredientCount"`
	TotalIngredientCount   int     `json:"totalIngredientCount"`
	MatchedCategoryCount   int     `json:"matchedCategoryCount"`
	Score                  float64 `json:"score"`
	Tier                   *string `json:"tier"`
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
	CreatedByID   *uuid.UUID           `json:"createdById"`
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
	PendingOnly        bool
	Seed               string
	CallerID           *string
	CallerIsAdmin      bool
	Page               int
	Limit              int
}

// ImageUpdate is what a recipe update does to the stored image; the zero value keeps it.
type ImageUpdate int

const (
	ImageKeep ImageUpdate = iota
	ImageReplace
	ImageRemove
)

// CreateRecipeInput is the validated payload the handler hands the repository, kept in models so neither package imports the other.
type CreateRecipeInput struct {
	Name          string
	Description   *string
	TimeInMinutes int
	Serves        int
	Instructions  []string
	Notes         []string
	CategoryIDs   []uuid.UUID
	Ingredients   []Ingredient
	ImageURL      *string
	ImageFilename *string
	Image         ImageUpdate // Update only: ImageReplace stores ImageURL/ImageFilename.
	CreatedByID   uuid.UUID
	Approved      bool
}
