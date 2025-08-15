package middleware

import (
	"net/http"


	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
)

type AuthMiddleware struct {
	config *config.Config
}

func NewAuthMiddleware(cfg *config.Config) *AuthMiddleware {
	return &AuthMiddleware{
		config: cfg,
	}
}

func (a *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return gin.HandlerFunc(func(c *gin.Context) {
		// Try to get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		var tokenString string

		if authHeader != "" {
			tokenString = utils.ExtractTokenFromHeader(authHeader)
		}

		// If no header token, try cookie
		if tokenString == "" {
			if cookie, err := c.Cookie("auth_token"); err == nil {
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

		claims, err := utils.ValidateJWT(tokenString, a.config.JWTSecret)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Invalid or expired token",
				"details":    gin.H{"error": err.Error()},
			})
			c.Abort()
			return
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
		// Try to get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		var tokenString string

		if authHeader != "" {
			tokenString = utils.ExtractTokenFromHeader(authHeader)
		}

		// If no header token, try cookie
		if tokenString == "" {
			if cookie, err := c.Cookie("auth_token"); err == nil {
				tokenString = cookie
			}
		}

		// If token exists, validate it
		if tokenString != "" {
			claims, err := utils.ValidateJWT(tokenString, a.config.JWTSecret)
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
			c.Header("Access-Control-Allow-Headers", "Origin,Content-Type,Accept,Authorization,X-Requested-With")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})
}