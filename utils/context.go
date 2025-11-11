package utils

import (
	"context"
	"time"
)

const (
	// DefaultTimeout is the default timeout for most database operations
	DefaultTimeout = 10 * time.Second
	
	// LongTimeout is for operations that may take longer (file uploads, etc.)
	LongTimeout = 30 * time.Second
	
	// ShortTimeout is for quick operations (cache lookups, etc.)
	ShortTimeout = 2 * time.Second
)

// WithTimeout creates a context with default timeout
func WithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, DefaultTimeout)
}

// WithLongTimeout creates a context with long timeout for operations that may take longer
func WithLongTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, LongTimeout)
}

// WithShortTimeout creates a context with short timeout for quick operations
func WithShortTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, ShortTimeout)
}

// WithCustomTimeout creates a context with custom timeout duration
func WithCustomTimeout(parent context.Context, duration time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, duration)
}

