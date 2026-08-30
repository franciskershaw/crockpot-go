package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// internalError attaches the real error to the Gin context — logged by gin.Default()'s
// logger — and responds with a generic message. The client never sees driver/repository error text.
func internalError(c *gin.Context, logMsg string, err error) {
	_ = c.Error(fmt.Errorf("%s: %w", logMsg, err))
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
}
