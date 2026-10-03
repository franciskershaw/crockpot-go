package middleware

import (
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// Gzip compresses responses for clients that send Accept-Encoding: gzip.
func Gzip() gin.HandlerFunc {
	return gzip.Gzip(gzip.DefaultCompression)
}
