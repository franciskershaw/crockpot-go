package handler

import (
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// bindJSON binds the request body JSON into target, writing a 400 response and
// returning ok=false if the body is missing or malformed.
func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		badRequest(c, "invalid_request")
		return false
	}
	return true
}

// validateName trims and validates a name, writing the appropriate error response
// and returning ok=false if invalid.
func validateName(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		badRequest(c, "name_required")
		return "", false
	}
	if len(trimmed) > 100 {
		badRequest(c, "name_too_long")
		return "", false
	}
	return trimmed, true
}

// googleDisplayName applies the user-name rule to Google's name claim without rejecting it: trimmed, cut to 50 characters, "" when blank.
func googleDisplayName(raw string) string {
	name := strings.TrimSpace(raw)
	if utf8.RuneCountInString(name) > maxUserNameLength {
		name = strings.TrimSpace(string([]rune(name)[:maxUserNameLength]))
	}
	return name
}

const maxUserNameLength = 50

// validateUserName trims a person's display name and checks it is 1-50 characters, writing 400 invalid_name and returning ok=false if not.
func validateUserName(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(trimmed); n < 1 || n > maxUserNameLength {
		badRequest(c, "invalid_name")
		return "", false
	}
	return trimmed, true
}

// validateNewPassword checks a password being set is 8-72 bytes (bcrypt ignores anything past 72), writing 400 password_too_short or password_too_long and returning ok=false if not.
func validateNewPassword(c *gin.Context, password string) bool {
	if len(password) < minPasswordLength {
		badRequest(c, "password_too_short")
		return false
	}
	if len(password) > maxPasswordBytes {
		badRequest(c, "password_too_long")
		return false
	}
	return true
}

// validateIconToken trims and validates an icon token, writing the appropriate error
// response and returning ok=false if invalid.
func validateIconToken(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		badRequest(c, "icon_required")
		return "", false
	}
	if len(trimmed) > 64 {
		badRequest(c, "icon_too_long")
		return "", false
	}
	return trimmed, true
}

// validateAbbreviation trims and validates a unit abbreviation, writing the appropriate
// error response and returning ok=false if invalid.
func validateAbbreviation(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		badRequest(c, "abbreviation_required")
		return "", false
	}
	if len(trimmed) > 32 {
		badRequest(c, "abbreviation_too_long")
		return "", false
	}
	return trimmed, true
}

// validateServes checks raw is within the 1-50 serves bound, writing 400 invalid_serves and returning ok=false if out of range.
func validateServes(c *gin.Context, raw int) (int, bool) {
	if raw < 1 || raw > 50 {
		badRequest(c, "invalid_serves")
		return 0, false
	}
	return raw, true
}

// validateQuantity checks raw is present and within the 0.01-100,000 quantity bound, writing 400 invalid_quantity and returning ok=false if not.
func validateQuantity(c *gin.Context, raw *float64) (float64, bool) {
	if raw == nil || *raw < minQuantity || *raw > maxQuantity {
		badRequest(c, "invalid_quantity")
		return 0, false
	}
	return *raw, true
}

// The floor is the smallest value NUMERIC(10, 2) stores without rounding to zero.
const (
	minQuantity = 0.01
	maxQuantity = 100000
)

// parseID checks raw is a well-formed UUID (not that it exists — the DB confirms that), writing 400 invalid_request and returning ok=false if malformed.
func parseID(c *gin.Context, raw string) bool {
	if _, err := uuid.Parse(raw); err != nil {
		badRequest(c, "invalid_request")
		return false
	}
	return true
}

// validateCategoryID trims and format-checks a required category id, writing the
// appropriate error response and returning ok=false if invalid.
func validateCategoryID(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		badRequest(c, "category_id_required")
		return "", false
	}
	if !parseID(c, trimmed) {
		return "", false
	}
	return trimmed, true
}

// parseOptionalUnitID treats an absent, null or blank unitId as no unit; anything else must be a UUID.
func parseOptionalUnitID(c *gin.Context, raw *string) (*string, bool) {
	if raw == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, true
	}
	if !parseID(c, trimmed) {
		return nil, false
	}
	return &trimmed, true
}
