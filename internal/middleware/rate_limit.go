package middleware

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
)

func RateLimit(store limiter.Store, rate limiter.Rate) gin.HandlerFunc {
	instance := limiter.New(store, rate)

	return mgin.NewMiddleware(instance,
		mgin.WithLimitReachedHandler(func(c *gin.Context) {
			c.Header("Retry-After", strconv.Itoa(int(rate.Period.Seconds())))
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
		}),
		mgin.WithErrorHandler(func(c *gin.Context, err error) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}),
	)
}
