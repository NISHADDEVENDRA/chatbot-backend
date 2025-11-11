package routes

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/services"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func SetupAuthRoutes(router *gin.Engine, cfg *config.Config, mongoClient *mongo.Client, rdb *redis.Client) {
	authGroup := router.Group("/auth")

	// Import auth middleware
	authMiddleware := middleware.NewAuthMiddleware(cfg, rdb)
	
	// Determine cookie security based on environment
	secure := cfg.GinMode == "release"

	db := mongoClient.Database(cfg.DBName)
	usersCollection := db.Collection("users")
	passwordResetsCollection := db.Collection("password_resets")

	// Register endpoint
	authGroup.POST("/register", func(c *gin.Context) {
		var req models.RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"validation_error": err.Error()},
			})
			return
		}

		// Check if username already exists
		ctx, cancel := utils.WithTimeout(context.Background())
		defer cancel()
		var existingUser models.User
		if err := usersCollection.FindOne(ctx, bson.M{"username": req.Username}).Decode(&existingUser); err == nil {
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

		userRole := "visitor"

		// Create user
		user := models.User{
			Username:     req.Username,
			Name:         req.Name,
			Email:        req.Email,
			Phone:        req.Phone,
			PasswordHash: hashedPassword,
			Role:         userRole,
			ClientID:     clientID,
			TokenUsage:   0,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		result, err := usersCollection.InsertOne(ctx, user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to create user",
			})
			return
		}

		// Generate secure token pair
		userID := result.InsertedID.(primitive.ObjectID).Hex()
		clientIDStr := ""
		if clientID != nil {
			clientIDStr = clientID.Hex()
		}

		tokenPair, err := auth.IssueTokenPair(userID, clientIDStr, userRole, rdb)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to generate tokens",
			})
			return
		}

		// Set access token cookie
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(
			"access_token",             // name
			tokenPair.AccessToken,      // value
			int(1*time.Hour.Seconds()), // maxAge in seconds (1 hour)
			"/",                        // path
			"",                         // domain (empty for localhost)
			secure,                     // secure (true in production, false in development)
			true,                       // httpOnly (true for security)
		)

		// Set refresh token cookie
		c.SetCookie(
			"refresh_token",               // name
			tokenPair.RefreshToken,        // value
			int(7*24*time.Hour.Seconds()), // maxAge in seconds (7 days)
			"/",                           // path
			"",                            // domain (empty for localhost)
			secure,                        // secure (true in production, false in development)
			true,                          // httpOnly (true for security)
		)

		c.JSON(http.StatusCreated, models.TokenPairResponse{
			AccessToken:  tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			AccessExp:    tokenPair.AccessExp,
			RefreshExp:   tokenPair.RefreshExp,
			User: models.UserInfo{
				ID:       userID,
				Username: req.Username,
				Name:     req.Name,
				Email:    req.Email,
				Phone:    req.Phone,
				Role:     userRole,
				ClientID: clientIDStr,
			},
		})
	})

	// Login endpoint
	authGroup.POST("/login", func(c *gin.Context) {
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
		ctx, cancel := utils.WithTimeout(context.Background())
		defer cancel()
		var user models.User
		if err := usersCollection.FindOne(ctx, bson.M{"username": req.Username}).Decode(&user); err != nil {
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

		// Generate secure token pair
		clientIDStr := ""
		if user.ClientID != nil {
			clientIDStr = user.ClientID.Hex()
		}

		tokenPair, err := auth.IssueTokenPair(user.ID.Hex(), clientIDStr, user.Role, rdb)
		if err != nil {
			log.Printf("❌ Token generation failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to generate tokens",
			})
			return
		}

		// Set access token cookie
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(
			"access_token",             // name
			tokenPair.AccessToken,      // value
			int(1*time.Hour.Seconds()), // maxAge in seconds (1 hour)
			"/",                        // path
			"",                         // domain (empty for localhost)
			secure,                     // secure (true in production, false in development)
			true,                       // httpOnly (true for security)
		)

		// Set refresh token cookie
		c.SetCookie(
			"refresh_token",               // name
			tokenPair.RefreshToken,        // value
			int(7*24*time.Hour.Seconds()), // maxAge in seconds (7 days)
			"/",                           // path
			"",                            // domain (empty for localhost)
			secure,                        // secure (true in production, false in development)
			true,                          // httpOnly (true for security)
		)

		c.JSON(http.StatusOK, models.TokenPairResponse{
			AccessToken:  tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			AccessExp:    tokenPair.AccessExp,
			RefreshExp:   tokenPair.RefreshExp,
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
	authGroup.POST("/refresh", func(c *gin.Context) {
		refreshToken := c.GetHeader("X-Refresh-Token")
		if refreshToken == "" {
			// Try to get from cookie
			if cookie, err := c.Cookie("refresh_token"); err == nil {
				refreshToken = cookie
			} else {
				c.JSON(http.StatusUnauthorized, gin.H{
					"error_code": "unauthorized",
					"message":    "Refresh token required",
				})
				return
			}
		}

		claims, err := auth.ValidateRefreshToken(refreshToken, rdb)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Invalid refresh token",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Revoke old refresh token (refresh token rotation)
		auth.RevokeToken(claims.ID, true, rdb)

		// Issue new token pair
		tokenPair, err := auth.IssueTokenPair(claims.UserID, claims.ClientID, claims.Role, rdb)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to issue tokens",
			})
			return
		}

		// Set new access token cookie
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(
			"access_token",             // name
			tokenPair.AccessToken,      // value
			int(1*time.Hour.Seconds()), // maxAge in seconds (1 hour)
			"/",                        // path
			"",                         // domain (empty for localhost)
			secure,                     // secure (true in production, false in development)
			true,                       // httpOnly (true for security)
		)

		// Set new refresh token cookie
		c.SetCookie(
			"refresh_token",               // name
			tokenPair.RefreshToken,        // value
			int(7*24*time.Hour.Seconds()), // maxAge in seconds (7 days)
			"/",                           // path
			"",                            // domain (empty for localhost)
			secure,                        // secure (true in production, false in development)
			true,                          // httpOnly (true for security)
		)

		c.JSON(http.StatusOK, gin.H{
			"access_token":  tokenPair.AccessToken,
			"refresh_token": tokenPair.RefreshToken,
			"access_exp":    tokenPair.AccessExp,
			"refresh_exp":   tokenPair.RefreshExp,
		})
	})

	// Logout endpoint
	authGroup.POST("/logout", func(c *gin.Context) {
		// Get access token from header or cookie
		accessToken := c.GetHeader("Authorization")
		if accessToken == "" {
			if cookie, err := c.Cookie("access_token"); err == nil {
				accessToken = cookie
			}
		} else {
			accessToken = utils.ExtractTokenFromHeader(accessToken)
		}

		// Revoke access token if found
		if accessToken != "" {
			claims, err := auth.ValidateAccessToken(accessToken, rdb)
			if err == nil {
				auth.RevokeToken(claims.ID, false, rdb)
			}
		}

		// Clear cookies
		c.SetCookie("access_token", "", -1, "/", "", secure, true)
		c.SetCookie("refresh_token", "", -1, "/", "", secure, true)

		c.JSON(http.StatusOK, gin.H{
			"message": "Logged out successfully",
		})
	})

	// Logout all sessions endpoint
	authGroup.POST("/logout-all", func(c *gin.Context) {
		// Get user ID from context (set by auth middleware)
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication required",
			})
			return
		}

		// Revoke all tokens for this user
		err := auth.RevokeAllUserTokens(userID.(string), rdb)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to revoke tokens",
			})
			return
		}

		// Clear cookies
		c.SetCookie("access_token", "", -1, "/", "", secure, true)
		c.SetCookie("refresh_token", "", -1, "/", "", secure, true)

		c.JSON(http.StatusOK, gin.H{
			"message": "All sessions logged out successfully",
		})
	})

	// Check if email exists endpoint (for authenticated users)
	authGroup.GET("/check-email", authMiddleware.RequireAuth(), func(c *gin.Context) {
		email := c.Query("email")
		excludeUserID := c.Query("exclude_user_id")
		if email == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Email parameter is required",
			})
			return
		}

		// Build query to check if email exists, optionally excluding current user
		query := bson.M{"email": email}
		if excludeUserID != "" {
			excludeObjID, err := primitive.ObjectIDFromHex(excludeUserID)
			if err == nil {
				query["_id"] = bson.M{"$ne": excludeObjID}
			}
		}

		// Check if email exists in users collection
		var existingUser models.User
		err := usersCollection.FindOne(context.Background(), query).Decode(&existingUser)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				// Email does not exist
				c.JSON(http.StatusOK, gin.H{
					"exists": false,
					"message": "Email is available",
				})
				return
			}
			// Database error
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to check email",
			})
			return
		}

		// Email exists
		c.JSON(http.StatusOK, gin.H{
			"exists": true,
			"message": "Email already exists",
		})
	})

	// Check if username exists endpoint (for authenticated users)
	authGroup.GET("/check-username", authMiddleware.RequireAuth(), func(c *gin.Context) {
		username := c.Query("username")
		excludeUserID := c.Query("exclude_user_id")
		if username == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Username parameter is required",
			})
			return
		}

		// Build query to check if username exists, optionally excluding current user
		query := bson.M{"username": username}
		if excludeUserID != "" {
			excludeObjID, err := primitive.ObjectIDFromHex(excludeUserID)
			if err == nil {
				query["_id"] = bson.M{"$ne": excludeObjID}
			}
		}

		// Check if username exists in users collection
		var existingUser models.User
		err := usersCollection.FindOne(context.Background(), query).Decode(&existingUser)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				// Username does not exist
				c.JSON(http.StatusOK, gin.H{
					"exists": false,
					"message": "Username is available",
				})
				return
			}
			// Database error
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to check username",
			})
			return
		}

		// Username exists
		c.JSON(http.StatusOK, gin.H{
			"exists": true,
			"message": "Username already exists",
		})
	})

	// Get user profile endpoint
	authGroup.GET("/profile", authMiddleware.RequireAuth(), func(c *gin.Context) {
		// Get user ID from context (set by auth middleware)
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication required",
			})
			return
		}

		// Convert string ID to ObjectID
		objectID, err := primitive.ObjectIDFromHex(userID.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
			})
			return
		}

		// Find user by ID
		var user models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"_id": objectID}).Decode(&user); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		// Return user profile (excluding password hash)
		c.JSON(http.StatusOK, gin.H{
			"id":         user.ID.Hex(),
			"username":   user.Username,
			"name":       user.Name,
			"email":      user.Email,
			"phone":      user.Phone,
			"avatar_url": user.AvatarURL,
			"role":       user.Role,
			"client_id":  user.ClientID,
			"created_at": user.CreatedAt,
			"updated_at": user.UpdatedAt,
		})
	})

	// Update user profile endpoint
	authGroup.PATCH("/profile", authMiddleware.RequireAuth(), func(c *gin.Context) {
		// Get user ID from context (set by auth middleware)
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication required",
			})
			return
		}

		// Convert string ID to ObjectID
		objectID, err := primitive.ObjectIDFromHex(userID.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
			})
			return
		}

		var updateData struct {
			Username string `json:"username,omitempty"`
			Name     string `json:"name,omitempty"`
			Email    string `json:"email,omitempty"`
			Phone    string `json:"phone,omitempty"`
		}

		if err := c.ShouldBindJSON(&updateData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_data",
				"message":    "Invalid request data",
			})
			return
		}

		// Check if username is being changed and if it already exists
		if updateData.Username != "" {
			var existingUser models.User
			if err := usersCollection.FindOne(context.Background(), bson.M{"username": updateData.Username, "_id": bson.M{"$ne": objectID}}).Decode(&existingUser); err == nil {
				c.JSON(http.StatusConflict, gin.H{
					"error_code": "username_exists",
					"message":    "Username already exists",
				})
				return
			}
		}

		// Check if email is being changed and if it already exists
		if updateData.Email != "" {
			var existingUser models.User
			if err := usersCollection.FindOne(context.Background(), bson.M{"email": updateData.Email, "_id": bson.M{"$ne": objectID}}).Decode(&existingUser); err == nil {
				c.JSON(http.StatusConflict, gin.H{
					"error_code": "email_exists",
					"message":    "Email already exists",
				})
				return
			}
		}

		// Prepare update document
		update := bson.M{"updated_at": time.Now()}
		if updateData.Username != "" {
			update["username"] = updateData.Username
		}
		if updateData.Name != "" {
			update["name"] = updateData.Name
		}
		if updateData.Email != "" {
			update["email"] = updateData.Email
		}
		if updateData.Phone != "" {
			update["phone"] = updateData.Phone
		}

		// Update user
		_, err = usersCollection.UpdateOne(context.Background(), bson.M{"_id": objectID}, bson.M{"$set": update})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to update profile",
			})
			return
		}

		// Fetch updated user
		var updatedUser models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"_id": objectID}).Decode(&updatedUser); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch updated profile",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Profile updated successfully",
			"user": gin.H{
				"id":         updatedUser.ID.Hex(),
				"username":   updatedUser.Username,
				"name":       updatedUser.Name,
				"email":      updatedUser.Email,
				"phone":      updatedUser.Phone,
				"avatar_url": updatedUser.AvatarURL,
				"role":       updatedUser.Role,
				"client_id":  updatedUser.ClientID,
				"updated_at": updatedUser.UpdatedAt,
			},
		})
	})

	// Change password endpoint
	authGroup.PATCH("/change-password", authMiddleware.RequireAuth(), func(c *gin.Context) {
		// Get user ID from context (set by auth middleware)
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication required",
			})
			return
		}

		// Convert string ID to ObjectID
		objectID, err := primitive.ObjectIDFromHex(userID.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
			})
			return
		}

		var passwordData struct {
			CurrentPassword string `json:"current_password" binding:"required"`
			NewPassword     string `json:"new_password" binding:"required,min=8,max=128"`
		}

		if err := c.ShouldBindJSON(&passwordData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_data",
				"message":    "Invalid request data",
			})
			return
		}

		// Find user by ID
		var user models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"_id": objectID}).Decode(&user); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		// Verify current password
		if !utils.CheckPassword(passwordData.CurrentPassword, user.PasswordHash) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_current_password",
				"message":    "Current password is incorrect",
			})
			return
		}

		// Hash new password
		hashedPassword, err := utils.HashPassword(passwordData.NewPassword, cfg.BcryptCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to process new password",
			})
			return
		}

		// Update password
		_, err = usersCollection.UpdateOne(context.Background(), bson.M{"_id": objectID}, bson.M{
			"$set": bson.M{
				"password_hash": hashedPassword,
				"updated_at":    time.Now(),
			},
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to update password",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Password changed successfully",
		})
	})

	// Upload avatar endpoint
	authGroup.POST("/upload-avatar", authMiddleware.RequireAuth(), func(c *gin.Context) {
		// Get user ID from context (set by auth middleware)
		userID, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication required",
			})
			return
		}

		// Convert string ID to ObjectID
		objectID, err := primitive.ObjectIDFromHex(userID.(string))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
			})
			return
		}

		// Get uploaded file
		file, err := c.FormFile("avatar")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "no_file",
				"message":    "No avatar file uploaded",
			})
			return
		}

		// Validate file type
		contentType := file.Header.Get("Content-Type")
		allowedTypes := []string{"image/jpeg", "image/png", "image/gif", "image/webp"}
		isValidType := false
		for _, allowedType := range allowedTypes {
			if contentType == allowedType {
				isValidType = true
				break
			}
		}

		if !isValidType {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_file_type",
				"message":    "Only image files (JPEG, PNG, GIF, WebP) are allowed",
			})
			return
		}

		// Validate file size (max 5MB)
		if file.Size > 5*1024*1024 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "file_too_large",
				"message":    "File size must be less than 5MB",
			})
			return
		}

		// Generate unique filename
		filename := fmt.Sprintf("avatar_%s_%d_%s", userID.(string), time.Now().Unix(), file.Filename)
		filepath := fmt.Sprintf("uploads/avatars/%s", filename)

		// Create uploads directory if it doesn't exist
		if err := os.MkdirAll("uploads/avatars", 0755); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to create upload directory",
			})
			return
		}

		// Save file
		if err := c.SaveUploadedFile(file, filepath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_save_error",
				"message":    "Failed to save avatar file",
			})
			return
		}

		// Update user's avatar URL in database
		avatarURL := fmt.Sprintf("/uploads/avatars/%s", filename)
		_, err = usersCollection.UpdateOne(context.Background(), bson.M{"_id": objectID}, bson.M{
			"$set": bson.M{
				"avatar_url": avatarURL,
				"updated_at": time.Now(),
			},
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to update avatar URL",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "Avatar uploaded successfully",
			"avatar_url": avatarURL,
		})
	})

	// Forgot password endpoint
	authGroup.POST("/forgot-password", func(c *gin.Context) {
		var req models.ForgotPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Find user by email
		var user models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"email": req.Email}).Decode(&user); err != nil {
			// Email does not exist in database
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "email_not_found",
					"message":    "No account found with this email address. Please check your email and try again.",
				})
				return
			}
			// Database error
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to check email address",
			})
			return
		}

		// Check if user has an email (shouldn't happen if email is required, but handle gracefully)
		if user.Email == "" {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "email_not_found",
				"message":    "No account found with this email address. Please check your email and try again.",
			})
			return
		}

		// Generate secure reset token
		token, err := utils.GenerateSecureRandomString(32)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to generate reset token",
			})
			return
		}

		// Create password reset record (expires in 1 hour)
		expiresAt := time.Now().Add(1 * time.Hour)
		passwordReset := models.PasswordReset{
			ID:        primitive.NewObjectID(),
			UserID:    user.ID,
			Token:     token,
			Email:     req.Email,
			ExpiresAt: expiresAt,
			Used:      false,
			CreatedAt: time.Now(),
		}

		// Invalidate any existing reset tokens for this user
		_, _ = passwordResetsCollection.UpdateMany(context.Background(),
			bson.M{"user_id": user.ID, "used": false},
			bson.M{"$set": bson.M{"used": true}},
		)

		// Save new reset token
		_, err = passwordResetsCollection.InsertOne(context.Background(), passwordReset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to create reset token",
			})
			return
		}

		// Get frontend URL from config or use default
		frontendURL := os.Getenv("FRONTEND_URL")
		if frontendURL == "" {
			frontendURL = "http://localhost:3000"
		}
		resetURL := fmt.Sprintf("%s/reset-password?token=%s", frontendURL, token)

		// Send reset email
		emailSender := services.NewSMTPEmailSender(*cfg)
		subject := "Password Reset Request"
		htmlBody := fmt.Sprintf(`
			<html>
			<body>
				<h2>Password Reset Request</h2>
				<p>Hello %s,</p>
				<p>You requested to reset your password. Click the link below to reset your password:</p>
				<p><a href="%s" style="background-color: #3B82F6; color: white; padding: 10px 20px; text-decoration: none; border-radius: 5px; display: inline-block;">Reset Password</a></p>
				<p>Or copy and paste this link into your browser:</p>
				<p>%s</p>
				<p>This link will expire in 1 hour.</p>
				<p>If you didn't request this, please ignore this email.</p>
			</body>
			</html>
		`, user.Name, resetURL, resetURL)
		textBody := fmt.Sprintf(`
			Password Reset Request
			
			Hello %s,
			
			You requested to reset your password. Click the link below to reset your password:
			
			%s
			
			This link will expire in 1 hour.
			
			If you didn't request this, please ignore this email.
		`, user.Name, resetURL)

		// Send email (don't fail if email sending fails, just log it)
		if err := emailSender.SendEmail([]string{req.Email}, subject, htmlBody, textBody); err != nil {
			log.Printf("Failed to send password reset email: %v", err)
			// Still return success to user (security best practice)
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "If an account with that email exists, a password reset link has been sent.",
		})
	})

	// Reset password endpoint
	authGroup.POST("/reset-password", func(c *gin.Context) {
		var req models.ResetPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Find password reset record
		var passwordReset models.PasswordReset
		if err := passwordResetsCollection.FindOne(context.Background(), bson.M{"token": req.Token}).Decode(&passwordReset); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_token",
				"message":    "Invalid or expired reset token",
			})
			return
		}

		// Check if token is already used
		if passwordReset.Used {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "token_used",
				"message":    "This reset token has already been used",
			})
			return
		}

		// Check if token is expired
		if time.Now().After(passwordReset.ExpiresAt) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "token_expired",
				"message":    "This reset token has expired",
			})
			return
		}

		// Find user
		var user models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"_id": passwordReset.UserID}).Decode(&user); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		// Hash new password
		hashedPassword, err := utils.HashPassword(req.Password, cfg.BcryptCost)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to process new password",
			})
			return
		}

		// Update user password
		_, err = usersCollection.UpdateOne(context.Background(), bson.M{"_id": user.ID}, bson.M{
			"$set": bson.M{
				"password_hash": hashedPassword,
				"updated_at":    time.Now(),
			},
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to update password",
			})
			return
		}

		// Mark token as used
		_, _ = passwordResetsCollection.UpdateOne(context.Background(), bson.M{"_id": passwordReset.ID}, bson.M{
			"$set": bson.M{"used": true},
		})

		c.JSON(http.StatusOK, gin.H{
			"message": "Password reset successfully",
		})
	})
}

// Helper function for min
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
