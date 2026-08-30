package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// bindJSON binds the request body JSON into target, writing a 400 response and
// returning ok=false if the body is missing or malformed.
func bindJSON(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return false
	}
	return true
}

// validateName trims and validates a name, writing the appropriate error response
// and returning ok=false if invalid.
func validateName(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name_required"})
		return "", false
	}
	if len(trimmed) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name_too_long"})
		return "", false
	}
	return trimmed, true
}

// validateIconToken trims and validates an icon token, writing the appropriate error
// response and returning ok=false if invalid.
func validateIconToken(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "icon_required"})
		return "", false
	}
	if len(trimmed) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "icon_too_long"})
		return "", false
	}
	return trimmed, true
}

// validateAbbreviation trims and validates a unit abbreviation, writing the appropriate
// error response and returning ok=false if invalid.
func validateAbbreviation(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "abbreviation_required"})
		return "", false
	}
	if len(trimmed) > 32 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "abbreviation_too_long"})
		return "", false
	}
	return trimmed, true
}

// parseID checks raw is a well-formed UUID (not that it exists — the DB confirms that), writing 400 invalid_request and returning ok=false if malformed.
func parseID(c *gin.Context, raw string) bool {
	if _, err := uuid.Parse(raw); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return false
	}
	return true
}

// validateCategoryID trims and format-checks a required category id, writing the
// appropriate error response and returning ok=false if invalid.
func validateCategoryID(c *gin.Context, raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "category_id_required"})
		return "", false
	}
	if !parseID(c, trimmed) {
		return "", false
	}
	return trimmed, true
}
