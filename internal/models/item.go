package models

import (
	"time"

	"github.com/google/uuid"
)

type Item struct {
	ID             uuid.UUID   `json:"id"`
	Name           string      `json:"name"`
	CategoryID     uuid.UUID   `json:"categoryId"`
	AllowedUnitIDs []uuid.UUID `json:"allowedUnitIds"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"-"`
}
