package middleware

import (
	"net/http"
	"time"

	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type AuthMiddleware struct {
	config *config.Config
	rdb    *redis.Client
}

func NewAuthMiddleware(cfg *config.Config, rdb *redis.Client) *AuthMiddleware {
	return &AuthMiddleware{
		config: cfg,
		rdb:    rdb,
	}
}

func (a *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// Try to get access token from Authorization header
		authHeader := c.GetHeader("Authorization")
		var tokenString string

		if authHeader != "" {
			tokenString = utils.ExtractTokenFromHeader(authHeader)
		}

		// If no header token, try access_token cookie
		if tokenString == "" {
			if cookie, err := c.Cookie("access_token"); err == nil {
				tokenString = cookie
			}
		}

		if tokenString == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication token is required",
			})
			c.Abort()
			return
		}

		claims, err := auth.ValidateAccessToken(tokenString, a.rdb)
		if err != nil {
			// Try to auto-refresh using refresh token
			if refreshToken, err := c.Cookie("refresh_token"); err == nil && refreshToken != "" {
				refreshClaims, refreshErr := auth.ValidateRefreshToken(refreshToken, a.rdb)
				if refreshErr == nil {
					// Valid refresh token found, issue new token pair
					if err := auth.RevokeToken(refreshClaims.ID, true, a.rdb); err != nil {
						// Log error but continue
						_ = err
					}

					tokenPair, issueErr := auth.IssueTokenPair(refreshClaims.UserID, refreshClaims.ClientID, refreshClaims.Role, a.rdb)
					if issueErr == nil {
					// Set new cookies - Production-ready with environment-aware security
					secure := a.config.GinMode == "release"
					c.SetSameSite(http.SameSiteLaxMode)
					c.SetCookie(
						"access_token",
						tokenPair.AccessToken,
						int(1*time.Hour.Seconds()),
						"/",
						"",
						secure,
						true,
					)
					c.SetCookie(
						"refresh_token",
						tokenPair.RefreshToken,
						int(7*24*time.Hour.Seconds()),
						"/",
						"",
						secure,
						true,
					)

						freshClaims, valErr := auth.ValidateAccessToken(tokenPair.AccessToken, a.rdb)
						if valErr == nil {
							claims = freshClaims
						}
					}
				}
			}

			// If still no valid claims after refresh attempt
			if claims == nil {
				// Determine specific error code based on refresh token status
				var errorCode string
				var errorMessage string
				
				// Check if refresh token exists and is valid
				refreshToken, refreshErr := c.Cookie("refresh_token")
				if refreshErr != nil || refreshToken == "" {
					// No refresh token - session expired
					errorCode = "session_expired"
					errorMessage = "Your session has expired. Please log in again."
				} else {
					// Refresh token exists but refresh failed - check why
					refreshClaims, refreshValidationErr := auth.ValidateRefreshToken(refreshToken, a.rdb)
					if refreshValidationErr != nil {
						// Refresh token is invalid or expired
						errorCode = "refresh_token_expired"
						errorMessage = "Your session has expired. Please log in again."
					} else {
						// Refresh token is valid but refresh failed for other reason
						errorCode = "token_refresh_failed"
						errorMessage = "Failed to refresh session. Please log in again."
					}
					_ = refreshClaims // Avoid unused variable warning
				}
				
				c.JSON(http.StatusUnauthorized, gin.H{
					"error_code": errorCode,
					"message":    errorMessage,
					"details":    gin.H{"error": err.Error()},
				})
				c.Abort()
				return
			}
		}

		// Store user info in context
		c.Set("user_id", claims.UserID)
		c.Set("role", claims.Role)
		c.Set("client_id", claims.ClientID)
		c.Set("claims", claims)

		c.Next()
	})
}

func (a *AuthMiddleware) OptionalAuth() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// Try to get access token from Authorization header
		authHeader := c.GetHeader("Authorization")
		var tokenString string

		if authHeader != "" {
			tokenString = utils.ExtractTokenFromHeader(authHeader)
		}

		// If no header token, try access_token cookie
		if tokenString == "" {
			if cookie, err := c.Cookie("access_token"); err == nil {
				tokenString = cookie
			}
		}

		// If token exists, validate it
		if tokenString != "" {
			claims, err := auth.ValidateAccessToken(tokenString, a.rdb)
			if err == nil {
				// Store user info in context if valid
				c.Set("user_id", claims.UserID)
				c.Set("role", claims.Role)
				c.Set("client_id", claims.ClientID)
				c.Set("claims", claims)
				c.Set("authenticated", true)
			}
		}

		c.Next()
	})
}

// Helper function to check if request is authenticated
func IsAuthenticated(c *gin.Context) bool {
	_, exists := c.Get("user_id")
	return exists
}

// Helper function to get user ID from context
func GetUserID(c *gin.Context) string {
	if userID, exists := c.Get("user_id"); exists {
		if id, ok := userID.(string); ok {
			return id
		}
	}
	return ""
}

// Helper function to get role from context
func GetRole(c *gin.Context) string {
	if role, exists := c.Get("role"); exists {
		if r, ok := role.(string); ok {
			return r
		}
	}
	return ""
}

// Helper function to get client ID from context
func GetClientID(c *gin.Context) string {
	if clientID, exists := c.Get("client_id"); exists {
		if id, ok := clientID.(string); ok {
			return id
		}
	}
	return ""
}

// CORS preflight handler
func CORSPreflightHandler() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		if c.Request.Method == "OPTIONS" {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization,X-Requested-With,X-Client-ID,X-Embed-Secret,X-Refresh-Token,X-Request-Time,X-Correlation-ID,Cookie")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
