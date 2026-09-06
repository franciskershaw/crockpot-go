package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodySizeLimit rejects a request whose declared Content-Length exceeds maxBytes before its body is
// read, and backstops a missing/understated Content-Length by capping the actual read too.
func BodySizeLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request_too_large"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
