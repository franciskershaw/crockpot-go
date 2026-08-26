package handler

import "github.com/gin-gonic/gin"

func userIDFromCtx(c *gin.Context) (string, bool) {
	val, exists := c.Get("userID")
	if !exists {
		return "", false
	}
	id, ok := val.(string)
	return id, ok && id != ""
}
