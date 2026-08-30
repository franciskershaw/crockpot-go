package middleware

import "github.com/gin-gonic/gin"

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
