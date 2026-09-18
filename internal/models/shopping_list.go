package models

import "github.com/google/uuid"

type ShoppingListItem struct {
	ID               uuid.UUID  `json:"id"`
	ItemID           uuid.UUID  `json:"itemId"`
	ItemName         string     `json:"itemName"`
	ItemCategoryID   uuid.UUID  `json:"itemCategoryId"`
	ItemCategoryName string     `json:"itemCategoryName"`
	UnitID           *uuid.UUID `json:"unitId"`
	UnitAbbreviation *string    `json:"unitAbbreviation"`
	Quantity         float64    `json:"quantity"`
	Obtained         bool       `json:"obtained"`
	IsManual         bool       `json:"isManual"`
}

type ShoppingList struct {
	Items []ShoppingListItem `json:"items"`
}
