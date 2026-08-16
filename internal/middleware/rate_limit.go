package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	mgin "github.com/ulule/limiter/v3/drivers/middleware/gin"
)

type RateLimitMiddleware struct {
	store limiter.Store
	rate  limiter.Rate
}

func NewRateLimitMiddleware(store limiter.Store, rate limiter.Rate) *RateLimitMiddleware {
	return &RateLimitMiddleware{store: store, rate: rate}
}

func (m *RateLimitMiddleware) Handler() gin.HandlerFunc {
	instance := limiter.New(m.store, m.rate)

	return mgin.NewMiddleware(instance,
		mgin.WithLimitReachedHandler(func(c *gin.Context) {
			c.Header("Retry-After", strconv.FormatInt(m.retryAfterSeconds(c), 10))
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
		}),
		mgin.WithErrorHandler(func(c *gin.Context, err error) {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}),
	)
}

// retryAfterSeconds reads the X-RateLimit-Reset header mgin's middleware already set (a Unix timestamp) for the non-negative seconds actually remaining, falling back to the full period if it's missing or unparseable.
func (m *RateLimitMiddleware) retryAfterSeconds(c *gin.Context) int64 {
	resetUnix, err := strconv.ParseInt(c.Writer.Header().Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return int64(m.rate.Period.Seconds())
	}
	if remaining := resetUnix - time.Now().Unix(); remaining > 0 {
		return remaining
	}
	return 0
}
