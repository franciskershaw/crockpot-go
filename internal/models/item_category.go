package models

import (
	"time"

	"github.com/google/uuid"
)

type ItemCategory struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Icon      string    `json:"icon"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"-"`
}
