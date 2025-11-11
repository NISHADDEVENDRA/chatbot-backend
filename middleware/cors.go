package middleware

import (
	"context"
	"strings"
	"time"

	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/models"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func CORSMiddleware() gin.HandlerFunc {
	config := cors.Config{
		AllowOrigins:     []string{"http://localhost:3000", "http://localhost:8080", "http://127.0.0.1:3000", "http://127.0.0.1:8080"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Client-ID", "X-Embed-Secret", "X-Refresh-Token", "X-Request-Time", "X-Correlation-ID"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}

	return cors.New(config)
}

// CORSMiddlewareWithOrigins allows configurable origins without breaking existing callers
func CORSMiddlewareWithOrigins(allowedOrigins []string) gin.HandlerFunc {
	config := cors.Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization", "X-Client-ID", "X-Embed-Secret", "X-Refresh-Token", "X-Request-Time", "X-Correlation-ID"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}
	return cors.New(config)
}

// Dynamic CORS validator for embedded widgets
func EmbedCORSValidator(db *mongo.Database, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		clientID := c.GetHeader("X-Client-ID")
		embedSecret := c.GetHeader("X-Embed-Secret")

		if origin == "" || clientID == "" || embedSecret == "" {
			c.AbortWithStatusJSON(403, gin.H{
				"error_code": "invalid_embed_request",
				"message":    "Invalid embed request",
			})
			return
		}

		// Verify embed secret and allowed origins
		client, err := getClientConfig(db, clientID)
		if err != nil || client.EmbedSecret != embedSecret {
			c.AbortWithStatusJSON(403, gin.H{
				"error_code": "invalid_credentials",
				"message":    "Invalid credentials",
			})
			return
		}

		if !client.Branding.AllowEmbedding {
			c.AbortWithStatusJSON(403, gin.H{
				"error_code": "embedding_disabled",
				"message":    "Embedding disabled",
			})
			return
		}

		// Check if origin is whitelisted
		if !isOriginAllowed(origin, client.AllowedOrigins) {
			c.AbortWithStatusJSON(403, gin.H{
				"error_code": "origin_not_allowed",
				"message":    "Origin not allowed",
			})
			return
		}

		// Set CORS headers for this specific origin
		c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

		// Create visitor token with limited permissions
		visitorToken, err := auth.IssueVisitorToken(clientID, origin, rdb)
		if err != nil {
			c.AbortWithStatusJSON(500, gin.H{
				"error_code": "token_issue_failed",
				"message":    "Failed to issue visitor token",
			})
			return
		}
		c.Set("visitor_token", visitorToken)
		c.Set("is_embed", true)
		c.Set("client_id", clientID)

		c.Next()
	}
}

func getClientConfig(db *mongo.Database, clientID string) (*models.Client, error) {
	ctx := context.Background()
	clientsCollection := db.Collection("clients")

	objectID, err := primitive.ObjectIDFromHex(clientID)
	if err != nil {
		return nil, err
	}

	var client models.Client
	err = clientsCollection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&client)
	return &client, err
}

func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowed := range allowedOrigins {
		if matchOriginPattern(origin, allowed) {
			return true
		}
	}
	return false
}

// Support wildcard patterns like https://*.example.com
func matchOriginPattern(origin, pattern string) bool {
	if pattern == "*" {
		return true // Dangerous, but explicit
	}
	if strings.HasPrefix(pattern, "*.") {
		domain := strings.TrimPrefix(pattern, "*.")
		return strings.HasSuffix(origin, domain)
	}
	return origin == pattern
}

// Add endpoint for clients to manage allowed origins
func AddAllowedOrigin(c *gin.Context) {
	var req struct {
		Origin string `json:"origin" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"error_code": "invalid_request",
			"message":    "Invalid request",
		})
		return
	}

	// Validate origin format
	if !isValidOrigin(req.Origin) {
		c.JSON(400, gin.H{
			"error_code": "invalid_origin_format",
			"message":    "Invalid origin format",
		})
		return
	}

	clientID := GetClientID(c)
	if clientID == "" {
		c.JSON(401, gin.H{
			"error_code": "unauthorized",
			"message":    "Unauthorized",
		})
		return
	}

	// Get tenant database
	tenantDB, exists := c.Get("tenantDB")
	if !exists {
		c.JSON(500, gin.H{
			"error_code": "database_error",
			"message":    "Database error",
		})
		return
	}

	db := tenantDB.(*mongo.Database)
	clientsCollection := db.Collection("clients")

	objectID, err := primitive.ObjectIDFromHex(clientID)
	if err != nil {
		c.JSON(400, gin.H{
			"error_code": "invalid_client_id",
			"message":    "Invalid client ID",
		})
		return
	}

	// Add to whitelist
	_, err = clientsCollection.UpdateOne(
		c.Request.Context(),
		bson.M{"_id": objectID},
		bson.M{"$addToSet": bson.M{"allowed_origins": req.Origin}},
	)

	if err != nil {
		c.JSON(500, gin.H{
			"error_code": "update_failed",
			"message":    "Failed to update",
		})
		return
	}

	c.JSON(200, gin.H{"message": "origin added"})
}

func isValidOrigin(origin string) bool {
	// Basic validation - should start with http:// or https://
	return strings.HasPrefix(origin, "http://") || strings.HasPrefix(origin, "https://")
}

// Remove allowed origin
func RemoveAllowedOrigin(c *gin.Context) {
	var req struct {
		Origin string `json:"origin" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{
			"error_code": "invalid_request",
			"message":    "Invalid request",
		})
		return
	}

	clientID := GetClientID(c)
	if clientID == "" {
		c.JSON(401, gin.H{
			"error_code": "unauthorized",
			"message":    "Unauthorized",
		})
		return
	}

	// Get tenant database
	tenantDB, exists := c.Get("tenantDB")
	if !exists {
		c.JSON(500, gin.H{
			"error_code": "database_error",
			"message":    "Database error",
		})
		return
	}

	db := tenantDB.(*mongo.Database)
	clientsCollection := db.Collection("clients")

	objectID, err := primitive.ObjectIDFromHex(clientID)
	if err != nil {
		c.JSON(400, gin.H{
			"error_code": "invalid_client_id",
			"message":    "Invalid client ID",
		})
		return
	}

	// Remove from whitelist
	_, err = clientsCollection.UpdateOne(
		c.Request.Context(),
		bson.M{"_id": objectID},
		bson.M{"$pull": bson.M{"allowed_origins": req.Origin}},
	)

	if err != nil {
		c.JSON(500, gin.H{
			"error_code": "update_failed",
			"message":    "Failed to update",
		})
		return
	}

	c.JSON(200, gin.H{"message": "origin removed"})
}
