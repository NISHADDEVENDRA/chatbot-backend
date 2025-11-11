package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorResponse represents a standardized error response
type ErrorResponse struct {
	ErrorCode string      `json:"error_code"`
	Message   string      `json:"message"`
	Details   interface{} `json:"details,omitempty"`
}

// RespondWithError sends a standardized error response
func RespondWithError(c *gin.Context, statusCode int, errorCode, message string, details interface{}) {
	c.JSON(statusCode, ErrorResponse{
		ErrorCode: errorCode,
		Message:   message,
		Details:   details,
	})
}

// RespondWithBadRequest sends a 400 Bad Request error
func RespondWithBadRequest(c *gin.Context, message string, details interface{}) {
	RespondWithError(c, http.StatusBadRequest, "bad_request", message, details)
}

// RespondWithUnauthorized sends a 401 Unauthorized error
func RespondWithUnauthorized(c *gin.Context, message string) {
	RespondWithError(c, http.StatusUnauthorized, "unauthorized", message, nil)
}

// RespondWithForbidden sends a 403 Forbidden error
func RespondWithForbidden(c *gin.Context, message string) {
	RespondWithError(c, http.StatusForbidden, "forbidden", message, nil)
}

// RespondWithNotFound sends a 404 Not Found error
func RespondWithNotFound(c *gin.Context, message string) {
	RespondWithError(c, http.StatusNotFound, "not_found", message, nil)
}

// RespondWithInternalError sends a 500 Internal Server Error
func RespondWithInternalError(c *gin.Context, message string, details interface{}) {
	RespondWithError(c, http.StatusInternalServerError, "internal_error", message, details)
}

