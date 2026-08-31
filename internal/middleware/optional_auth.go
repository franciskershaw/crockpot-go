package middleware

import (
	"strings"

	"github.com/franciskershaw/crockpot-go/internal/auth"
	"github.com/gin-gonic/gin"
)

// OptionalAuthMiddleware sets the same claims as AuthMiddleware for a valid token, but lets a missing or invalid one through anonymously.
func OptionalAuthMiddleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		parts := strings.Fields(c.GetHeader("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.Next()
			return
		}

		claims, err := auth.ValidateAccessToken(parts[1], secret)
		if err != nil {
			c.Next()
			return
		}

		c.Set("userID", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)
		c.Next()
	}
}
