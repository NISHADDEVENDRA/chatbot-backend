package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/gin-gonic/gin"
)

const RequestIDHeader = "X-Request-ID"

// RequestIDMiddleware adds a unique request ID to each request
// This ID is propagated through logs and responses for tracing
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if request ID already exists in header
		requestID := c.GetHeader(RequestIDHeader)
		
		// If not present, generate a new one
		if requestID == "" {
			requestID = generateRequestID()
		}
		
		// Set in context for use throughout request lifecycle
		c.Set("request_id", requestID)
		
		// Set in response header
		c.Header(RequestIDHeader, requestID)
		
		c.Next()
	}
}

// GetRequestID retrieves the request ID from context
func GetRequestID(c *gin.Context) string {
	if id, exists := c.Get("request_id"); exists {
		if str, ok := id.(string); ok {
			return str
		}
	}
	return ""
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

