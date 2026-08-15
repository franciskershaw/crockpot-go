package middleware

import "github.com/gin-gonic/gin"

func AuthMiddleware(secret string) gin.HandlerFunc {
	return func(ctx *gin.Context) {

	}
}
