package middleware

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RateLimitMiddleware implements rate limiting using Redis
// It limits requests per IP + endpoint combination
func RateLimitMiddleware(rdb *redis.Client, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip rate limiting for health checks
		if c.FullPath() == "/health" || c.FullPath() == "/ready" {
			c.Next()
			return
		}

		// Use IP + endpoint for granular rate limiting
		key := "ratelimit:" + c.ClientIP() + ":" + c.FullPath()
		
		ctx := context.Background()
		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			// Fail open - don't block requests if Redis is down
			// Log error but continue
			if cfg.GinMode == "debug" {
				c.Set("ratelimit_error", err.Error())
			}
			c.Next()
			return
		}
		
		// Set expiration on first request
		if count == 1 {
			rdb.Expire(ctx, key, time.Duration(cfg.RateLimitWindow)*time.Second)
		}
		
		// Check limit
		if count > int64(cfg.RateLimitReqs) {
			c.Header("X-RateLimit-Limit", strconv.Itoa(cfg.RateLimitReqs))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(
				time.Now().Add(time.Duration(cfg.RateLimitWindow)*time.Second).Unix(), 10))
			
			utils.RespondWithError(c, http.StatusTooManyRequests,
				"rate_limit_exceeded",
				"Too many requests. Please try again later.",
				gin.H{
					"retry_after": cfg.RateLimitWindow,
					"limit":       cfg.RateLimitReqs,
				})
			c.Abort()
			return
		}
		
		// Set rate limit headers
		c.Header("X-RateLimit-Limit", strconv.Itoa(cfg.RateLimitReqs))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(cfg.RateLimitReqs - int(count)))
		c.Next()
	}
}

// RoleBasedRateLimit provides different limits based on user role
func RoleBasedRateLimit(rdb *redis.Client, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user role from context
		role := GetRole(c)
		
		// Determine limit based on role
		var limit int
		var window int
		
		switch role {
		case "superadmin", "admin":
			limit = cfg.RateLimitReqs * 10 // 10x for admins
			window = cfg.RateLimitWindow
		case "client":
			limit = cfg.RateLimitReqs * 2 // 2x for clients
			window = cfg.RateLimitWindow
		default:
			limit = cfg.RateLimitReqs
			window = cfg.RateLimitWindow
		}
		
		// Use role-specific key
		key := "ratelimit:" + role + ":" + c.ClientIP() + ":" + c.FullPath()
		
		ctx := context.Background()
		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			// Fail open
			c.Next()
			return
		}
		
		if count == 1 {
			rdb.Expire(ctx, key, time.Duration(window)*time.Second)
		}
		
		if count > int64(limit) {
			c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", strconv.FormatInt(
				time.Now().Add(time.Duration(window)*time.Second).Unix(), 10))
			
			utils.RespondWithError(c, http.StatusTooManyRequests,
				"rate_limit_exceeded",
				"Too many requests. Please try again later.",
				gin.H{
					"retry_after": window,
					"limit":       limit,
					"role":        role,
				})
			c.Abort()
			return
		}
		
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(limit - int(count)))
		c.Next()
	}
}

