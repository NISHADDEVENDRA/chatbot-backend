package routes

import (
	"context"
	"net/http"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func SetupAuthRoutes(router *gin.Engine, cfg *config.Config, mongoClient *mongo.Client) {
	auth := router.Group("/auth")

	db := mongoClient.Database(cfg.DBName)
	usersCollection := db.Collection("users")

	// Register endpoint
	auth.POST("/register", func(c *gin.Context) {
		var req models.RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Check if username already exists
		var existingUser models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"username": req.Username}).Decode(&existingUser); err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error_code": "username_exists",
				"message":    "Username already exists",
			})
			return
		}

		// Hash password
		hashedPassword, err := utils.HashPassword(req.Password, cfg.BcryptCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to process password",
			})
			return
		}

		// Parse client ID if provided
		var clientID *primitive.ObjectID
		if req.ClientID != "" {
			objID, err := primitive.ObjectIDFromHex(req.ClientID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_client_id",
					"message":    "Invalid client ID format",
				})
				return
			}
			clientID = &objID
		}

		// Create user
		user := models.User{
			Username:     req.Username,
			Name:         req.Name,
			Email:        req.Email,
			Phone:        req.Phone,
			PasswordHash: hashedPassword,
			Role:         req.Role,
			ClientID:     clientID,
			TokenUsage:   0,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		result, err := usersCollection.InsertOne(context.Background(), user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to create user",
			})
			return
		}

		// Generate JWT token
		userID := result.InsertedID.(primitive.ObjectID).Hex()
		clientIDStr := ""
		if clientID != nil {
			clientIDStr = clientID.Hex()
		}

		duration, _ := time.ParseDuration(cfg.JWTExpiresIn)
		token, err := utils.GenerateJWT(userID, req.Role, clientIDStr, cfg.JWTSecret, duration)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to generate token",
			})
			return
		}

		c.JSON(http.StatusCreated, models.LoginResponse{
			Token:     token,
			ExpiresAt: time.Now().Add(duration),
			User: models.UserInfo{
				ID:       userID,
				Username: req.Username,
				Name:     req.Name,
				Email:    req.Email,
				Phone:    req.Phone,
				Role:     req.Role,
				ClientID: clientIDStr,
			},
		})
	})

	// Login endpoint
	auth.POST("/login", func(c *gin.Context) {
		var req models.LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Find user by username
		var user models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"username": req.Username}).Decode(&user); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "invalid_credentials",
				"message":    "Invalid username or password",
			})
			return
		}

		// Check password
		if !utils.CheckPassword(req.Password, user.PasswordHash) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "invalid_credentials",
				"message":    "Invalid username or password",
			})
			return
		}

		// Generate JWT token
		clientIDStr := ""
		if user.ClientID != nil {
			clientIDStr = user.ClientID.Hex()
		}

		duration, _ := time.ParseDuration(cfg.JWTExpiresIn)
		token, err := utils.GenerateJWT(user.ID.Hex(), user.Role, clientIDStr, cfg.JWTSecret, duration)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to generate token",
			})
			return
		}

		c.JSON(http.StatusOK, models.LoginResponse{
			Token:     token,
			ExpiresAt: time.Now().Add(duration),
			User: models.UserInfo{
				ID:       user.ID.Hex(),
				Username: user.Username,
				Name:     user.Name,
				Email:    user.Email,
				Phone:    user.Phone,
				Role:     user.Role,
				ClientID: clientIDStr,
			},
		})
	})

	// Refresh token endpoint
	auth.POST("/refresh", func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authorization header required",
			})
			return
		}

		tokenString := utils.ExtractTokenFromHeader(authHeader)
		duration, _ := time.ParseDuration(cfg.JWTExpiresIn)
		newToken, err := utils.RefreshJWT(tokenString, cfg.JWTSecret, duration)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Failed to refresh token",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"token":      newToken,
			"expires_at": time.Now().Add(duration),
		})
	})
}
