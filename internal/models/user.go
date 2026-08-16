package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID              uuid.UUID  `json:"id"`
	GoogleID        *string    `json:"-"`
	PasswordHash    *string    `json:"-"`
	Email           string     `json:"email"`
	Name            *string    `json:"name"`
	Image           *string    `json:"image"`
	Role            string     `json:"role"`
	EmailVerifiedAt *time.Time `json:"-"`
	LastLoginAt     *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"-"`
}
