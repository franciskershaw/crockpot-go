package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS allows cross-origin requests only from allowedOrigin, with credentials.
func CORS(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// The Allow-Origin header below is emitted conditionally on the request Origin,
		// so every response through here varies by it — keep caches from crossing origins.
		c.Writer.Header().Add("Vary", "Origin")

		origin := c.Request.Header.Get("Origin")
		if origin != allowedOrigin {
			c.Next()
			return
		}

		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == http.MethodOptions {
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
