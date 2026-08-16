package models

import (
	"time"

	"github.com/google/uuid"
)

// RefreshTokenFamily is one row per login — overwritten in place on each rotation rather than appended to.
type RefreshTokenFamily struct {
	ID                     uuid.UUID
	UserID                 uuid.UUID
	TokenHash              string
	PreviousTokenHash      *string
	PreviousTokenRotatedAt *time.Time
	ExpiresAt              time.Time
	RevokedAt              *time.Time
	CreatedAt              time.Time
}
