package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
)

func RateLimit(store limiter.Store, rate limiter.Rate) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
