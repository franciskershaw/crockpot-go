package models

import "github.com/google/uuid"

type MenuEntry struct {
	RecipeID uuid.UUID   `json:"recipeId"`
	Serves   int         `json:"serves"`
	Recipe   *RecipeCard `json:"recipe"`
}

type Menu struct {
	Entries []MenuEntry `json:"entries"`
}
