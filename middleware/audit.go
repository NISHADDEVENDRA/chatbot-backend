package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AuditMiddleware creates audit logs for all requests
func AuditMiddleware(auditor *models.AuditLogger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// Capture request body for audit (skip multipart and cap size)
		var bodyBytes []byte
		if c.Request.Body != nil {
			ct := c.Request.Header.Get("Content-Type")
			if !strings.HasPrefix(ct, "multipart/") {
				limited := io.LimitReader(c.Request.Body, 1<<20) // 1MB cap
				bodyBytes, _ = io.ReadAll(limited)
				c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}
		}

		requestID := c.GetString("request_id")
		if requestID == "" {
			requestID = uuid.NewString()
			c.Set("request_id", requestID)
		}

		c.Next()

		// Log after request completes
		event := createAuditEvent(c, bodyBytes, start, requestID)

		// Log asynchronously to not block response
		auditor.LogAsync(event)
	}
}

// createAuditEvent creates an audit event from the request context
func createAuditEvent(c *gin.Context, bodyBytes []byte, start time.Time, requestID string) *models.AuditEvent {
	event := &models.AuditEvent{
		IPAddress: c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		RequestID: requestID,
		Success:   c.Writer.Status() < 400,
		CreatedAt: time.Now(),
	}

	// Extract user information from claims
	if claims, exists := c.Get("claims"); exists {
		if cl, ok := claims.(*auth.Claims); ok {
			event.ClientID = cl.ClientID
			event.UserID = cl.UserID
		}
	}

	// Map HTTP method to action
	event.Action = mapHTTPMethodToAction(c.Request.Method)

	// Extract resource information
	event.Resource, event.ResourceID = extractResourceFromPath(c.Request.URL.Path)

	// Extract error message if any
	if !event.Success {
		event.ErrorMessage = extractErrorFromResponse(c)
	}

	// Extract changes from request body
	event.Changes = extractChangesFromBody(bodyBytes, event.Action)

	return event
}

// mapHTTPMethodToAction maps HTTP methods to audit actions
func mapHTTPMethodToAction(method string) string {
	switch method {
	case "GET":
		return "READ"
	case "POST":
		return "CREATE"
	case "PUT", "PATCH":
		return "UPDATE"
	case "DELETE":
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// extractResourceFromPath extracts resource type and ID from URL path
func extractResourceFromPath(path string) (string, string) {
	// Parse common patterns
	switch {
	case contains(path, "/api/auth/"):
		return "auth", ""
	case contains(path, "/api/client/"):
		return "client", extractIDFromPath(path)
	case contains(path, "/api/chat/"):
		return "message", extractIDFromPath(path)
	case contains(path, "/api/async/upload"):
		return "pdf", ""
	case contains(path, "/api/async/pdf/"):
		return "pdf", extractIDFromPath(path)
	case contains(path, "/api/admin/"):
		return "admin", extractIDFromPath(path)
	default:
		return "unknown", ""
	}
}

// extractIDFromPath extracts ID from URL path
func extractIDFromPath(path string) string {
	// Simple ID extraction - look for UUID-like patterns
	parts := splitPath(path)
	for _, part := range parts {
		if len(part) == 36 && contains(part, "-") {
			return part
		}
	}
	return ""
}

// extractErrorFromResponse extracts error message from response
func extractErrorFromResponse(c *gin.Context) string {
	// Try to get error from response body
	if c.Writer.Size() > 0 {
		// This is a simplified version - in production you'd parse the response
		return "HTTP " + string(rune(c.Writer.Status()))
	}
	return ""
}

// extractChangesFromBody extracts changes from request body
func extractChangesFromBody(bodyBytes []byte, action string) map[string]interface{} {
	if len(bodyBytes) == 0 || action == "READ" || action == "DELETE" {
		return nil
	}

	// Parse JSON body to extract changes
	var body map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return map[string]interface{}{
			"raw_body": string(bodyBytes),
		}
	}

	// Filter sensitive fields
	sensitiveFields := []string{"password", "token", "secret", "key"}
	filteredBody := make(map[string]interface{})

	for key, value := range body {
		if !containsSensitiveField(key, sensitiveFields) {
			filteredBody[key] = value
		} else {
			filteredBody[key] = "[REDACTED]"
		}
	}

	return filteredBody
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && contains(s[1:], substr)
}

// splitPath splits a path into parts
func splitPath(path string) []string {
	parts := make([]string, 0)
	current := ""

	for _, char := range path {
		if char == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}

	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

// containsSensitiveField checks if a field name is sensitive
func containsSensitiveField(field string, sensitiveFields []string) bool {
	fieldLower := toLowerCase(field)
	for _, sensitive := range sensitiveFields {
		if contains(fieldLower, sensitive) {
			return true
		}
	}
	return false
}

// toLowerCase converts string to lowercase
func toLowerCase(s string) string {
	result := ""
	for _, char := range s {
		if char >= 'A' && char <= 'Z' {
			result += string(char + 32)
		} else {
			result += string(char)
		}
	}
	return result
}
