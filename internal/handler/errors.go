package handler

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// serverError responds with the generic 500 — the client never sees driver/repository error text.
func serverError(c *gin.Context) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
}

// internalError logs the wrapped error via Gin's logger, then responds with the generic 500.
func internalError(c *gin.Context, logMsg string, err error) {
	_ = c.Error(fmt.Errorf("%s: %w", logMsg, err))
	serverError(c)
}

func badRequest(c *gin.Context, code string)   { c.JSON(http.StatusBadRequest, gin.H{"error": code}) }
func notFound(c *gin.Context, code string)     { c.JSON(http.StatusNotFound, gin.H{"error": code}) }
func conflict(c *gin.Context, code string)     { c.JSON(http.StatusConflict, gin.H{"error": code}) }
func unauthorized(c *gin.Context, code string) { c.JSON(http.StatusUnauthorized, gin.H{"error": code}) }
func forbidden(c *gin.Context, code string)    { c.JSON(http.StatusForbidden, gin.H{"error": code}) }

// Retry-After carries the same seconds as the body, matching the rate limiter's 429.
func resendTooSoon(c *gin.Context, retryAfter time.Duration) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.JSON(http.StatusTooManyRequests, gin.H{"error": "resend_too_soon", "retryAfterSeconds": seconds})
}
