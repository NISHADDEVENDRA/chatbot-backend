package middleware

import (
	"net/http"
	"saas-chatbot-platform/utils"
	"github.com/gin-gonic/gin"
)

// RequestSizeLimit middleware limits the size of request bodies
func RequestSizeLimit(maxSize int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check Content-Length header
		if c.Request.ContentLength > maxSize {
			utils.RespondWithError(c, http.StatusRequestEntityTooLarge,
				"request_too_large",
				"Request body exceeds maximum size",
				gin.H{
					"max_size":  maxSize,
					"received":  c.Request.ContentLength,
					"max_size_mb": maxSize / (1024 * 1024),
				})
			c.Abort()
			return
		}
		c.Next()
	}
}

