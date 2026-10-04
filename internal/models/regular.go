package models

import "github.com/google/uuid"

type Regular struct {
	ID               uuid.UUID  `json:"id"`
	ItemID           uuid.UUID  `json:"itemId"`
	ItemName         string     `json:"itemName"`
	CategoryID       uuid.UUID  `json:"categoryId"`
	CategoryName     string     `json:"categoryName"`
	UnitID           *uuid.UUID `json:"unitId"`
	UnitAbbreviation *string    `json:"unitAbbreviation"`
	Quantity         float64    `json:"quantity"`
}
