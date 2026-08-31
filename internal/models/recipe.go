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
