package middleware

import (
	"time"

	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/internal/telemetry"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware provides OpenTelemetry tracing for Gin
func TracingMiddleware() gin.HandlerFunc {
	return otelgin.Middleware("saas-chatbot-platform")
}

// EnrichTrace enriches traces with custom attributes
func EnrichTrace() gin.HandlerFunc {
	return func(c *gin.Context) {
		span := trace.SpanFromContext(c.Request.Context())

		// Add custom attributes
		if claims, exists := c.Get("claims"); exists {
			if cl, ok := claims.(*auth.Claims); ok {
				span.SetAttributes(
					attribute.String("tenant.id", cl.ClientID),
					attribute.String("user.id", cl.UserID),
					attribute.String("user.role", cl.Role),
				)
			}
		}

		// Add request attributes
		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.url", c.Request.URL.String()),
			attribute.String("http.user_agent", c.Request.UserAgent()),
			attribute.String("http.client_ip", c.ClientIP()),
		)

		c.Next()

		// Add response attributes
		span.SetAttributes(
			attribute.Int("http.response.status_code", c.Writer.Status()),
			attribute.Int("http.response.size", c.Writer.Size()),
		)
	}
}

// MetricsMiddleware records request metrics
func MetricsMiddleware(metrics *telemetry.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		// Record metrics
		duration := time.Since(start).Seconds()
		status := c.Writer.Status()
		statusStr := "success"
		if status >= 400 {
			statusStr = "error"
		}

		metrics.RecordRequest(
			c.Request.Method,
			c.Request.URL.Path,
			statusStr,
			duration,
		)
	}
}

// ManualTracing provides manual tracing utilities
func ManualTracing() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Add tracing context to request
		ctx := c.Request.Context()
		tracer := otel.Tracer("saas-chatbot-platform")
		
		// Create a span for the entire request
		ctx, span := tracer.Start(ctx, "http.request")
		defer span.End()

		// Add request ID to context (should already be set by RequestIDMiddleware)
		requestID := GetRequestID(c)
		if requestID == "" {
			// Fallback: generate if not set (shouldn't happen if RequestIDMiddleware is used)
			requestID = generateRequestID()
		}
		span.SetAttributes(attribute.String("request.id", requestID))

		// Update request context
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

