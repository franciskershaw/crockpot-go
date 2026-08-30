package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
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
