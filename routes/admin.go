package routes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/internal/crawler"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/services"
	"saas-chatbot-platform/utils"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/generative-ai-go/genai"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/api/option"
)

func SetupAdminRoutes(
	router *gin.Engine,
	cfg *config.Config,
	mongoClient *mongo.Client,
	authMiddleware *middleware.AuthMiddleware,
	roleMiddleware *middleware.RoleMiddleware,
) {
	admin := router.Group("/admin")
	admin.Use(authMiddleware.RequireAuth())
	admin.Use(roleMiddleware.AdminGuard())

	db := mongoClient.Database(cfg.DBName)
	clientsCollection := db.Collection("clients")
	usersCollection := db.Collection("users")
	messagesCollection := db.Collection("messages")
	pdfsCollection := db.Collection("pdfs")
	crawlsCollection := db.Collection("crawls")
	imagesCollection := db.Collection("images")
	facebookPostsCollection := db.Collection("facebook_posts")
	instagramPostsCollection := db.Collection("instagram_posts")
	alertsCollection := db.Collection("suspicious_activity_alerts")

	// Check if email exists endpoint
	admin.GET("/check-email", func(c *gin.Context) {
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

	// Check if username exists endpoint
	admin.GET("/check-username", func(c *gin.Context) {
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

	// Check if phone exists endpoint
	admin.GET("/check-phone", func(c *gin.Context) {
		phone := c.Query("phone")
		excludeUserID := c.Query("exclude_user_id")
		if phone == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Phone parameter is required",
			})
			return
		}

		// Build query to check if phone exists, optionally excluding current user
		query := bson.M{"phone": phone}
		if excludeUserID != "" {
			excludeObjID, err := primitive.ObjectIDFromHex(excludeUserID)
			if err == nil {
				query["_id"] = bson.M{"$ne": excludeObjID}
			}
		}

		// Check if phone exists in users collection
		var existingUser models.User
		err := usersCollection.FindOne(context.Background(), query).Decode(&existingUser)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				// Phone does not exist
				c.JSON(http.StatusOK, gin.H{
					"exists": false,
					"message": "Phone is available",
				})
				return
			}
			// Database error
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to check phone",
			})
			return
		}

		// Phone exists
		c.JSON(http.StatusOK, gin.H{
			"exists": true,
			"message": "Phone already exists",
		})
	})

	admin.DELETE("/client/:id", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Check if client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to verify client",
			})
			return
		}

		// ✅ Fixed field names: clientid → client_id
		// 1. Delete all PDFs for this client
		_, err = pdfsCollection.DeleteMany(context.Background(), bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to delete client PDFs",
			})
			return
		}

		// 2. Delete all messages for this client
		_, err = messagesCollection.DeleteMany(context.Background(), bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to delete client messages",
			})
			return
		}

		// 3. Delete all users for this client
		_, err = usersCollection.DeleteMany(context.Background(), bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to delete client users",
			})
			return
		}

		// 4. Delete all media for this client
		mediaCollection := db.Collection("media")
		_, err = mediaCollection.DeleteMany(context.Background(), bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to delete client media",
			})
			return
		}

		// 5. Finally, delete the client itself
		result, err := clientsCollection.DeleteOne(context.Background(), bson.M{"_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to delete client",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Success response
		c.JSON(http.StatusOK, gin.H{
			"message":   "Client and all associated data deleted successfully",
			"client_id": clientID.Hex(),
			"deleted": gin.H{
				"client":   1,
				"users":    "all associated users",
				"messages": "all associated messages",
				"pdfs":     "all associated PDFs",
				"media":    "all associated media files",
			},
		})
	})

	// Get single client endpoint
	// -------------------------
	// Manage Users - Client List
	// -------------------------
	admin.GET("/manage-users/clients", func(c *gin.Context) {
		cursor, err := clientsCollection.Find(context.Background(), bson.M{}, options.Find().SetProjection(bson.M{
			"name":         1,
			"status":       1,
			"token_limit":  1,
			"token_used":   1,
			"created_at":   1,
			"updated_at":   1,
		}))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve clients",
			})
			return
		}
		defer cursor.Close(context.Background())

		var clients []gin.H
		for cursor.Next(context.Background()) {
			var client models.Client
			if err := cursor.Decode(&client); err != nil {
				continue
			}

			usagePercentage := 0.0
			if client.TokenLimit > 0 {
				usagePercentage = float64(client.TokenUsed) / float64(client.TokenLimit) * 100
			}

			clients = append(clients, gin.H{
				"id":               client.ID.Hex(),
				"name":             client.Name,
				"status":           client.Status,
				"token_limit":      client.TokenLimit,
				"token_used":       client.TokenUsed,
				"usage_percentage": usagePercentage,
				"created_at":       client.CreatedAt,
				"updated_at":       client.UpdatedAt,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"clients": clients,
		})
	})

	// -------------------------
	// Client Permissions
	// -------------------------
	// Get client permissions
	admin.GET("/client/:id/permissions", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Return permissions (empty arrays if not set - backward compatible)
		permissions := client.Permissions
		if permissions.AllowedNavigationItems == nil {
			permissions.AllowedNavigationItems = []string{}
		}
		if permissions.EnabledFeatures == nil {
			permissions.EnabledFeatures = []string{}
		}

		c.JSON(http.StatusOK, gin.H{
			"client_id":                clientID.Hex(),
			"allowed_navigation_items": permissions.AllowedNavigationItems,
			"enabled_features":         permissions.EnabledFeatures,
		})
	})

	// Update client permissions
	admin.PATCH("/client/:id/permissions", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var req models.UpdateClientPermissionsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Validate navigation items
		if err := services.ValidateNavigationItems(req.AllowedNavigationItems); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_navigation_items",
				"message":    err.Error(),
			})
			return
		}

		// Auto-sync features based on navigation items
		enabledFeatures := services.SyncFeaturesFromNavigationItems(req.AllowedNavigationItems)

		// Debug logging
		if os.Getenv("GIN_MODE") != "release" {
			fmt.Printf("[DEBUG] Updating permissions for client %s: allowed_items=%v, enabled_features=%v\n",
				clientID.Hex(), req.AllowedNavigationItems, enabledFeatures)
		}

		// Update client permissions
		update := bson.M{
			"$set": bson.M{
				"permissions.allowed_navigation_items": req.AllowedNavigationItems,
				"permissions.enabled_features":         enabledFeatures,
				"updated_at":                           time.Now(),
			},
		}

		result, err := clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to update client permissions",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Verify the update was successful by reading back
		var updatedClient models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&updatedClient); err == nil {
			if os.Getenv("GIN_MODE") != "release" {
				fmt.Printf("[DEBUG] Permissions saved successfully for client %s: allowed_items=%v\n",
					clientID.Hex(), updatedClient.Permissions.AllowedNavigationItems)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"message":                  "Client permissions updated successfully",
			"client_id":                clientID.Hex(),
			"allowed_navigation_items": req.AllowedNavigationItems,
			"enabled_features":         enabledFeatures,
		})
	})

	// -------------------------
	// Calendly Configuration
	// -------------------------
	// Get Calendly configuration for a client
	admin.GET("/client/:id/calendly", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var client models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Calendly configuration",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"calendly_url":     client.CalendlyURL,
			"calendly_enabled": client.CalendlyEnabled,
		})
	})

	// Update Calendly configuration for a client
	admin.PATCH("/client/:id/calendly", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var request struct {
			CalendlyURL     string `json:"calendly_url"`
			CalendlyEnabled *bool  `json:"calendly_enabled,omitempty"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate Calendly URL format if provided
		if request.CalendlyURL != "" {
			parsedURL, err := url.Parse(request.CalendlyURL)
			if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_url",
					"message":    "Invalid Calendly URL format",
				})
				return
			}
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if request.CalendlyURL != "" {
			update["$set"].(bson.M)["calendly_url"] = request.CalendlyURL
		}

		if request.CalendlyEnabled != nil {
			update["$set"].(bson.M)["calendly_enabled"] = *request.CalendlyEnabled
		}

		result, err := clientsCollection.UpdateOne(ctx, bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update Calendly configuration",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Fetch updated client to return
		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message":         "Calendly configuration updated successfully",
				"calendly_url":     request.CalendlyURL,
				"calendly_enabled": request.CalendlyEnabled,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":         "Calendly configuration updated successfully",
			"calendly_url":     updatedClient.CalendlyURL,
			"calendly_enabled": updatedClient.CalendlyEnabled,
		})
	})

	// -------------------------
	// QR Code Configuration
	// -------------------------
	// Get QR Code configuration for a client
	admin.GET("/client/:id/qr-code", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var client models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch QR code configuration",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"qr_code_image_url": client.QRCodeImageURL,
			"qr_code_enabled":   client.QRCodeEnabled,
		})
	})

	// Update QR Code configuration for a client
	admin.PATCH("/client/:id/qr-code", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var request struct {
			QRCodeImageURL string `json:"qr_code_image_url"`
			QRCodeEnabled  *bool  `json:"qr_code_enabled,omitempty"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate QR code image URL format if provided
		if request.QRCodeImageURL != "" {
			qrCodeURL := request.QRCodeImageURL
			if strings.HasPrefix(qrCodeURL, "data:image/") {
				// Data URL is valid
			} else if strings.HasPrefix(qrCodeURL, "/") {
				// Relative URL is valid
			} else {
				parsedURL, err := url.Parse(qrCodeURL)
				if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
					c.JSON(http.StatusBadRequest, gin.H{
						"error_code": "invalid_url",
						"message":    "Invalid QR code image URL format. Must be a valid HTTP/HTTPS URL, data URL, or relative path",
					})
					return
				}
			}
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if request.QRCodeImageURL != "" {
			update["$set"].(bson.M)["qr_code_image_url"] = request.QRCodeImageURL
		}

		if request.QRCodeEnabled != nil {
			update["$set"].(bson.M)["qr_code_enabled"] = *request.QRCodeEnabled
		}

		result, err := clientsCollection.UpdateOne(ctx, bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update QR code configuration",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message":           "QR code configuration updated successfully",
				"qr_code_image_url": request.QRCodeImageURL,
				"qr_code_enabled":   request.QRCodeEnabled,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":           "QR code configuration updated successfully",
			"qr_code_image_url": updatedClient.QRCodeImageURL,
			"qr_code_enabled":   updatedClient.QRCodeEnabled,
		})
	})

	// -------------------------
	// WhatsApp QR Code Configuration
	// -------------------------
	// Get WhatsApp QR Code configuration for a client
	admin.GET("/client/:id/whatsapp-qr-code", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var client models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch WhatsApp QR code configuration",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"whatsapp_qr_code_image_url": client.WhatsAppQRCodeImageURL,
			"whatsapp_qr_code_enabled":   client.WhatsAppQRCodeEnabled,
		})
	})

	// Update WhatsApp QR Code configuration for a client
	admin.PATCH("/client/:id/whatsapp-qr-code", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var request struct {
			WhatsAppQRCodeImageURL string `json:"whatsapp_qr_code_image_url"`
			WhatsAppQRCodeEnabled  *bool  `json:"whatsapp_qr_code_enabled,omitempty"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate WhatsApp QR code image URL format if provided
		if request.WhatsAppQRCodeImageURL != "" {
			qrCodeURL := request.WhatsAppQRCodeImageURL
			if strings.HasPrefix(qrCodeURL, "data:image/") {
				// Data URL is valid
			} else if strings.HasPrefix(qrCodeURL, "/") {
				// Relative URL is valid
			} else {
				parsedURL, err := url.Parse(qrCodeURL)
				if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
					c.JSON(http.StatusBadRequest, gin.H{
						"error_code": "invalid_url",
						"message":    "Invalid WhatsApp QR code image URL format. Must be a valid HTTP/HTTPS URL, data URL, or relative path",
					})
					return
				}
			}
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if request.WhatsAppQRCodeImageURL != "" {
			update["$set"].(bson.M)["whatsapp_qr_code_image_url"] = request.WhatsAppQRCodeImageURL
		}

		if request.WhatsAppQRCodeEnabled != nil {
			update["$set"].(bson.M)["whatsapp_qr_code_enabled"] = *request.WhatsAppQRCodeEnabled
		}

		result, err := clientsCollection.UpdateOne(ctx, bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update WhatsApp QR code configuration",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message":                     "WhatsApp QR code configuration updated successfully",
				"whatsapp_qr_code_image_url":  request.WhatsAppQRCodeImageURL,
				"whatsapp_qr_code_enabled":    request.WhatsAppQRCodeEnabled,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":                     "WhatsApp QR code configuration updated successfully",
			"whatsapp_qr_code_image_url":  updatedClient.WhatsAppQRCodeImageURL,
			"whatsapp_qr_code_enabled":    updatedClient.WhatsAppQRCodeEnabled,
		})
	})

	// -------------------------
	// Telegram QR Code Configuration
	// -------------------------
	// Get Telegram QR Code configuration for a client
	admin.GET("/client/:id/telegram-qr-code", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var client models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Telegram QR code configuration",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"telegram_qr_code_image_url": client.TelegramQRCodeImageURL,
			"telegram_qr_code_enabled":   client.TelegramQRCodeEnabled,
		})
	})

	// Update Telegram QR Code configuration for a client
	admin.PATCH("/client/:id/telegram-qr-code", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var request struct {
			TelegramQRCodeImageURL string `json:"telegram_qr_code_image_url"`
			TelegramQRCodeEnabled  *bool  `json:"telegram_qr_code_enabled,omitempty"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate Telegram QR code image URL format if provided
		if request.TelegramQRCodeImageURL != "" {
			qrCodeURL := request.TelegramQRCodeImageURL
			if strings.HasPrefix(qrCodeURL, "data:image/") {
				// Data URL is valid
			} else if strings.HasPrefix(qrCodeURL, "/") {
				// Relative URL is valid
			} else {
				parsedURL, err := url.Parse(qrCodeURL)
				if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
					c.JSON(http.StatusBadRequest, gin.H{
						"error_code": "invalid_url",
						"message":    "Invalid Telegram QR code image URL format. Must be a valid HTTP/HTTPS URL, data URL, or relative path",
					})
					return
				}
			}
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if request.TelegramQRCodeImageURL != "" {
			update["$set"].(bson.M)["telegram_qr_code_image_url"] = request.TelegramQRCodeImageURL
		}

		if request.TelegramQRCodeEnabled != nil {
			update["$set"].(bson.M)["telegram_qr_code_enabled"] = *request.TelegramQRCodeEnabled
		}

		result, err := clientsCollection.UpdateOne(ctx, bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update Telegram QR code configuration",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message":                      "Telegram QR code configuration updated successfully",
				"telegram_qr_code_image_url":   request.TelegramQRCodeImageURL,
				"telegram_qr_code_enabled":     request.TelegramQRCodeEnabled,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":                      "Telegram QR code configuration updated successfully",
			"telegram_qr_code_image_url":   updatedClient.TelegramQRCodeImageURL,
			"telegram_qr_code_enabled":     updatedClient.TelegramQRCodeEnabled,
		})
	})

	// -------------------------
	// Email Templates
	// -------------------------
	// Get all email templates for a client
	admin.GET("/client/:id/email-templates", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		emailTemplatesCollection := db.Collection("email_templates")
		cursor, err := emailTemplatesCollection.Find(ctx, bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch email templates",
				"details":    err.Error(),
			})
			return
		}
		defer cursor.Close(ctx)

		var templates []models.EmailTemplate
		if err := cursor.All(ctx, &templates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode email templates",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, templates)
	})

	// Get email template by type for a client
	admin.GET("/client/:id/email-templates/:type", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		templateType := c.Param("type")
		if templateType == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_type",
				"message":    "Template type is required",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		emailTemplatesCollection := db.Collection("email_templates")
		var template models.EmailTemplate
		err = emailTemplatesCollection.FindOne(ctx, bson.M{
			"client_id": clientID,
			"type":       templateType,
		}).Decode(&template)

		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "template_not_found",
				"message":    "Email template not found",
			})
			return
		}

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch email template",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, template)
	})

	// Create email template for a client
	admin.POST("/client/:id/email-templates", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to verify client",
			})
			return
		}

		var req struct {
			Type           string                     `json:"type" binding:"required"`
			Name           string                     `json:"name" binding:"required"`
			Subject        string                     `json:"subject" binding:"required"`
			HTMLBody       string                     `json:"html_body" binding:"required"`
			TextBody       string                     `json:"text_body" binding:"required"`
			TemplateFields models.EmailTemplateFields `json:"template_fields"`
			IsActive       bool                       `json:"is_active"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		emailTemplatesCollection := db.Collection("email_templates")

		// Check if template with same type already exists
		var existingTemplate models.EmailTemplate
		err = emailTemplatesCollection.FindOne(ctx, bson.M{
			"client_id": clientID,
			"type":      req.Type,
		}).Decode(&existingTemplate)

		if err == nil {
			c.JSON(http.StatusConflict, gin.H{
				"error_code": "template_exists",
				"message":    "Email template with this type already exists",
			})
			return
		}

		template := models.EmailTemplate{
			ID:             primitive.NewObjectID(),
			ClientID:       clientID,
			Type:           req.Type,
			Name:           req.Name,
			Subject:        req.Subject,
			HTMLBody:       req.HTMLBody,
			TextBody:       req.TextBody,
			TemplateFields: req.TemplateFields,
			IsActive:       req.IsActive,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}

		_, err = emailTemplatesCollection.InsertOne(ctx, template)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to create email template",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusCreated, template)
	})

	// Update email template for a client
	admin.PUT("/client/:id/email-templates/:templateId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		templateIDHex := c.Param("templateId")
		templateObjID, err := primitive.ObjectIDFromHex(templateIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_id",
				"message":    "Invalid template ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to verify client",
			})
			return
		}

		var req struct {
			Name           *string                     `json:"name"`
			Subject        *string                     `json:"subject"`
			HTMLBody       *string                     `json:"html_body"`
			TextBody       *string                     `json:"text_body"`
			TemplateFields *models.EmailTemplateFields `json:"template_fields"`
			IsActive       *bool                       `json:"is_active"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		emailTemplatesCollection := db.Collection("email_templates")

		// Verify template belongs to client
		var existingTemplate models.EmailTemplate
		err = emailTemplatesCollection.FindOne(ctx, bson.M{
			"_id":       templateObjID,
			"client_id": clientID,
		}).Decode(&existingTemplate)

		if err == mongo.ErrNoDocuments {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "template_not_found",
				"message":    "Email template not found",
			})
			return
		}

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch email template",
				"details":    err.Error(),
			})
			return
		}

		// Build update document
		setFields := bson.M{"updated_at": time.Now()}
		if req.Name != nil {
			setFields["name"] = *req.Name
		}
		if req.Subject != nil {
			setFields["subject"] = *req.Subject
		}
		if req.HTMLBody != nil {
			setFields["html_body"] = *req.HTMLBody
		}
		if req.TextBody != nil {
			setFields["text_body"] = *req.TextBody
		}
		if req.TemplateFields != nil {
			setFields["template_fields"] = *req.TemplateFields
		}
		if req.IsActive != nil {
			setFields["is_active"] = *req.IsActive
		}

		update := bson.M{"$set": setFields}

		result, err := emailTemplatesCollection.UpdateOne(ctx, bson.M{
			"_id":       templateObjID,
			"client_id": clientID,
		}, update)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to update email template",
				"details":    err.Error(),
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "template_not_found",
				"message":    "Email template not found or does not belong to client",
			})
			return
		}

		// Fetch updated template
		var updatedTemplate models.EmailTemplate
		err = emailTemplatesCollection.FindOne(ctx, bson.M{"_id": templateObjID}).Decode(&updatedTemplate)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch updated template",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, updatedTemplate)
	})

	// Delete email template for a client
	admin.DELETE("/client/:id/email-templates/:templateId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		templateIDHex := c.Param("templateId")
		templateObjID, err := primitive.ObjectIDFromHex(templateIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_id",
				"message":    "Invalid template ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to verify client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		emailTemplatesCollection := db.Collection("email_templates")

		result, err := emailTemplatesCollection.DeleteOne(ctx, bson.M{
			"_id":       templateObjID,
			"client_id": clientID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to delete email template",
				"details":    err.Error(),
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "template_not_found",
				"message":    "Email template not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Email template deleted successfully",
		})
	})

	admin.GET("/client/:id", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Fetch the initial user (first user created for this client) to get email and phone
		var initialUser models.User
		err = usersCollection.FindOne(
			context.Background(),
			bson.M{"client_id": clientID},
			options.FindOne().SetSort(bson.M{"created_at": 1}), // Get the first user (oldest)
		).Decode(&initialUser)

		// Build response with initial user info if available
		response := gin.H{
			"client": client,
		}

		// If client doesn't have contact_email/contact_phone but we found an initial user, include their info
		if err == nil {
			response["initial_user"] = gin.H{
				"id":    initialUser.ID.Hex(),
				"email": initialUser.Email,
				"phone": initialUser.Phone,
			}
			// If client's contact fields are empty, populate them from initial user
			if client.ContactEmail == "" && initialUser.Email != "" {
				client.ContactEmail = initialUser.Email
			}
			if client.ContactPhone == "" && initialUser.Phone != "" {
				client.ContactPhone = initialUser.Phone
			}
			response["client"] = client
		}

		c.JSON(http.StatusOK, response)
	})

	// Add this to your models file (models/client.go) to support token history

	// Then in your routes file, add this enhanced version:
	admin.POST("/client/:id/token-reset", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error_code": "invalid_client_id", "message": "Invalid client ID format"})
			return
		}

		// Get the token reset request
		var req struct {
			NewTokenLimit int    `json:"new_token_limit" binding:"required,min=1000"`
			Reason        string `json:"reason,omitempty"`
			AdminUserID   string `json:"admin_user_id,omitempty"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{"error_code": "client_not_found", "message": "Client not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to verify client"})
			return
		}

		// Create token history entry
		tokenHistory := models.TokenHistory{
			ID:          primitive.NewObjectID(),
			ClientID:    clientID,
			OldLimit:    client.TokenLimit,
			NewLimit:    req.NewTokenLimit,
			Reason:      req.Reason,
			AdminUserID: req.AdminUserID,
			Timestamp:   time.Now(),
			Action:      "reset",
		}

		// Save token history (create a new collection for this)
		tokenHistoryCollection := db.Collection("token_history")
		_, err = tokenHistoryCollection.InsertOne(context.Background(), tokenHistory)
		if err != nil {
			// Log error but continue with token reset
			fmt.Printf("Warning: Failed to save token history: %v\n", err)
		}

		// Update client token limit and reset used tokens
		update := bson.M{
			"$set": bson.M{
				"token_limit": req.NewTokenLimit,
				"token_used":  0, // Reset used tokens
				"updated_at":  time.Now(),
			},
		}

		result, err := clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to update client tokens"})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error_code": "client_not_found", "message": "Client not found"})
			return
		}

		// Return success response
		c.JSON(http.StatusOK, gin.H{
			"message":    "Client tokens successfully reset",
			"client_id":  clientID.Hex(),
			"old_limit":  client.TokenLimit,
			"new_limit":  req.NewTokenLimit,
			"reason":     req.Reason,
			"reset_at":   time.Now(),
			"history_id": tokenHistory.ID.Hex(),
		})
	})

	// -------------------------
	// Create new client tenant
	// -------------------------
	admin.POST("/client", func(c *gin.Context) {
		var req models.CreateClientRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		embedSecret, err := utils.GenerateEmbedSecret()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to generate embed secret"})
			return
		}

		status := req.Status
		if status == "" {
			status = "active"
		}

		client := models.Client{
			Name:         req.Name,
			Branding:     req.Branding,
			TokenLimit:   req.TokenLimit,
			TokenUsed:    0,
			EmbedSecret:  embedSecret,
			Status:       status,
			ContactEmail: req.ContactEmail,
			ContactPhone: req.ContactPhone,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		result, err := clientsCollection.InsertOne(context.Background(), client)
		if err != nil {
			if mongo.IsDuplicateKeyError(err) {
				c.JSON(http.StatusConflict, gin.H{"error_code": "client_exists", "message": "Client with this name already exists"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to create client"})
			return
		}

		client.ID = result.InsertedID.(primitive.ObjectID)

		// Optional first user
		var createdUser *models.UserInfo
		if req.InitialUser != nil {
			var existing models.User
			if err := usersCollection.FindOne(context.Background(), bson.M{"username": req.InitialUser.Username}).Decode(&existing); err == nil {
				c.JSON(http.StatusConflict, gin.H{"error_code": "username_exists", "message": "Initial user username already exists"})
				return
			}

			hashed, err := utils.HashPassword(req.InitialUser.Password, cfg.BcryptCost)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to process password"})
				return
			}

			role := "client" // default
			if req.InitialUser.Role != "" {
				// Only allow visitor or client roles - admin/superadmin must be created via superadmin endpoint
				if req.InitialUser.Role == "admin" || req.InitialUser.Role == "superadmin" {
					c.JSON(http.StatusBadRequest, gin.H{
						"error_code": "invalid_role",
						"message":    "Admin and superadmin roles cannot be created through client creation. Use the admin management interface instead.",
					})
					return
				}
				role = req.InitialUser.Role // Frontend can override (only visitor or client)
			}

			user := models.User{
				Username:     req.InitialUser.Username,
				Name:         req.InitialUser.Name,
				Email:        req.InitialUser.Email,
				Phone:        req.InitialUser.Phone,
				PasswordHash: hashed,
				Role:         role,
				ClientID:     &client.ID,
				TokenUsage:   0,
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
			}

			uRes, err := usersCollection.InsertOne(context.Background(), user)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Client created but failed to create initial user"})
				return
			}
			user.ID = uRes.InsertedID.(primitive.ObjectID)
			createdUser = &models.UserInfo{
				ID:       user.ID.Hex(),
				Username: user.Username,
				Name:     user.Name,
				Email:    user.Email,
				Phone:    user.Phone,
				Role:     user.Role,
				ClientID: client.ID.Hex(),
			}
		}

		type Resp struct {
			models.Client `json:"client"`
			InitialUser   *models.UserInfo `json:"initial_user,omitempty"`
		}
		c.JSON(http.StatusCreated, Resp{Client: client, InitialUser: createdUser})
	})

	// -------------------------
	// Update client
	// -------------------------
	// List clients
	// -------------------------
	admin.GET("/clients", func(c *gin.Context) {
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		skip := (page - 1) * limit

		cursor, err := clientsCollection.Find(context.Background(), bson.M{}, options.Find().SetSkip(int64(skip)).SetLimit(int64(limit)))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to retrieve clients"})
			return
		}
		defer cursor.Close(context.Background())

		var clientStats []models.ClientUsageStats
		for cursor.Next(context.Background()) {
			var client models.Client
			if err := cursor.Decode(&client); err != nil {
				continue
			}

			usagePercentage := 0.0
			if client.TokenLimit > 0 {
				usagePercentage = float64(client.TokenUsed) / float64(client.TokenLimit) * 100
			}

			var lastMessage models.Message
			_ = messagesCollection.FindOne(context.Background(), bson.M{"client_id": client.ID}, options.FindOne().SetSort(bson.M{"timestamp": -1})).Decode(&lastMessage)

			totalMessages, _ := messagesCollection.CountDocuments(context.Background(), bson.M{"client_id": client.ID})

			thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
			activeUsers, _ := usersCollection.CountDocuments(context.Background(), bson.M{
				"client_id":  client.ID,
				"updated_at": bson.M{"$gte": thirtyDaysAgo},
			})

			clientStats = append(clientStats, models.ClientUsageStats{
				Client:          client,
				UsagePercentage: usagePercentage,
				LastActivity:    lastMessage.Timestamp,
				TotalMessages:   int(totalMessages),
				ActiveUsers:     int(activeUsers),
			})
		}

		totalClients, _ := clientsCollection.CountDocuments(context.Background(), bson.M{})

		c.JSON(http.StatusOK, gin.H{
			"clients":     clientStats,
			"total":       totalClients,
			"page":        page,
			"limit":       limit,
			"total_pages": (totalClients + int64(limit) - 1) / int64(limit),
		})
	})

	// -------------------------
	// Usage analytics
	// -------------------------
	admin.GET("/usage", func(c *gin.Context) {
		periodStart := time.Now().AddDate(0, 0, -30)
		periodEnd := time.Now()

		totalClients, _ := clientsCollection.CountDocuments(context.Background(), bson.M{})

		pipeline := mongo.Pipeline{
			{primitive.E{Key: "$group", Value: bson.D{
				primitive.E{Key: "_id", Value: nil},
				primitive.E{Key: "total_tokens", Value: bson.D{primitive.E{Key: "$sum", Value: "$token_used"}}},
			}}},
		}

		cursor, err := clientsCollection.Aggregate(context.Background(), pipeline)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to calculate usage analytics"})
			return
		}
		defer cursor.Close(context.Background())

		var totalTokensUsed int
		if cursor.Next(context.Background()) {
			var result struct {
				TotalTokens int `bson:"total_tokens"`
			}
			_ = cursor.Decode(&result)
			totalTokensUsed = result.TotalTokens
		}

		totalMessages, _ := messagesCollection.CountDocuments(context.Background(),
			bson.M{"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd}},
		)

		activeClients, _ := messagesCollection.Distinct(context.Background(),
			"client_id",
			bson.M{"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd}},
		)

		// Aggregate daily usage data
		dailyUsagePipeline := mongo.Pipeline{
			{primitive.E{Key: "$match", Value: bson.M{
				"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd},
			}}},
			{primitive.E{Key: "$group", Value: bson.M{
				"_id": bson.M{
					"$dateToString": bson.M{
						"format": "%Y-%m-%d",
						"date":   "$timestamp",
					},
				},
				"tokens":       bson.M{"$sum": "$token_cost"},
				"messages":     bson.M{"$sum": 1},
				"active_users": bson.M{"$addToSet": "$session_id"},
				"conversations": bson.M{"$addToSet": "$conversation_id"},
			}}},
			{primitive.E{Key: "$project", Value: bson.M{
				"_id":                0,
				"date":               "$_id",
				"tokens":             bson.M{"$ifNull": []interface{}{"$tokens", 0}},
				"messages":           bson.M{"$ifNull": []interface{}{"$messages", 0}},
				"active_users":       bson.M{"$size": bson.M{"$ifNull": []interface{}{"$active_users", []interface{}{}}}},
				"total_conversations": bson.M{"$size": bson.M{"$ifNull": []interface{}{"$conversations", []interface{}{}}}},
			}}},
			{primitive.E{Key: "$sort", Value: bson.M{"date": 1}}},
		}

		dailyUsageCursor, err := messagesCollection.Aggregate(context.Background(), dailyUsagePipeline)
		var dailyUsage []models.DailyUsageData
		if err == nil {
			defer dailyUsageCursor.Close(context.Background())
			for dailyUsageCursor.Next(context.Background()) {
				var dayData struct {
					Date                string `bson:"date"`
					Tokens              int    `bson:"tokens"`
					Messages            int    `bson:"messages"`
					ActiveUsers         int    `bson:"active_users"`
					TotalConversations  int    `bson:"total_conversations"`
				}
				if err := dailyUsageCursor.Decode(&dayData); err == nil {
					dailyUsage = append(dailyUsage, models.DailyUsageData{
						Date:               dayData.Date,
						Tokens:             dayData.Tokens,
						Messages:           dayData.Messages,
						ActiveUsers:        dayData.ActiveUsers,
						TotalConversations: dayData.TotalConversations,
					})
				}
			}
		}

		// Aggregate hourly usage data - aggregate by hour of day across entire period
		// This shows the pattern of usage throughout the day (e.g., all 2 PM messages summed)
		hourlyUsagePipeline := mongo.Pipeline{
			{primitive.E{Key: "$match", Value: bson.M{
				"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd},
			}}},
			{primitive.E{Key: "$group", Value: bson.M{
				"_id": bson.M{
					"$dateToString": bson.M{
						"format": "%H:00",
						"date":   "$timestamp",
					},
				},
				"tokens":       bson.M{"$sum": bson.M{"$ifNull": []interface{}{"$token_cost", 0}}},
				"messages":     bson.M{"$sum": 1},
				"active_users": bson.M{"$addToSet": bson.M{"$cond": []interface{}{bson.M{"$ne": []interface{}{"$session_id", ""}}, "$session_id", "$$REMOVE"}}},
				"conversations": bson.M{"$addToSet": bson.M{"$cond": []interface{}{bson.M{"$ne": []interface{}{"$conversation_id", ""}}, "$conversation_id", "$$REMOVE"}}},
			}}},
			{primitive.E{Key: "$project", Value: bson.M{
				"_id":                0,
				"hour":               "$_id",
				"tokens":             bson.M{"$ifNull": []interface{}{"$tokens", 0}},
				"messages":           bson.M{"$ifNull": []interface{}{"$messages", 0}},
				"active_users":       bson.M{"$size": bson.M{"$ifNull": []interface{}{"$active_users", []interface{}{}}}},
				"total_conversations": bson.M{"$size": bson.M{"$ifNull": []interface{}{"$conversations", []interface{}{}}}},
			}}},
			{primitive.E{Key: "$sort", Value: bson.M{"hour": 1}}},
		}

		hourlyUsageCursor, err := messagesCollection.Aggregate(context.Background(), hourlyUsagePipeline)
		hourlyUsageMap := make(map[string]models.HourlyUsageData)
		if err == nil {
			defer hourlyUsageCursor.Close(context.Background())
			for hourlyUsageCursor.Next(context.Background()) {
				var hourData struct {
					Hour                string `bson:"hour"`
					Tokens              int    `bson:"tokens"`
					Messages            int    `bson:"messages"`
					ActiveUsers         int    `bson:"active_users"`
					TotalConversations  int    `bson:"total_conversations"`
				}
				if err := hourlyUsageCursor.Decode(&hourData); err == nil {
					// Format hour label (e.g., "14:00" -> "2 PM")
					hourLabel := hourData.Hour
					// Extract hour from "HH:00" format
					if len(hourLabel) >= 2 {
						hourInt := 0
						// Parse "HH:00" format - extract first two digits
						if _, err := fmt.Sscanf(hourLabel, "%d:", &hourInt); err != nil {
							// Fallback: try parsing as integer
							fmt.Sscanf(hourLabel, "%d", &hourInt)
						}
						if hourInt >= 0 && hourInt < 24 {
							var label string
							if hourInt == 0 {
								label = "12 AM"
							} else if hourInt < 12 {
								label = fmt.Sprintf("%d AM", hourInt)
							} else if hourInt == 12 {
								label = "12 PM"
							} else {
								label = fmt.Sprintf("%d PM", hourInt-12)
							}
							hourLabel = label
						}
					}
					hourlyUsageMap[hourData.Hour] = models.HourlyUsageData{
						Hour:               hourData.Hour,
						Label:              hourLabel,
						Tokens:             hourData.Tokens,
						Messages:           hourData.Messages,
						ActiveUsers:        hourData.ActiveUsers,
						TotalConversations: hourData.TotalConversations,
					}
				}
			}
		}

		// Fill in all 24 hours with zero values if no data exists
		var hourlyUsage []models.HourlyUsageData
		for i := 0; i < 24; i++ {
			hourStr := fmt.Sprintf("%02d:00", i)
			var label string
			if i == 0 {
				label = "12 AM"
			} else if i < 12 {
				label = fmt.Sprintf("%d AM", i)
			} else if i == 12 {
				label = "12 PM"
			} else {
				label = fmt.Sprintf("%d PM", i-12)
			}

			if data, exists := hourlyUsageMap[hourStr]; exists {
				hourlyUsage = append(hourlyUsage, data)
			} else {
				hourlyUsage = append(hourlyUsage, models.HourlyUsageData{
					Hour:               hourStr,
					Label:              label,
					Tokens:             0,
					Messages:           0,
					ActiveUsers:        0,
					TotalConversations: 0,
				})
			}
		}

		clientsCursor, err := clientsCollection.Find(context.Background(), bson.M{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to retrieve client statistics"})
			return
		}
		defer clientsCursor.Close(context.Background())

		var clientStats []models.ClientUsageStats
		for clientsCursor.Next(context.Background()) {
			var client models.Client
			if err := clientsCursor.Decode(&client); err != nil {
				continue
			}

			usage := 0.0
			if client.TokenLimit > 0 {
				usage = float64(client.TokenUsed) / float64(client.TokenLimit) * 100
			}

			var lastMessage models.Message
			_ = messagesCollection.FindOne(context.Background(),
				bson.M{"client_id": client.ID},
				options.FindOne().SetSort(bson.M{"timestamp": -1}),
			).Decode(&lastMessage)

			clientMessageCount, _ := messagesCollection.CountDocuments(context.Background(), bson.M{
				"client_id": client.ID,
				"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd},
			})

			activeUsersCount, _ := usersCollection.CountDocuments(context.Background(), bson.M{
				"client_id":  client.ID,
				"updated_at": bson.M{"$gte": periodStart},
			})

			clientStats = append(clientStats, models.ClientUsageStats{
				Client:          client,
				UsagePercentage: usage,
				LastActivity:    lastMessage.Timestamp,
				TotalMessages:   int(clientMessageCount),
				ActiveUsers:     int(activeUsersCount),
			})
		}

		// Calculate performance metrics
		// System Uptime: Check MongoDB connection status (ping)
		systemUptime := 99.9 // Default to 99.9% if we can't determine
		if err := mongoClient.Ping(context.Background(), nil); err == nil {
			systemUptime = 100.0 // MongoDB is connected, system is up
		} else {
			systemUptime = 0.0 // MongoDB is unreachable
		}

		// Average Response Time: Calculate from messages timestamp differences (simplified)
		// For now, we'll use a simple calculation based on message processing
		// In a real system, you'd track this in audit logs or middleware
		averageResponseTime := 0.0
		recentMessages, _ := messagesCollection.Find(context.Background(),
			bson.M{"timestamp": bson.M{"$gte": periodStart.Add(-1 * time.Hour)}},
			options.Find().SetLimit(100).SetSort(bson.M{"timestamp": -1}),
		)
		var responseTimes []float64
		var prevTimestamp time.Time
		for recentMessages.Next(context.Background()) {
			var msg models.Message
			if err := recentMessages.Decode(&msg); err == nil {
				if !prevTimestamp.IsZero() {
					diff := prevTimestamp.Sub(msg.Timestamp).Milliseconds()
					if diff > 0 && diff < 10000 { // Reasonable range (0-10 seconds)
						responseTimes = append(responseTimes, float64(diff))
					}
				}
				prevTimestamp = msg.Timestamp
			}
		}
		recentMessages.Close(context.Background())
		if len(responseTimes) > 0 {
			sum := 0.0
			for _, rt := range responseTimes {
				sum += rt
			}
			averageResponseTime = sum / float64(len(responseTimes))
		}

		// Error Rate: Calculate from audit logs
		errorRate := 0.0
		auditLogsCollection := db.Collection("audit_logs")
		totalAuditEvents, _ := auditLogsCollection.CountDocuments(context.Background(),
			bson.M{"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd}},
		)
		failedAuditEvents, _ := auditLogsCollection.CountDocuments(context.Background(),
			bson.M{
				"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd},
				"success":   false,
			},
		)
		if totalAuditEvents > 0 {
			errorRate = (float64(failedAuditEvents) / float64(totalAuditEvents)) * 100
		}

		// Calculate active users from messages
		activeUsersList, _ := messagesCollection.Distinct(context.Background(),
			"session_id",
			bson.M{"timestamp": bson.M{"$gte": periodStart, "$lte": periodEnd}},
		)

		c.JSON(http.StatusOK, models.UsageAnalytics{
			TotalClients:        int(totalClients),
			TotalTokensUsed:     totalTokensUsed,
			TotalMessages:       int(totalMessages),
			ActiveClients:       len(activeClients),
			ActiveUsers:         len(activeUsersList),
			ClientStats:         clientStats,
			DailyUsage:          dailyUsage,
			HourlyUsage:         hourlyUsage,
			SystemUptime:        systemUptime,
			AverageResponseTime: averageResponseTime,
			ErrorRate:           errorRate,
			PeriodStart:         periodStart,
			PeriodEnd:           periodEnd,
		})
	})

	// ------------------------------------------------
	// NEW: Lightweight system stats for Admin dashboard
	// ------------------------------------------------
	admin.GET("/stats", func(c *gin.Context) {
		dbStatus := "ok"
		if err := mongoClient.Ping(context.Background(), nil); err != nil {
			dbStatus = "unreachable"
		}
		geminiStatus := "missing_key"
		if cfg.GeminiAPIKey != "" {
			geminiStatus = "configured"
		}

		now := time.Now()
		last24h := now.Add(-24 * time.Hour)
		last30m := now.Add(-30 * time.Minute)

		totalClients, _ := clientsCollection.CountDocuments(context.Background(), bson.M{})
		totalUsers, _ := usersCollection.CountDocuments(context.Background(), bson.M{})
		totalPDFs, _ := pdfsCollection.CountDocuments(context.Background(), bson.M{})
		totalMessages, _ := messagesCollection.CountDocuments(context.Background(), bson.M{})
		msgLast24h, _ := messagesCollection.CountDocuments(context.Background(), bson.M{"timestamp": bson.M{"$gte": last24h}})

		activeUsersIDs, _ := messagesCollection.Distinct(
			context.Background(),
			"from_user_id",
			bson.M{"timestamp": bson.M{"$gte": last30m}},
		)

		// total tokens used across clients
		pipeline := mongo.Pipeline{
			{primitive.E{Key: "$group", Value: bson.D{
				primitive.E{Key: "_id", Value: nil},
				primitive.E{Key: "total_tokens", Value: bson.D{primitive.E{Key: "$sum", Value: "$token_used"}}},
			}}},
		}
		cur, _ := clientsCollection.Aggregate(context.Background(), pipeline)
		totalTokens := 0
		if cur.Next(context.Background()) {
			var agg struct {
				TotalTokens int `bson:"total_tokens"`
			}
			_ = cur.Decode(&agg)
			totalTokens = agg.TotalTokens
		}
		_ = cur.Close(context.Background())

		health := models.SystemHealth{
			Status:         "ok",
			Timestamp:      now.Format(time.RFC3339),
			Database:       dbStatus,
			GeminiAPI:      geminiStatus,
			ActiveSessions: len(activeUsersIDs),
			Metrics: map[string]interface{}{
				"clients":           totalClients,
				"users":             totalUsers,
				"pdfs":              totalPDFs,
				"messages":          totalMessages,
				"messages_last_24h": msgLast24h,
				"total_tokens_used": totalTokens,
			},
		}
		c.JSON(http.StatusOK, health)
	})

	// -------------------------
	// Embed snippet
	// -------------------------
	admin.GET("/client/:id/embed-snippet", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error_code": "invalid_client_id", "message": "Invalid client ID format"})
			return
		}

		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error_code": "client_not_found", "message": "Client not found"})
			return
		}

		baseURL := c.Request.Host
		if c.Request.TLS == nil {
			baseURL = "http://" + baseURL
		} else {
			baseURL = "https://" + baseURL
		}

		scriptTag := `<script>
			(function() {
				var chatbot = document.createElement('iframe');
				chatbot.src = '` + baseURL + `/embed/chatframe/` + client.ID.Hex() + `?secret=` + client.EmbedSecret + `';
				chatbot.style.cssText = 'position: fixed; bottom: 20px; right: 20px; width: 350px; height: 500px; border: none; border-radius: 10px; box-shadow: 0 4px 20px rgba(0,0,0,0.15); z-index: 9999;';
				document.body.appendChild(chatbot);
			})();
		</script>`

		iframeTag := `<iframe src="` + baseURL + `/embed/chatframe/` + client.ID.Hex() + `?secret=` + client.EmbedSecret + `" 
			width="350" height="500" style="border: none; border-radius: 10px; box-shadow: 0 4px 20px rgba(0,0,0,0.15);"></iframe>`

		c.JSON(http.StatusOK, models.EmbedSnippet{
			ClientID:    client.ID.Hex(),
			EmbedSecret: client.EmbedSecret,
			ScriptTag:   scriptTag,
			IframeTag:   iframeTag,
		})
	})

	// Update client endpoint
	admin.PUT("/client/:id", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var updateData map[string]interface{}

		if err := c.ShouldBindJSON(&updateData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_data",
				"message":    "Invalid request data",
			})
			return
		}

		// Check if client exists
		var existingClient models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&existingClient); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Get the initial user for this client (to exclude from email/phone validation)
		var initialUser models.User
		var initialUserID *primitive.ObjectID
		err = usersCollection.FindOne(
			context.Background(),
			bson.M{"client_id": clientID},
			options.FindOne().SetSort(bson.M{"created_at": 1}),
		).Decode(&initialUser)
		if err == nil {
			initialUserID = &initialUser.ID
		}

		// Validate email uniqueness if contact_email is being updated
		if contactEmail, ok := updateData["contact_email"]; ok && contactEmail != nil {
			emailStr, ok := contactEmail.(string)
			if ok && emailStr != "" {
				// Only validate if email is different from current initial user's email
				shouldValidateEmail := true
				if initialUserID != nil && initialUser.Email != "" {
					// If the email is the same as current initial user's email, skip validation
					if emailStr == initialUser.Email {
						shouldValidateEmail = false
					}
				}
				
				if shouldValidateEmail {
					// Check if email exists for another user (excluding initial user)
					emailQuery := bson.M{"email": emailStr}
					if initialUserID != nil {
						emailQuery["_id"] = bson.M{"$ne": initialUserID}
					}
					var existingUser models.User
					if err := usersCollection.FindOne(context.Background(), emailQuery).Decode(&existingUser); err == nil {
						c.JSON(http.StatusConflict, gin.H{
							"error_code": "email_exists",
							"message":    "This email is already registered to another user",
						})
						return
					}
				}
			}
		}

		// Validate phone uniqueness if contact_phone is being updated
		if contactPhone, ok := updateData["contact_phone"]; ok && contactPhone != nil {
			phoneStr, ok := contactPhone.(string)
			if ok && phoneStr != "" {
				// Only validate if phone is different from current initial user's phone
				shouldValidatePhone := true
				if initialUserID != nil && initialUser.Phone != "" {
					// If the phone is the same as current initial user's phone, skip validation
					if phoneStr == initialUser.Phone {
						shouldValidatePhone = false
					}
				}
				
				if shouldValidatePhone {
					// Check if phone exists for another user (excluding initial user)
					phoneQuery := bson.M{"phone": phoneStr}
					if initialUserID != nil {
						phoneQuery["_id"] = bson.M{"$ne": initialUserID}
					}
					var existingUser models.User
					if err := usersCollection.FindOne(context.Background(), phoneQuery).Decode(&existingUser); err == nil {
						c.JSON(http.StatusConflict, gin.H{
							"error_code": "phone_exists",
							"message":    "This phone number is already registered to another user",
						})
						return
					}
				}
			}
		}

		// Prepare update document - only include fields that were actually sent
		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		// Track if we need to update initial user's email/phone
		var updateInitialUserEmail string
		var updateInitialUserPhone string

		// Only add fields that are present in the request
		if name, ok := updateData["name"]; ok && name != nil {
			update["$set"].(bson.M)["name"] = name
		}
		if contactEmail, ok := updateData["contact_email"]; ok && contactEmail != nil {
			emailStr, ok := contactEmail.(string)
			if ok {
				update["$set"].(bson.M)["contact_email"] = emailStr
				// Also update initial user's email if initial user exists
				if initialUserID != nil {
					updateInitialUserEmail = emailStr
				}
			}
		}
		if contactPhone, ok := updateData["contact_phone"]; ok && contactPhone != nil {
			phoneStr, ok := contactPhone.(string)
			if ok {
				update["$set"].(bson.M)["contact_phone"] = phoneStr
				// Also update initial user's phone if initial user exists
				if initialUserID != nil {
					updateInitialUserPhone = phoneStr
				}
			}
		}
		if status, ok := updateData["status"]; ok && status != nil && status != "" {
			update["$set"].(bson.M)["status"] = status
		}
		if tokenLimit, ok := updateData["token_limit"]; ok && tokenLimit != nil {
			update["$set"].(bson.M)["token_limit"] = tokenLimit
		}
		if branding, ok := updateData["branding"]; ok && branding != nil {
			update["$set"].(bson.M)["branding"] = branding
		}

		// Update client
		_, err = clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to update client",
			})
			return
		}

		// Update initial user's email and phone if they were changed
		if initialUserID != nil && (updateInitialUserEmail != "" || updateInitialUserPhone != "") {
			userUpdate := bson.M{
				"$set": bson.M{
					"updated_at": time.Now(),
				},
			}
			if updateInitialUserEmail != "" {
				userUpdate["$set"].(bson.M)["email"] = updateInitialUserEmail
			}
			if updateInitialUserPhone != "" {
				userUpdate["$set"].(bson.M)["phone"] = updateInitialUserPhone
			}
			_, err = usersCollection.UpdateOne(context.Background(), bson.M{"_id": initialUserID}, userUpdate)
			if err != nil {
				// Log error but don't fail the request - client was already updated
				fmt.Printf("Warning: Failed to update initial user email/phone: %v\n", err)
			}
		}

		// Fetch updated client
		var updatedClient models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&updatedClient); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch updated client",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Client updated successfully",
			"client":  updatedClient,
		})
	})

	// -------------------------
	// Domain Management Endpoints
	// -------------------------

	// Get domain management settings for a client
	admin.GET("/client/:id/domains", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		fmt.Printf("🔍 Fetching domain settings for client: %s\n", clientID.Hex())

		var client struct {
			ID                primitive.ObjectID `bson:"_id" json:"id"`
			Name              string             `bson:"name" json:"name"`
			DomainWhitelist   []string           `bson:"domain_whitelist" json:"domain_whitelist"`
			DomainBlacklist   []string           `bson:"domain_blacklist" json:"domain_blacklist"`
			DomainMode        string             `bson:"domain_mode" json:"domain_mode"`
			RequireDomainAuth bool               `bson:"require_domain_auth" json:"require_domain_auth"`
		}

		err = clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error_code": "internal_error",
					"message":    "Failed to fetch client domain settings",
				})
			}
			return
		}

		fmt.Printf("📋 Client domain settings: whitelist=%v, blacklist=%v, mode=%s, auth=%t\n",
			client.DomainWhitelist, client.DomainBlacklist, client.DomainMode, client.RequireDomainAuth)

		c.JSON(http.StatusOK, gin.H{
			"client_id":           client.ID.Hex(),
			"client_name":         client.Name,
			"domain_whitelist":    client.DomainWhitelist,
			"domain_blacklist":    client.DomainBlacklist,
			"domain_mode":         client.DomainMode,
			"require_domain_auth": client.RequireDomainAuth,
		})
	})

	// Update domain management settings for a client
	admin.PUT("/client/:id/domains", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		fmt.Printf("🔧 Updating domain settings for client: %s\n", clientID.Hex())

		var req models.DomainManagementRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			fmt.Printf("❌ Failed to bind JSON: %v\n", err)
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    err.Error(),
			})
			return
		}

		authValue := false
		if req.RequireDomainAuth != nil {
			authValue = *req.RequireDomainAuth
		}
		fmt.Printf("📝 Request data: whitelist=%v, blacklist=%v, mode=%s, auth=%t\n",
			req.DomainWhitelist, req.DomainBlacklist, req.DomainMode, authValue)

		// Validate domain mode
		if req.DomainMode != "" && req.DomainMode != "whitelist" && req.DomainMode != "blacklist" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_domain_mode",
				"message":    "Domain mode must be 'whitelist' or 'blacklist'",
			})
			return
		}

		// Build update document
		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if req.DomainWhitelist != nil {
			update["$set"].(bson.M)["domain_whitelist"] = req.DomainWhitelist
		}
		if req.DomainBlacklist != nil {
			update["$set"].(bson.M)["domain_blacklist"] = req.DomainBlacklist
		}
		if req.DomainMode != "" {
			update["$set"].(bson.M)["domain_mode"] = req.DomainMode
		}
		if req.RequireDomainAuth != nil {
			update["$set"].(bson.M)["require_domain_auth"] = *req.RequireDomainAuth
		}

		// Update client
		result, err := clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to update client domain settings",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":   "Domain settings updated successfully",
			"client_id": clientID.Hex(),
		})
	})

	// Get suspicious activity alerts
	admin.GET("/alerts", func(c *gin.Context) {
		// Get query parameters
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		severity := c.Query("severity")
		resolved := c.Query("resolved")

		// Build filter
		filter := bson.M{}
		if severity != "" {
			filter["severity"] = severity
		}
		if resolved != "" {
			filter["resolved"] = resolved == "true"
		}

		// Calculate skip
		skip := (page - 1) * limit

		// Find alerts
		opts := options.Find().
			SetSort(bson.M{"created_at": -1}).
			SetSkip(int64(skip)).
			SetLimit(int64(limit))

		cursor, err := alertsCollection.Find(context.Background(), filter, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to fetch alerts",
			})
			return
		}
		defer cursor.Close(context.Background())

		var alerts []models.SuspiciousActivityAlert
		if err := cursor.All(context.Background(), &alerts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to decode alerts",
			})
			return
		}

		// Get total count
		totalCount, err := alertsCollection.CountDocuments(context.Background(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to count alerts",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"alerts":      alerts,
			"total_count": totalCount,
			"page":        page,
			"limit":       limit,
			"total_pages": (totalCount + int64(limit) - 1) / int64(limit),
		})
	})

	// Mark alert as resolved
	admin.PUT("/alerts/:id/resolve", func(c *gin.Context) {
		alertID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_alert_id",
				"message":    "Invalid alert ID format",
			})
			return
		}

		// Get admin user ID from context (you'll need to implement this)
		adminUserID := "admin" // This should come from the auth context

		update := bson.M{
			"$set": bson.M{
				"resolved":    true,
				"resolved_at": time.Now(),
				"resolved_by": adminUserID,
			},
		}

		result, err := alertsCollection.UpdateOne(context.Background(), bson.M{"_id": alertID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to resolve alert",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "alert_not_found",
				"message":    "Alert not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Alert resolved successfully",
		})
	})

	// ===== AI PERSONA MANAGEMENT =====

	// Upload AI Persona file
	admin.POST("/client/:id/ai-persona", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Get the uploaded file
		file, err := c.FormFile("persona_file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "no_file_uploaded",
				"message":    "No file uploaded",
			})
			return
		}

		// Validate file type
		allowedExts := []string{".pdf", ".doc", ".docx"}
		fileExt := ""
		for i := len(file.Filename) - 1; i >= 0; i-- {
			if file.Filename[i] == '.' {
				fileExt = "." + file.Filename[i+1:]
				break
			}
		}

		isValidExt := false
		for _, ext := range allowedExts {
			if fileExt == ext {
				isValidExt = true
				break
			}
		}

		if !isValidExt {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_file_type",
				"message":    "File must be .pdf, .doc, or .docx",
			})
			return
		}

		// Validate file size (max 20MB)
		if file.Size > 20*1024*1024 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "file_too_large",
				"message":    "File size exceeds 20MB limit",
			})
			return
		}

		// Open the uploaded file
		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_open_error",
				"message":    "Failed to open uploaded file",
			})
			return
		}
		defer src.Close()

		// Read file content into buffer
		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, src)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_read_error",
				"message":    "Failed to read uploaded file",
			})
			return
		}

		// Create temporary file for extraction
		tempDir := os.TempDir()
		tempFile := filepath.Join(tempDir, fmt.Sprintf("persona_%s_%s", clientID.Hex(), file.Filename))
		tempFileHandle, err := os.Create(tempFile)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "temp_file_error",
				"message":    "Failed to create temporary file",
			})
			return
		}

		_, err = tempFileHandle.Write(buf.Bytes())
		tempFileHandle.Close()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "temp_file_write_error",
				"message":    "Failed to write temporary file",
			})
			return
		}
		defer os.Remove(tempFile) // Clean up temp file

		// Extract content from file based on file type
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var extractedContent string
		var pages, wordCount, charCount int

		// Detect file type by extension
		isDocx := strings.HasSuffix(strings.ToLower(file.Filename), ".docx")
		isDoc := strings.HasSuffix(strings.ToLower(file.Filename), ".doc")
		isPdf := strings.HasSuffix(strings.ToLower(file.Filename), ".pdf")

		if isDocx || isDoc {
			// For DOCX/DOC files, use Gemini File API
			mimeType := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			if isDoc {
				mimeType = "application/msword"
			}

			fileType := "DOCX"
			if isDoc {
				fileType = "DOC"
			}
			fmt.Printf("Detected %s file, using Gemini extraction...\n", fileType)

			// Read file content
			fileContent, err := os.ReadFile(tempFile)
			if err != nil {
				extractedContent = fmt.Sprintf("Failed to read file: %v", err)
			} else {
				// Initialize Gemini client
				geminiClient, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
				if err != nil {
					extractedContent = fmt.Sprintf("Failed to initialize Gemini client: %v", err)
				} else {
					defer geminiClient.Close()

					// Upload file to Gemini
					uploadedFile, err := geminiClient.UploadFile(ctx, "", bytes.NewReader(fileContent), &genai.UploadFileOptions{
						MIMEType: mimeType,
					})
					if err != nil {
						extractedContent = fmt.Sprintf("Failed to upload file to Gemini: %v", err)
					} else {
						defer geminiClient.DeleteFile(ctx, uploadedFile.Name)

						// Configure model
						model := geminiClient.GenerativeModel("gemini-2.0-flash")
						model.SetTemperature(0.1)
						model.SystemInstruction = &genai.Content{
							Parts: []genai.Part{genai.Text(`You are a precise document text extractor. Extract ALL text content from this document exactly as it appears, maintaining original formatting, line breaks, and structure. Do not summarize, interpret, or modify the content. Include headers, footers, captions, and all readable text elements. Preserve the document's natural flow and organization.`)},
						}

						// Extract text
						resp, err := model.GenerateContent(ctx,
							genai.FileData{URI: uploadedFile.URI},
							genai.Text("Extract all text content from this document. Maintain original formatting and structure."),
						)
						if err != nil {
							extractedContent = fmt.Sprintf("Failed to extract text from document: %v", err)
						} else if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
							// Extract text from response
							for _, part := range resp.Candidates[0].Content.Parts {
								if textPart, ok := part.(genai.Text); ok {
									extractedContent += string(textPart)
								}
							}

							// Calculate word and character counts
							charCount = len(extractedContent)
							wordCount = len(strings.Fields(extractedContent))
							pages = 1 // DOCX doesn't have page count in traditional sense
						}
					}
				}
			}
		} else if isPdf {
			// For PDF files, use existing PDFExtractor
			extractor := services.NewPDFExtractor(cfg)
			result, err := extractor.ExtractText(ctx, tempFile)
			if err != nil {
				fmt.Printf("Warning: Failed to extract text from persona file: %v\n", err)
				extractedContent = fmt.Sprintf("Failed to extract content: %v", err)
			} else {
				extractedContent = result.Text
				pages = result.Pages
				wordCount = result.WordCount
				charCount = result.CharacterCount
			}
		} else {
			extractedContent = fmt.Sprintf("Unsupported file type: %s", fileExt)
		}

		// Save file info and content to database
		personaData := models.AIPersonaData{
			Filename:       file.Filename,
			Size:           file.Size,
			UploadedAt:     time.Now(),
			Content:        extractedContent,
			Pages:          pages,
			WordCount:      wordCount,
			CharacterCount: charCount,
		}

		update := bson.M{
			"$set": bson.M{
				"ai_persona": personaData,
				"updated_at": time.Now(),
			},
		}

		_, err = clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to save AI persona info",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "AI Persona uploaded successfully",
			"filename":   file.Filename,
			"client_id":  clientID.Hex(),
			"extracted":  wordCount > 0,
			"word_count": wordCount,
		})
	})

	// Get AI Persona info
	admin.GET("/client/:id/ai-persona", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Return null if no persona exists
		if client.AIPersona == nil {
			c.JSON(http.StatusOK, gin.H{
				"ai_persona": nil,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"ai_persona": client.AIPersona,
		})
	})

	// Delete AI Persona
	admin.DELETE("/client/:id/ai-persona", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		update := bson.M{
			"$unset": bson.M{
				"ai_persona": "",
			},
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		_, err = clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to delete AI persona",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "AI Persona deleted successfully",
		})
	})

	// ===================
	// DEFAULT PERSONA MANAGEMENT (Layer 1)
	// ===================
	systemSettingsCollection := db.Collection("system_settings")

	// Upload Default Persona
	admin.POST("/default-persona", func(c *gin.Context) {
		// Get the uploaded file
		file, err := c.FormFile("persona_file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "no_file_uploaded",
				"message":    "No file uploaded",
			})
			return
		}

		// Validate file type
		allowedExts := []string{".pdf", ".doc", ".docx"}
		fileExt := ""
		for i := len(file.Filename) - 1; i >= 0; i-- {
			if file.Filename[i] == '.' {
				fileExt = "." + file.Filename[i+1:]
				break
			}
		}

		isValidExt := false
		for _, ext := range allowedExts {
			if fileExt == ext {
				isValidExt = true
				break
			}
		}

		if !isValidExt {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_file_type",
				"message":    "File must be .pdf, .doc, or .docx",
			})
			return
		}

		// Validate file size (max 20MB)
		if file.Size > 20*1024*1024 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "file_too_large",
				"message":    "File size exceeds 20MB limit",
			})
			return
		}

		// Open the uploaded file
		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_open_error",
				"message":    "Failed to open uploaded file",
			})
			return
		}
		defer src.Close()

		// Read file content into buffer
		buf := new(bytes.Buffer)
		_, err = io.Copy(buf, src)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "file_read_error",
				"message":    "Failed to read uploaded file",
			})
			return
		}

		// Create temporary file for extraction
		tempDir := os.TempDir()
		tempFile := filepath.Join(tempDir, fmt.Sprintf("default_persona_%s", file.Filename))
		tempFileHandle, err := os.Create(tempFile)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "temp_file_error",
				"message":    "Failed to create temporary file",
			})
			return
		}

		_, err = tempFileHandle.Write(buf.Bytes())
		tempFileHandle.Close()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "temp_file_write_error",
				"message":    "Failed to write temporary file",
			})
			return
		}
		defer os.Remove(tempFile) // Clean up temp file

		// Extract content from file based on file type
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		var extractedContent string
		var pages, wordCount, charCount int

		// Detect file type by extension
		isDocx := strings.HasSuffix(strings.ToLower(file.Filename), ".docx")
		isDoc := strings.HasSuffix(strings.ToLower(file.Filename), ".doc")
		isPdf := strings.HasSuffix(strings.ToLower(file.Filename), ".pdf")

		if isDocx || isDoc {
			// For DOCX/DOC files, use Gemini File API
			mimeType := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
			if isDoc {
				mimeType = "application/msword"
			}

			fileType := "DOCX"
			if isDoc {
				fileType = "DOC"
			}
			fmt.Printf("Detected %s file, using Gemini extraction...\n", fileType)

			// Read file content
			fileContent, err := os.ReadFile(tempFile)
			if err != nil {
				extractedContent = fmt.Sprintf("Failed to read file: %v", err)
			} else {
				// Initialize Gemini client
				geminiClient, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
				if err != nil {
					extractedContent = fmt.Sprintf("Failed to initialize Gemini client: %v", err)
				} else {
					defer geminiClient.Close()

					// Upload file to Gemini
					uploadedFile, err := geminiClient.UploadFile(ctx, "", bytes.NewReader(fileContent), &genai.UploadFileOptions{
						MIMEType: mimeType,
					})
					if err != nil {
						extractedContent = fmt.Sprintf("Failed to upload file to Gemini: %v", err)
					} else {
						defer geminiClient.DeleteFile(ctx, uploadedFile.Name)

						// Configure model
						model := geminiClient.GenerativeModel("gemini-2.0-flash")
						model.SetTemperature(0.1)
						model.SystemInstruction = &genai.Content{
							Parts: []genai.Part{genai.Text(`You are a precise document text extractor. Extract ALL text content from this document exactly as it appears, maintaining original formatting, line breaks, and structure. Do not summarize, interpret, or modify the content. Include headers, footers, captions, and all readable text elements. Preserve the document's natural flow and organization.`)},
						}

						// Extract text
						resp, err := model.GenerateContent(ctx,
							genai.FileData{URI: uploadedFile.URI},
							genai.Text("Extract all text content from this document. Maintain original formatting and structure."),
						)
						if err != nil {
							extractedContent = fmt.Sprintf("Failed to extract text from document: %v", err)
						} else if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
							// Extract text from response
							for _, part := range resp.Candidates[0].Content.Parts {
								if textPart, ok := part.(genai.Text); ok {
									extractedContent += string(textPart)
								}
							}

							// Calculate word and character counts
							charCount = len(extractedContent)
							wordCount = len(strings.Fields(extractedContent))
							pages = 1 // DOCX doesn't have page count in traditional sense
						}
					}
				}
			}
		} else if isPdf {
			// For PDF files, use existing PDFExtractor
			extractor := services.NewPDFExtractor(cfg)
			result, err := extractor.ExtractText(ctx, tempFile)
			if err != nil {
				fmt.Printf("Warning: Failed to extract text from default persona file: %v\n", err)
				extractedContent = fmt.Sprintf("Failed to extract content: %v", err)
			} else {
				extractedContent = result.Text
				pages = result.Pages
				wordCount = result.WordCount
				charCount = result.CharacterCount
			}
		} else {
			extractedContent = fmt.Sprintf("Unsupported file type: %s", fileExt)
		}

		// Save file info and content to database
		personaData := models.AIPersonaData{
			Filename:       file.Filename,
			Size:           file.Size,
			UploadedAt:     time.Now(),
			Content:        extractedContent,
			Pages:          pages,
			WordCount:      wordCount,
			CharacterCount: charCount,
		}

		// Store in system_settings collection with key "default_persona"
		filter := bson.M{"key": "default_persona"}
		update := bson.M{
			"$set": bson.M{
				"key":        "default_persona",
				"value":      personaData,
				"updated_at": time.Now(),
			},
			"$setOnInsert": bson.M{
				"created_at": time.Now(),
			},
		}

		_, err = systemSettingsCollection.UpdateOne(context.Background(), filter, update, options.Update().SetUpsert(true))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to save default persona",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "Default Persona uploaded successfully",
			"filename":   file.Filename,
			"extracted":  wordCount > 0,
			"word_count": wordCount,
		})
	})

	// Get Default Persona info
	admin.GET("/default-persona", func(c *gin.Context) {
		var settingDoc bson.M
		err := systemSettingsCollection.FindOne(context.Background(), bson.M{"key": "default_persona"}).Decode(&settingDoc)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusOK, gin.H{
					"default_persona": nil,
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve default persona",
			})
			return
		}

		// Extract persona data from document
		valueRaw, ok := settingDoc["value"]
		if !ok || valueRaw == nil {
			c.JSON(http.StatusOK, gin.H{
				"default_persona": nil,
			})
			return
		}

		// Convert to AIPersonaData
		var personaData models.AIPersonaData
		personaBytes, _ := bson.Marshal(valueRaw)
		bson.Unmarshal(personaBytes, &personaData)

		c.JSON(http.StatusOK, gin.H{
			"default_persona": personaData,
		})
	})

	// Delete Default Persona
	admin.DELETE("/default-persona", func(c *gin.Context) {
		_, err := systemSettingsCollection.DeleteOne(context.Background(), bson.M{"key": "default_persona"})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to delete default persona",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Default Persona deleted successfully",
		})
	})

	// ===== SUPERADMIN-ONLY ROUTES =====
	// SuperAdmin-only routes group
	superAdmin := admin.Group("/system")
	superAdmin.Use(roleMiddleware.SuperAdminGuard()) // Only superadmin

	// Create admin user (superadmin only)
	superAdmin.POST("/users/admin", func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required,min=3,max=50"`
			Name     string `json:"name" binding:"required,min=2,max=100"`
			Email    string `json:"email,omitempty"`
			Phone    string `json:"phone,omitempty"`
			Password string `json:"password" binding:"required,min=8,max=128"`
			Role     string `json:"role" binding:"required,oneof=superadmin admin"`
		}

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

		// Create admin/superadmin user (no client_id)
		user := models.User{
			Username:     req.Username,
			Name:         req.Name,
			Email:        req.Email,
			Phone:        req.Phone,
			PasswordHash: hashedPassword,
			Role:         req.Role,
			ClientID:     nil, // Admin/superadmin has no client_id
			TokenUsage:   0,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		result, err := usersCollection.InsertOne(context.Background(), user)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to create admin user",
			})
			return
		}

		userID := result.InsertedID.(primitive.ObjectID).Hex()

		c.JSON(http.StatusCreated, gin.H{
			"message": "Admin user created successfully",
			"user": models.UserInfo{
				ID:       userID,
				Username: user.Username,
				Name:     user.Name,
				Email:    user.Email,
				Phone:    user.Phone,
				Role:     user.Role,
				ClientID: "", // Admin has no client_id
			},
		})
	})

	// Change user role (superadmin only)
	superAdmin.PATCH("/users/:id/role", func(c *gin.Context) {
		userID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
			})
			return
		}

		var req struct {
			Role string `json:"role" binding:"required,oneof=superadmin admin client visitor"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Get current user ID from context
		currentUserID := c.GetString("user_id")
		if currentUserID == userID.Hex() {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Cannot change your own role",
			})
			return
		}

		// Get target user
		var targetUser models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"_id": userID}).Decode(&targetUser); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		// Check if trying to demote last superadmin
		if targetUser.Role == "superadmin" && req.Role != "superadmin" {
			count, err := usersCollection.CountDocuments(context.Background(), bson.M{"role": "superadmin"})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error_code": "internal_error",
					"message":    "Failed to check superadmin count",
				})
				return
			}
			if count <= 1 {
				c.JSON(http.StatusForbidden, gin.H{
					"error_code": "forbidden",
					"message":    "Cannot demote last superadmin",
				})
				return
			}
		}

		// Update user role
		update := bson.M{
			"$set": bson.M{
				"role":      req.Role,
				"updated_at": time.Now(),
			},
		}

		// If changing to client/visitor, keep client_id; if changing to admin/superadmin, remove client_id
		if req.Role == "admin" || req.Role == "superadmin" {
			update["$set"].(bson.M)["client_id"] = nil
		}

		result, err := usersCollection.UpdateOne(context.Background(), bson.M{"_id": userID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to update user role",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "User role updated successfully",
			"role":    req.Role,
		})
	})

	// List admin/superadmin users (superadmin only)
	superAdmin.GET("/users/admins", func(c *gin.Context) {
		filter := bson.M{
			"role": bson.M{"$in": []string{"superadmin", "admin"}},
		}

		cursor, err := usersCollection.Find(context.Background(), filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to fetch admin users",
			})
			return
		}
		defer cursor.Close(context.Background())

		var users []models.UserInfo
		for cursor.Next(context.Background()) {
			var user models.User
			if err := cursor.Decode(&user); err != nil {
				continue
			}

			clientIDStr := ""
			if user.ClientID != nil {
				clientIDStr = user.ClientID.Hex()
			}

			users = append(users, models.UserInfo{
				ID:       user.ID.Hex(),
				Username: user.Username,
				Name:     user.Name,
				Email:    user.Email,
				Phone:    user.Phone,
				Role:     user.Role,
				ClientID: clientIDStr,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"users": users,
			"count": len(users),
		})
	})

	// Delete admin user (superadmin only, with restrictions)
	superAdmin.DELETE("/users/admin/:id", func(c *gin.Context) {
		userID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
			})
			return
		}

		// Get target user
		var targetUser models.User
		if err := usersCollection.FindOne(context.Background(), bson.M{"_id": userID}).Decode(&targetUser); err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		// Cannot delete superadmin users
		if targetUser.Role == "superadmin" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Cannot delete superadmin users. Demote to admin first.",
			})
			return
		}

		// Delete user
		result, err := usersCollection.DeleteOne(context.Background(), bson.M{"_id": userID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to delete admin user",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Admin user deleted successfully",
		})
	})

	// -------------------------
	// Admin Client Resource Management
	// -------------------------
	// Admin can manage all client resources (documents, branding, analytics, etc.)
	
	// Upload document for a client (admin-scoped)
	admin.POST("/client/:id/documents", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Parse multipart form with LIMITED memory (just for headers, not full file)
		const maxMemory = 32 << 20 // 32 MB
		if err := c.Request.ParseMultipartForm(maxMemory); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "parse_error",
				"message":    "Failed to parse multipart form",
			})
			return
		}

		// Get file from form (this streams the file, not loading into memory)
		file, header, err := c.Request.FormFile("pdf")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "no_file",
				"message":    "No PDF file provided",
			})
			return
		}
		defer file.Close()

		// Validate file size (check header.Size without reading file into memory)
		if header.Size > cfg.MaxFileSize {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "file_too_large",
				"message":    fmt.Sprintf("File size (%d bytes) exceeds maximum limit (%d bytes)", header.Size, cfg.MaxFileSize),
			})
			return
		}

		// Check if async processing is requested
		isAsync := c.PostForm("async") == "true"

		// Create PDF service
		pdfService := services.NewPDFService(cfg, pdfsCollection)

		// Create secure upload request
		uploadReq := &services.SecureUploadRequest{
			File:     file,
			Header:   header,
			ClientID: clientID,
			UserID:   primitive.NilObjectID, // Admin upload
			IsAsync:  isAsync,
		}

		// Process upload
		result, err := pdfService.ValidateAndProcessUpload(c.Request.Context(), uploadReq)
		if err != nil {
			fmt.Printf("❌ PDF upload failed: %s - %v\n", header.Filename, err)

			// Check for specific error types
			if strings.Contains(err.Error(), "file size") {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "file_too_large",
					"message":    err.Error(),
				})
				return
			}

			if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "corrupted") {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_file",
					"message":    err.Error(),
				})
				return
			}

			// Check if it's a quota/API limit error
			if isGeminiQuotaError(err) {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error_code": "ai_quota_exceeded",
					"message":    "Free Gemini API limit reached. Please try again in a few minutes.",
					"details": gin.H{
						"filename":  header.Filename,
						"file_size": formatBytes(header.Size),
					},
				})
				return
			}

			// General error handling
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "upload_failed",
				"message":    "Failed to process PDF upload",
				"details":    err.Error(),
			})
			return
		}

		// Prepare response
		response := models.UploadResponse{
			ID:       result.PDF.ID.Hex(),
			Filename: result.PDF.OriginalName,
			Status:   result.PDF.Status,
			Metadata: result.PDF.Metadata,
		}

		// Add chunk count if processing is completed
		if result.PDF.Status == models.StatusCompleted {
			response.ChunkCount = len(result.PDF.ContentChunks)
			response.Message = "PDF processed successfully"
		} else {
			response.Message = "PDF uploaded successfully, processing in background"
		}

		// Add task ID for async processing
		if result.TaskID != "" {
			response.TaskID = result.TaskID
		}

		fmt.Printf("✅ PDF upload successful: %s (status: %s, chunks: %d)\n",
			header.Filename, result.PDF.Status, len(result.PDF.ContentChunks))

		c.JSON(http.StatusOK, response)
	})

	// Delete client document (admin-scoped)
	admin.DELETE("/client/:id/documents/:documentId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		documentID, err := primitive.ObjectIDFromHex(c.Param("documentId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_document_id",
				"message":    "Invalid document ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Check if document exists and belongs to the client
		var pdfDoc models.PDF
		err = pdfsCollection.FindOne(context.Background(), bson.M{
			"_id":       documentID,
			"client_id": clientID,
		}).Decode(&pdfDoc)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "document_not_found",
					"message":    "Document not found or does not belong to this client",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to check document existence",
			})
			return
		}

		// Delete the document
		deleteResult, err := pdfsCollection.DeleteOne(context.Background(), bson.M{
			"_id":       documentID,
			"client_id": clientID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "delete_failed",
				"message":    "Failed to delete document",
			})
			return
		}

		if deleteResult.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "document_not_found",
				"message":    "Document not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "Document deleted successfully",
			"document_id":   c.Param("documentId"),
			"filename":      pdfDoc.Filename,
			"deleted_at":    time.Now().UTC(),
			"deleted_count": deleteResult.DeletedCount,
		})
	})

	// Bulk delete client documents (admin-scoped)
	admin.DELETE("/client/:id/documents/bulk", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		var request struct {
			PdfIDs []string `json:"pdf_ids" binding:"required"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		if len(request.PdfIDs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "empty_pdf_list",
				"message":    "At least one PDF ID is required",
			})
			return
		}

		// Convert string IDs to ObjectIDs
		var pdfObjIDs []primitive.ObjectID
		for _, pdfID := range request.PdfIDs {
			objID, err := primitive.ObjectIDFromHex(pdfID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_pdf_id",
					"message":    fmt.Sprintf("Invalid PDF ID format: %s", pdfID),
				})
				return
			}
			pdfObjIDs = append(pdfObjIDs, objID)
		}

		// Delete multiple PDFs
		deleteResult, err := pdfsCollection.DeleteMany(context.Background(), bson.M{
			"_id":       bson.M{"$in": pdfObjIDs},
			"client_id": clientID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "delete_failed",
				"message":    "Failed to delete documents",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "Documents deleted successfully",
			"deleted_count": deleteResult.DeletedCount,
			"deleted_at":    time.Now().UTC(),
		})
	})

	// Get client document status (admin-scoped)
	admin.GET("/client/:id/documents/:documentId/status", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		documentID, err := primitive.ObjectIDFromHex(c.Param("documentId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_document_id",
				"message":    "Invalid document ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var pdfDoc models.PDF
		err = pdfsCollection.FindOne(ctx, bson.M{
			"_id":       documentID,
			"client_id": clientID,
		}).Decode(&pdfDoc)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "document_not_found",
					"message":    "Document not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve document status",
			})
			return
		}

		response := gin.H{
			"id":           pdfDoc.ID.Hex(),
			"filename":     pdfDoc.OriginalName,
			"status":       pdfDoc.Status,
			"progress":     pdfDoc.Progress,
			"uploaded_at":  pdfDoc.UploadedAt,
			"processed_at": pdfDoc.ProcessedAt,
			"metadata":     pdfDoc.Metadata,
		}

		if pdfDoc.ErrorMessage != "" {
			response["error_message"] = pdfDoc.ErrorMessage
		}

		if pdfDoc.Status == models.StatusCompleted {
			response["chunk_count"] = len(pdfDoc.ContentChunks)
		}

		c.JSON(http.StatusOK, response)
	})
	
	// Get client documents (admin-scoped)
	admin.GET("/client/:id/documents", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Parse pagination parameters
		page := 1
		limit := 10
		if pageStr := c.Query("page"); pageStr != "" {
			if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
				page = p
			}
		}
		if limitStr := c.Query("limit"); limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		// Get total count
		total, err := pdfsCollection.CountDocuments(context.Background(), bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to count documents",
			})
			return
		}

		// Calculate skip
		skip := (page - 1) * limit

		// Get PDFs with pagination
		cursor, err := pdfsCollection.Find(
			context.Background(),
			bson.M{"client_id": clientID},
			options.Find().
				SetSort(bson.M{"uploaded_at": -1}).
				SetSkip(int64(skip)).
				SetLimit(int64(limit)),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve documents",
			})
			return
		}
		defer cursor.Close(context.Background())

		var pdfs []gin.H
		for cursor.Next(context.Background()) {
			var pdf models.PDF
			if err := cursor.Decode(&pdf); err != nil {
				continue
			}
			pdfs = append(pdfs, gin.H{
				"id":          pdf.ID.Hex(),
				"filename":    pdf.Filename,
				"status":      pdf.Status,
				"uploaded_at": pdf.UploadedAt,
				"metadata": gin.H{
					"size": pdf.Metadata.Size,
					"pages": pdf.Metadata.Pages,
				},
			})
		}

		// Return same structure as client endpoint for compatibility
		c.JSON(http.StatusOK, gin.H{
			"pdfs":        pdfs,
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		})
	})

	// Start crawl for a client (admin-scoped)
	admin.POST("/client/:id/crawls/start", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		var req struct {
			URL            string   `json:"url" binding:"required"`
			MaxPages       int      `json:"max_pages,omitempty"`
			AllowedDomains []string `json:"allowed_domains,omitempty"`
			AllowedPaths   []string `json:"allowed_paths,omitempty"`
			FollowLinks    bool     `json:"follow_links,omitempty"`
			IncludeImages  bool     `json:"include_images,omitempty"`
			RespectRobots  bool     `json:"respect_robots,omitempty"`
			RenderJS       bool     `json:"render_js,omitempty"`
			WaitSelector   string   `json:"wait_selector,omitempty"`
			RenderTimeout  int      `json:"render_timeout_ms,omitempty"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request: " + err.Error(),
			})
			return
		}

		// Create crawl job
		crawlJob := models.CrawlJob{
			ID:             primitive.NewObjectID(),
			ClientID:       clientID,
			URL:            req.URL,
			Status:         models.CrawlStatusPending,
			Progress:       0,
			PagesFound:     0,
			PagesCrawled:   0,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
			MaxPages:       req.MaxPages,
			AllowedDomains: req.AllowedDomains,
			AllowedPaths:   req.AllowedPaths,
			FollowLinks:    req.FollowLinks,
			IncludeImages:  req.IncludeImages,
			RespectRobots:  req.RespectRobots,
		}

		// Save to MongoDB
		ctx := context.Background()
		_, err = crawlsCollection.InsertOne(ctx, crawlJob)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to create crawl job: " + err.Error(),
			})
			return
		}

		// Helper functions for crawl status updates
		updateCrawlStatus := func(crawlsCollection *mongo.Collection, crawlID string, status string, progress int) {
			ctx := context.Background()
			crawlObjID, err := primitive.ObjectIDFromHex(crawlID)
			if err != nil {
				return
			}

			update := bson.M{
				"$set": bson.M{
					"status":     status,
					"progress":   progress,
					"updated_at": time.Now(),
				},
			}
			crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)
		}

		updateCrawlError := func(crawlsCollection *mongo.Collection, crawlID string, errorMsg string) {
			ctx := context.Background()
			crawlObjID, err := primitive.ObjectIDFromHex(crawlID)
			if err != nil {
				return
			}

			update := bson.M{
				"$set": bson.M{
					"error":      errorMsg,
					"status":     "failed",
					"updated_at": time.Now(),
				},
			}
			crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)
		}

		// Start crawl in background goroutine (reuse the same logic from client endpoint)
		go func() {
			startTime := time.Now()
			updateCrawlStatus(crawlsCollection, crawlJob.ID.Hex(), models.CrawlStatusCrawling, 5)

			// Configure crawler with production settings
			maxPages := req.MaxPages
			if maxPages <= 0 {
				maxPages = 50 // Default limit
			}

			crawlConfig := crawler.CrawlConfig{
				URL:            req.URL,
				MaxPages:       maxPages,
				AllowedDomains: req.AllowedDomains,
				AllowedPaths:   req.AllowedPaths,
				FollowLinks:    req.FollowLinks,
				IncludeImages:  req.IncludeImages,
				RespectRobots:  req.RespectRobots,
				Timeout:        60 * time.Second,
				RenderJS:       req.RenderJS,
				WaitSelector:   req.WaitSelector,
				RenderTimeout: time.Duration(func() int {
					if req.RenderTimeout <= 0 {
						return 45000
					}
					return req.RenderTimeout
				}()) * time.Millisecond,
				NetworkIdleAfter: 800 * time.Millisecond,
			}

			// Update progress during crawl
			updateCrawlStatus(crawlsCollection, crawlJob.ID.Hex(), models.CrawlStatusCrawling, 10)

			// Execute crawl
			result, err := crawler.CrawlURL(crawlConfig)

			if err != nil {
				// Try to get partial results if available
				if result != nil && len(result.Pages) > 0 {
					// We got some pages, save them as partial success
					crawledPages := make([]models.CrawledPage, len(result.Pages))
					copy(crawledPages, result.Pages)

					completedAt := time.Now()
					processingTime := completedAt.Sub(startTime)
					update := bson.M{
						"$set": bson.M{
							"status":          models.CrawlStatusCompleted,
							"progress":        100,
							"title":           result.Title,
							"content":         result.Content,
							"pages_found":     result.PagesFound,
							"pages_crawled":   result.PagesCrawled,
							"crawled_pages":   crawledPages,
							"error":           fmt.Sprintf("Partial success: %v", err.Error()),
							"updated_at":      time.Now(),
							"completed_at":    completedAt,
							"processing_time": processingTime,
						},
					}

					ctx := context.Background()
					crawlObjID, _ := primitive.ObjectIDFromHex(crawlJob.ID.Hex())
					crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)
					return
				}

				// Complete failure
				updateCrawlStatus(crawlsCollection, crawlJob.ID.Hex(), models.CrawlStatusFailed, 0)
				updateCrawlError(crawlsCollection, crawlJob.ID.Hex(), err.Error())
				return
			}

			// Success - convert pages to model format
			crawledPages := make([]models.CrawledPage, len(result.Pages))
			copy(crawledPages, result.Pages)

			completedAt := time.Now()
			processingTime := completedAt.Sub(startTime)
			update := bson.M{
				"$set": bson.M{
					"status":          models.CrawlStatusCompleted,
					"progress":        100,
					"title":           result.Title,
					"content":         result.Content,
					"pages_found":     result.PagesFound,
					"pages_crawled":   result.PagesCrawled,
					"crawled_pages":   crawledPages,
					"updated_at":      time.Now(),
					"completed_at":    completedAt,
					"processing_time": processingTime,
				},
			}

			ctx := context.Background()
			crawlObjID, _ := primitive.ObjectIDFromHex(crawlJob.ID.Hex())
			crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)
		}()

		c.JSON(http.StatusOK, gin.H{
			"id":       crawlJob.ID.Hex(),
			"client_id": clientID.Hex(),
			"url":      req.URL,
			"status":   crawlJob.Status,
			"message":  "Crawl job started successfully",
		})
	})

	// Bulk start crawls for a client (admin-scoped)
	admin.POST("/client/:id/crawls/bulk", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		var req struct {
			URLs           []string `json:"urls" binding:"required"`
			MaxPages       int      `json:"max_pages,omitempty"`
			AllowedDomains []string `json:"allowed_domains,omitempty"`
			AllowedPaths   []string `json:"allowed_paths,omitempty"`
			FollowLinks    bool     `json:"follow_links,omitempty"`
			IncludeImages  bool     `json:"include_images,omitempty"`
			RespectRobots  bool     `json:"respect_robots,omitempty"`
			RenderJS       bool     `json:"render_js,omitempty"`
			WaitSelector   string   `json:"wait_selector,omitempty"`
			RenderTimeout  int      `json:"render_timeout_ms,omitempty"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request: " + err.Error(),
			})
			return
		}

		// Validate and clean URLs
		validURLs := make([]string, 0, len(req.URLs))
		for _, urlStr := range req.URLs {
			urlStr = strings.TrimSpace(urlStr)
			if urlStr == "" {
				continue
			}
			if _, err := url.Parse(urlStr); err != nil {
				continue // Skip invalid URLs
			}
			validURLs = append(validURLs, urlStr)
		}

		if len(validURLs) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "No valid URLs provided",
			})
			return
		}

		// Limit bulk crawl to reasonable number
		if len(validURLs) > 100 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Maximum 100 URLs allowed per bulk crawl",
			})
			return
		}

		// Helper functions for crawl status updates
		updateCrawlStatus := func(crawlsCollection *mongo.Collection, crawlID string, status string, progress int) {
			ctx := context.Background()
			crawlObjID, err := primitive.ObjectIDFromHex(crawlID)
			if err != nil {
				return
			}

			update := bson.M{
				"$set": bson.M{
					"status":     status,
					"progress":   progress,
					"updated_at": time.Now(),
				},
			}
			crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)
		}

		updateCrawlError := func(crawlsCollection *mongo.Collection, crawlID string, errorMsg string) {
			ctx := context.Background()
			crawlObjID, err := primitive.ObjectIDFromHex(crawlID)
			if err != nil {
				return
			}

			update := bson.M{
				"$set": bson.M{
					"error":      errorMsg,
					"status":     "failed",
					"updated_at": time.Now(),
				},
			}
			crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)
		}

		// Create crawl jobs for each URL and start processing
		ctx := context.Background()
		var createdJobs []gin.H
		var crawlIDs []string

		for _, urlStr := range validURLs {
			crawlJob := models.CrawlJob{
				ID:             primitive.NewObjectID(),
				ClientID:       clientID,
				URL:            urlStr,
				Status:         models.CrawlStatusPending,
				Progress:       0,
				PagesFound:     0,
				PagesCrawled:   0,
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
				MaxPages:       req.MaxPages,
				AllowedDomains: req.AllowedDomains,
				AllowedPaths:   req.AllowedPaths,
				FollowLinks:    req.FollowLinks,
				IncludeImages:  req.IncludeImages,
				RespectRobots:  req.RespectRobots,
			}

			_, err = crawlsCollection.InsertOne(ctx, crawlJob)
			if err != nil {
				fmt.Printf("Failed to create crawl job for %s: %v\n", urlStr, err)
				continue
			}

			createdJobs = append(createdJobs, gin.H{
				"id":     crawlJob.ID.Hex(),
				"url":    crawlJob.URL,
				"status": crawlJob.Status,
			})
			crawlIDs = append(crawlIDs, crawlJob.ID.Hex())

			// Start crawl in background goroutine for each job
			go func(jobID string, jobURL string) {
				startTime := time.Now()
				updateCrawlStatus(crawlsCollection, jobID, models.CrawlStatusCrawling, 5)

				maxPages := req.MaxPages
				if maxPages <= 0 {
					maxPages = 50
				}

				crawlConfig := crawler.CrawlConfig{
					URL:            jobURL,
					MaxPages:       maxPages,
					AllowedDomains: req.AllowedDomains,
					AllowedPaths:   req.AllowedPaths,
					FollowLinks:    req.FollowLinks,
					IncludeImages:  req.IncludeImages,
					RespectRobots:  req.RespectRobots,
					Timeout:        60 * time.Second,
					RenderJS:       req.RenderJS,
					WaitSelector:   req.WaitSelector,
					RenderTimeout: time.Duration(func() int {
						if req.RenderTimeout <= 0 {
							return 45000
						}
						return req.RenderTimeout
					}()) * time.Millisecond,
					NetworkIdleAfter: 800 * time.Millisecond,
				}

				updateCrawlStatus(crawlsCollection, jobID, models.CrawlStatusCrawling, 10)

				result, err := crawler.CrawlURL(crawlConfig)

				if err != nil {
					// Try to save partial results if available
					if result != nil && len(result.Pages) > 0 {
						crawledPages := make([]models.CrawledPage, len(result.Pages))
						copy(crawledPages, result.Pages)

						completedAt := time.Now()
						processingTime := completedAt.Sub(startTime)
						update := bson.M{
							"$set": bson.M{
								"status":          models.CrawlStatusCompleted,
								"progress":        100,
								"title":           result.Title,
								"content":         result.Content,
								"pages_found":     result.PagesFound,
								"pages_crawled":   result.PagesCrawled,
								"crawled_pages":   crawledPages,
								"error":           fmt.Sprintf("Partial success: %v", err.Error()),
								"updated_at":      time.Now(),
								"completed_at":    completedAt,
								"processing_time": processingTime,
							},
						}

						ctx := context.Background()
						crawlObjID, _ := primitive.ObjectIDFromHex(jobID)
						crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)
						return
					}

					// Complete failure
					updateCrawlStatus(crawlsCollection, jobID, models.CrawlStatusFailed, 0)
					updateCrawlError(crawlsCollection, jobID, err.Error())
					return
				}

				// Success - convert pages to model format
				crawledPages := make([]models.CrawledPage, len(result.Pages))
				copy(crawledPages, result.Pages)

				completedAt := time.Now()
				processingTime := completedAt.Sub(startTime)
				update := bson.M{
					"$set": bson.M{
						"status":          models.CrawlStatusCompleted,
						"progress":        100,
						"title":           result.Title,
						"content":         result.Content,
						"pages_found":     result.PagesFound,
						"pages_crawled":   result.PagesCrawled,
						"crawled_pages":   crawledPages,
						"updated_at":      time.Now(),
						"completed_at":    completedAt,
						"processing_time": processingTime,
					},
				}

				ctx := context.Background()
				crawlObjID, _ := primitive.ObjectIDFromHex(jobID)
				crawlsCollection.UpdateOne(ctx, bson.M{"_id": crawlObjID}, update)

				fmt.Printf("✅ Crawl completed for %s: %d pages in %v\n", jobURL, result.PagesCrawled, processingTime)
			}(crawlJob.ID.Hex(), urlStr)
		}

		c.JSON(http.StatusOK, gin.H{
			"client_id": clientID.Hex(),
			"urls":     validURLs,
			"job_ids":  crawlIDs,
			"jobs":     createdJobs,
			"count":    len(crawlIDs),
			"message":  "Bulk crawl jobs started successfully",
		})
	})

	// Get client crawls (admin-scoped)
	admin.GET("/client/:id/crawls", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Parse pagination parameters
		page := 1
		limit := 10
		if pageStr := c.Query("page"); pageStr != "" {
			if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
				page = p
			}
		}
		if limitStr := c.Query("limit"); limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}

		// Get total count
		total, err := crawlsCollection.CountDocuments(context.Background(), bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to count crawls",
			})
			return
		}

		// Calculate skip
		skip := (page - 1) * limit

		// Get crawls with pagination
		cursor, err := crawlsCollection.Find(
			context.Background(),
			bson.M{"client_id": clientID},
			options.Find().
				SetSort(bson.M{"created_at": -1}).
				SetSkip(int64(skip)).
				SetLimit(int64(limit)),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve crawls",
			})
			return
		}
		defer cursor.Close(context.Background())

		var crawls []models.CrawlJob
		if err := cursor.All(context.Background(), &crawls); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to decode crawls",
			})
			return
		}

		// Return same structure as client endpoint for compatibility
		c.JSON(http.StatusOK, gin.H{
			"crawls":      crawls,
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		})
	})

	// Get client crawl by ID (admin-scoped)
	admin.GET("/client/:id/crawls/:crawlId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		crawlID, err := primitive.ObjectIDFromHex(c.Param("crawlId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_crawl_id",
				"message":    "Invalid crawl ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Get crawl
		var crawl models.CrawlJob
		err = crawlsCollection.FindOne(context.Background(), bson.M{
			"_id":       crawlID,
			"client_id": clientID,
		}).Decode(&crawl)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "crawl_not_found",
					"message":    "Crawl job not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve crawl job",
			})
			return
		}

		c.JSON(http.StatusOK, crawl)
	})

	// Get client crawl status (admin-scoped)
	admin.GET("/client/:id/crawls/:crawlId/status", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		crawlID, err := primitive.ObjectIDFromHex(c.Param("crawlId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_crawl_id",
				"message":    "Invalid crawl ID format",
			})
			return
		}

		// Get crawl status
		var crawl models.CrawlJob
		err = crawlsCollection.FindOne(context.Background(), bson.M{
			"_id":       crawlID,
			"client_id": clientID,
		}).Decode(&crawl)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "crawl_not_found",
					"message":    "Crawl job not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve crawl job",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":   crawl.Status,
			"progress": crawl.Progress,
		})
	})

	// Delete client crawl (admin-scoped)
	admin.DELETE("/client/:id/crawls/:crawlId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		crawlID, err := primitive.ObjectIDFromHex(c.Param("crawlId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_crawl_id",
				"message":    "Invalid crawl ID format",
			})
			return
		}

		// Delete crawl
		result, err := crawlsCollection.DeleteOne(context.Background(), bson.M{
			"_id":       crawlID,
			"client_id": clientID,
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to delete crawl job",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "crawl_not_found",
				"message":    "Crawl job not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Crawl job deleted successfully",
		})
	})

	// Get client branding (admin-scoped)
	admin.GET("/client/:id/branding", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"client_id": clientID.Hex(),
			"branding":  client.Branding,
		})
	})

	// Update client branding (admin-scoped)
	admin.PATCH("/client/:id/branding", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var req models.Branding
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Update client branding
		update := bson.M{
			"$set": bson.M{
				"branding":   req,
				"updated_at": time.Now(),
			},
		}

		result, err := clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to update client branding",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "Client branding updated successfully",
			"client_id":  clientID.Hex(),
			"branding":   req,
		})
	})

	// Get client analytics (admin-scoped)
	admin.GET("/client/:id/analytics", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Parse period parameter (same as client endpoint)
		period := strings.ToLower(strings.TrimSpace(c.DefaultQuery("period", "30d")))
		dur := parsePeriod(period)

		end := time.Now()
		start := end.Add(-dur)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Use the same generateAnalytics function as client endpoint
		analytics, err := generateAnalytics(ctx, messagesCollection, clientID, start, end, period)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "analytics_error",
				"message":    "Failed to generate analytics",
				"details":    err.Error(),
			})
			return
		}

		// Add token limit info from client
		analytics["token_used"] = client.TokenUsed
		analytics["token_limit"] = client.TokenLimit

		c.JSON(http.StatusOK, analytics)
	})

	// Get client token usage (admin-scoped)
	admin.GET("/client/:id/tokens", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		usagePercentage := 0.0
		if client.TokenLimit > 0 {
			usagePercentage = float64(client.TokenUsed) / float64(client.TokenLimit) * 100
		}

		remaining := client.TokenLimit - client.TokenUsed
		if remaining < 0 {
			remaining = 0
		}

		// Return same format as client endpoint for compatibility
		c.JSON(http.StatusOK, gin.H{
			"used":      client.TokenUsed,
			"limit":     client.TokenLimit,
			"remaining": remaining,
			"usage":     usagePercentage,
		})
	})

	// Get client images (admin-scoped)
	admin.GET("/client/:id/images", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cursor, err := imagesCollection.Find(ctx, bson.M{"client_id": clientID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch images",
			})
			return
		}
		defer cursor.Close(ctx)

		var images []models.Image
		if err = cursor.All(ctx, &images); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode images",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"images": images,
			"count":  len(images),
		})
	})

	// Add client image (admin-scoped)
	admin.POST("/client/:id/images", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		var req struct {
			URL   string `json:"url" binding:"required"`
			Title string `json:"title" binding:"required"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		image := models.Image{
			ID:        primitive.NewObjectID(),
			ClientID:  clientID,
			URL:       req.URL,
			Title:     req.Title,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = imagesCollection.InsertOne(ctx, image)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to add image",
			})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"message": "Image added successfully",
			"image":   image,
		})
	})

	// Delete client image (admin-scoped)
	admin.DELETE("/client/:id/images/:imageId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		imageID, err := primitive.ObjectIDFromHex(c.Param("imageId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_image_id",
				"message":    "Invalid image ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := imagesCollection.DeleteOne(ctx, bson.M{
			"_id":       imageID,
			"client_id": clientID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to delete image",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "image_not_found",
				"message":    "Image not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Image deleted successfully",
		})
	})

	// ========== FACEBOOK POSTS ENDPOINTS (admin-scoped) ==========

	// Get client Facebook posts (admin-scoped)
	admin.GET("/client/:id/facebook-posts", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cursor, err := facebookPostsCollection.Find(ctx, bson.M{"client_id": clientID}, options.Find().SetSort(bson.M{"created_at": -1}))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve Facebook posts",
			})
			return
		}
		defer cursor.Close(ctx)

		var posts []models.FacebookPost
		if err := cursor.All(ctx, &posts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode Facebook posts",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"posts": posts,
			"total": len(posts),
		})
	})

	// Add client Facebook post (admin-scoped)
	admin.POST("/client/:id/facebook-posts", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		var req struct {
			PostURL string `json:"post_url" binding:"required"`
			Title   string `json:"title,omitempty"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate Facebook post URL
		parsedURL, err := url.Parse(req.PostURL)
		if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_url",
				"message":    "Invalid Facebook post URL format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		post := models.FacebookPost{
			ID:        primitive.NewObjectID(),
			ClientID:  clientID,
			PostURL:   req.PostURL,
			Title:     req.Title,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = facebookPostsCollection.InsertOne(ctx, post)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to add Facebook post",
			})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"message": "Facebook post added successfully",
			"post":    post,
		})
	})

	// Delete client Facebook post (admin-scoped)
	admin.DELETE("/client/:id/facebook-posts/:postId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		postID, err := primitive.ObjectIDFromHex(c.Param("postId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_post_id",
				"message":    "Invalid post ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := facebookPostsCollection.DeleteOne(ctx, bson.M{
			"_id":       postID,
			"client_id": clientID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to delete Facebook post",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "post_not_found",
				"message":    "Facebook post not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Facebook post deleted successfully",
		})
	})

	// Get client Facebook posts config (admin-scoped)
	admin.GET("/client/:id/facebook-posts-config", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var client models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Facebook posts configuration",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"facebook_posts_enabled": client.FacebookPostsEnabled,
		})
	})

	// Update client Facebook posts config (admin-scoped)
	admin.POST("/client/:id/facebook-posts-config", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var request struct {
			FacebookPostsEnabled *bool `json:"facebook_posts_enabled,omitempty"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if request.FacebookPostsEnabled != nil {
			update["$set"].(bson.M)["facebook_posts_enabled"] = *request.FacebookPostsEnabled
		}

		result, err := clientsCollection.UpdateOne(
			ctx,
			bson.M{"_id": clientID},
			update,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update Facebook posts configuration",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Fetch updated client to return
		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch updated client",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":                "Facebook posts configuration updated successfully",
			"facebook_posts_enabled": updatedClient.FacebookPostsEnabled,
		})
	})

	// ========== INSTAGRAM POSTS ENDPOINTS (admin-scoped) ==========

	// Get client Instagram posts (admin-scoped)
	admin.GET("/client/:id/instagram-posts", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		cursor, err := instagramPostsCollection.Find(ctx, bson.M{"client_id": clientID}, options.Find().SetSort(bson.M{"created_at": -1}))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve Instagram posts",
			})
			return
		}
		defer cursor.Close(ctx)

		var posts []models.InstagramPost
		if err := cursor.All(ctx, &posts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode Instagram posts",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"posts": posts,
			"total": len(posts),
		})
	})

	// Add client Instagram post (admin-scoped)
	admin.POST("/client/:id/instagram-posts", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		var req struct {
			PostURL string `json:"post_url" binding:"required"`
			Title   string `json:"title,omitempty"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate Instagram post URL
		parsedURL, err := url.Parse(req.PostURL)
		if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_url",
				"message":    "Invalid Instagram post URL format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		post := models.InstagramPost{
			ID:        primitive.NewObjectID(),
			ClientID:  clientID,
			PostURL:   req.PostURL,
			Title:     req.Title,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err = instagramPostsCollection.InsertOne(ctx, post)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to add Instagram post",
			})
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"message": "Instagram post added successfully",
			"post":    post,
		})
	})

	// Delete client Instagram post (admin-scoped)
	admin.DELETE("/client/:id/instagram-posts/:postId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		postID, err := primitive.ObjectIDFromHex(c.Param("postId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_post_id",
				"message":    "Invalid post ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		result, err := instagramPostsCollection.DeleteOne(ctx, bson.M{
			"_id":       postID,
			"client_id": clientID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to delete Instagram post",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "post_not_found",
				"message":    "Instagram post not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Instagram post deleted successfully",
		})
	})

	// Get client Instagram posts config (admin-scoped)
	admin.GET("/client/:id/instagram-posts-config", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var client models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Instagram posts configuration",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"instagram_posts_enabled": client.InstagramPostsEnabled,
		})
	})

	// Update client Instagram posts config (admin-scoped)
	admin.POST("/client/:id/instagram-posts-config", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var request struct {
			InstagramPostsEnabled *bool `json:"instagram_posts_enabled,omitempty"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if request.InstagramPostsEnabled != nil {
			update["$set"].(bson.M)["instagram_posts_enabled"] = *request.InstagramPostsEnabled
		}

		result, err := clientsCollection.UpdateOne(
			ctx,
			bson.M{"_id": clientID},
			update,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update Instagram posts configuration",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Fetch updated client to return
		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch updated client",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":                  "Instagram posts configuration updated successfully",
			"instagram_posts_enabled": updatedClient.InstagramPostsEnabled,
		})
	})

	// Get client website embed config (admin-scoped)
	admin.GET("/client/:id/website-embed-config", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var client models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&client)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch website embed configuration",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"website_embed_enabled": client.WebsiteEmbedEnabled,
			"website_embed_url":     client.WebsiteEmbedURL,
		})
	})

	// Update client website embed config (admin-scoped)
	admin.POST("/client/:id/website-embed-config", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var request struct {
			WebsiteEmbedEnabled *bool   `json:"website_embed_enabled,omitempty"`
			WebsiteEmbedURL     *string `json:"website_embed_url,omitempty"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate URL if provided
		if request.WebsiteEmbedURL != nil && *request.WebsiteEmbedURL != "" {
			parsedURL, err := url.Parse(*request.WebsiteEmbedURL)
			if err != nil || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_url",
					"message":    "Invalid website URL format. Must be a valid HTTP or HTTPS URL",
				})
				return
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"updated_at": time.Now(),
			},
		}

		if request.WebsiteEmbedEnabled != nil {
			update["$set"].(bson.M)["website_embed_enabled"] = *request.WebsiteEmbedEnabled
		}

		if request.WebsiteEmbedURL != nil {
			update["$set"].(bson.M)["website_embed_url"] = *request.WebsiteEmbedURL
		}

		result, err := clientsCollection.UpdateOne(
			ctx,
			bson.M{"_id": clientID},
			update,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update website embed configuration",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Website embed configuration updated successfully",
		})
	})

	// Get client conversations (admin-scoped)
	admin.GET("/client/:id/conversations", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Aggregate conversations from messages - filter by client_id and ensure conversation_id exists
		pipeline := mongo.Pipeline{
			{primitive.E{Key: "$match", Value: bson.M{
				"client_id":       clientID,
				"conversation_id": bson.M{"$exists": true, "$ne": ""},
			}}},
			{primitive.E{Key: "$group", Value: bson.M{
				"_id":           "$conversation_id",
				"last_message":  bson.M{"$last": "$$ROOT"},
				"message_count": bson.M{"$sum": 1},
				"total_tokens":  bson.M{"$sum": "$token_cost"},
				"updated_at":    bson.M{"$max": "$timestamp"},
			}}},
			{primitive.E{Key: "$sort", Value: bson.M{"updated_at": -1}}},
		}

		cursor, err := messagesCollection.Aggregate(ctx, pipeline)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve conversations",
			})
			return
		}
		defer cursor.Close(ctx)

		var conversations []gin.H
		for cursor.Next(ctx) {
			var result struct {
				ID           string         `bson:"_id"`
				LastMessage  models.Message `bson:"last_message"`
				MessageCount int            `bson:"message_count"`
				TotalTokens  int            `bson:"total_tokens"`
				UpdatedAt    time.Time      `bson:"updated_at"`
			}
			if err := cursor.Decode(&result); err != nil {
				continue
			}

			// Skip if conversation_id is empty or last message doesn't belong to this client
			if result.ID == "" || result.LastMessage.ClientID != clientID {
				continue
			}

			conversations = append(conversations, gin.H{
				"conversation_id": result.ID,
				"last_message":    result.LastMessage,
				"message_count": result.MessageCount,
				"total_tokens":  result.TotalTokens,
				"updated_at":    result.UpdatedAt,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"conversations": conversations,
			"total":         len(conversations),
		})
	})

	// Get client conversation by ID (admin-scoped)
	admin.GET("/client/:id/conversations/:conversationId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		conversationID := c.Param("conversationId")
		if conversationID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_conversation_id",
				"message":    "Conversation ID required",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Find messages for this conversation
		filter := bson.M{
			"client_id":       clientID,
			"conversation_id": conversationID,
		}

		cursor, err := messagesCollection.Find(
			ctx,
			filter,
			options.Find().SetSort(bson.M{"timestamp": 1}),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve messages",
			})
			return
		}
		defer cursor.Close(ctx)

		var messages []models.Message
		if err := cursor.All(ctx, &messages); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode messages",
			})
			return
		}

		// Calculate total tokens
		totalTokens := 0
		for _, msg := range messages {
			totalTokens += msg.TokenCost
		}

		var createdAt, updatedAt time.Time
		if len(messages) > 0 {
			createdAt = messages[0].Timestamp
			updatedAt = messages[len(messages)-1].Timestamp
		}

		// Return in the same format as the client endpoint (ConversationHistory)
		conversation := models.ConversationHistory{
			ConversationID: conversationID,
			Messages:       messages,
			TotalTokens:    totalTokens,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		}

		c.JSON(http.StatusOK, conversation)
	})

	// Export client chats (admin-scoped)
	admin.POST("/client/:id/export/chats", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Get user claims from context
		claims, exists := c.Get("claims")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication required",
			})
			return
		}

		userClaims, ok := claims.(*auth.Claims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "invalid_claims",
				"message":    "Invalid user claims",
			})
			return
		}

		// Parse export request
		var req services.ExportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid export request: " + err.Error(),
			})
			return
		}

		// Override client_id with the admin-selected client
		req.ClientID = clientID.Hex()

		// Validate format
		if req.Format == "" {
			req.Format = "json" // Default format
		}

		// Set default limit if not specified
		if req.Limit == 0 {
			req.Limit = 10000 // Default limit
		}

		// Create export service
		exportService := services.NewExportService(messagesCollection, clientsCollection)

		// Perform export
		response, err := exportService.ExportChats(c.Request.Context(), &req, userClaims)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "export_failed",
				"message":    "Failed to export chats: " + err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, response)
	})

	// Download client chat export (admin-scoped)
	admin.GET("/client/:id/export/chats/download", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Get user claims from context
		claims, exists := c.Get("claims")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Authentication required",
			})
			return
		}

		userClaims, ok := claims.(*auth.Claims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "invalid_claims",
				"message":    "Invalid user claims",
			})
			return
		}

		// Parse query parameters
		format := c.Query("format")
		if format == "" {
			format = "json" // Default format
		}

		// Parse date range
		var dateFrom, dateTo time.Time
		if dateFromStr := c.Query("date_from"); dateFromStr != "" {
			dateFrom, err = time.Parse("2006-01-02", dateFromStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_date",
					"message":    "Invalid date_from format. Use YYYY-MM-DD",
				})
				return
			}
		}

		if dateToStr := c.Query("date_to"); dateToStr != "" {
			dateTo, err = time.Parse("2006-01-02", dateToStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_date",
					"message":    "Invalid date_to format. Use YYYY-MM-DD",
				})
				return
			}
		}

		// Parse limit
		limit := 0
		if limitStr := c.Query("limit"); limitStr != "" {
			limit, err = strconv.Atoi(limitStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_limit",
					"message":    "Invalid limit value",
				})
				return
			}
		}

		// Parse boolean flags
		includeGeo := c.Query("include_geo") == "true"
		includeMeta := c.Query("include_meta") == "true"

		// Build export request - override client_id with admin-selected client
		req := &services.ExportRequest{
			Format:         format,
			DateFrom:       dateFrom,
			DateTo:         dateTo,
			ClientID:       clientID.Hex(), // Use admin-selected client
			ConversationID: c.Query("conversation_id"),
			Limit:          limit,
			IncludeGeo:     includeGeo,
			IncludeMeta:    includeMeta,
		}

		// Create export service
		exportService := services.NewExportService(messagesCollection, clientsCollection)

		// Perform export
		response, err := exportService.ExportChats(c.Request.Context(), req, userClaims)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "export_failed",
				"message":    "Failed to export chats: " + err.Error(),
			})
			return
		}

		// If no records found, return JSON response
		if response.RecordCount == 0 {
		c.JSON(http.StatusOK, gin.H{
				"success":      true,
				"message":      "No records found for the specified criteria",
				"record_count": 0,
			})
			return
		}

		// For direct download, we need to regenerate the data and stream it
		filter := exportService.BuildQueryFilter(req, userClaims)

		opts := options.Find()
		if req.Limit > 0 {
			opts.SetLimit(int64(req.Limit))
		}
		opts.SetSort(bson.D{{"timestamp", -1}})

		cursor, err := messagesCollection.Find(c.Request.Context(), filter, opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch messages",
			})
			return
		}
		defer cursor.Close(c.Request.Context())

		var messages []models.Message
		if err := cursor.All(c.Request.Context(), &messages); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode messages",
			})
			return
		}

		// Generate summary
		summary, err := exportService.GenerateSummary(c.Request.Context(), messages, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "summary_error",
				"message":    "Failed to generate summary",
			})
			return
		}

		// Convert to export format
		exportData := exportService.ConvertToExportFormat(messages, req, summary)

		// Stream the export directly
		if err := exportService.StreamExport(c, exportData, format); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "stream_error",
				"message":    "Failed to stream export: " + err.Error(),
			})
			return
		}
	})

	// Get client chat history (admin-scoped)
	admin.GET("/client/:id/chat-history", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Get pagination parameters
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
		search := c.Query("search")

		if page < 1 {
			page = 1
		}
		if limit < 1 || limit > 100 {
			limit = 20
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Build filter for embed users only
		filter := bson.M{
			"client_id":     clientID,
			"is_embed_user": true,
		}

		// Add search filter if provided
		if search != "" {
			filter["$or"] = []bson.M{
				{"message": bson.M{"$regex": search, "$options": "i"}},
				{"reply": bson.M{"$regex": search, "$options": "i"}},
			}
		}

		// Get total count
		total, _ := messagesCollection.CountDocuments(ctx, filter)

		// Calculate skip
		skip := (page - 1) * limit

		// Aggregate conversations
		pipeline := mongo.Pipeline{
			{primitive.E{Key: "$match", Value: filter}},
			{primitive.E{Key: "$group", Value: bson.M{
				"_id":           "$conversation_id",
				"first_message": bson.M{"$first": "$$ROOT"},
				"last_message":  bson.M{"$last": "$$ROOT"},
				"message_count": bson.M{"$sum": 1},
				"updated_at":    bson.M{"$max": "$timestamp"},
				"user_ip":       bson.M{"$first": "$user_ip"},
				"user_agent":    bson.M{"$first": "$user_agent"},
				"country":       bson.M{"$first": "$country"},
				"city":          bson.M{"$first": "$city"},
				"referrer":      bson.M{"$first": "$referrer"},
			}}},
			{primitive.E{Key: "$sort", Value: bson.M{"updated_at": -1}}},
			{primitive.E{Key: "$skip", Value: int64(skip)}},
			{primitive.E{Key: "$limit", Value: int64(limit)}},
		}

		cursor, err := messagesCollection.Aggregate(ctx, pipeline)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve chat history",
			})
			return
		}
		defer cursor.Close(ctx)

		var conversations []gin.H
		for cursor.Next(ctx) {
			var result struct {
				ID           string         `bson:"_id"`
				FirstMessage models.Message `bson:"first_message"`
				LastMessage  models.Message `bson:"last_message"`
				MessageCount int            `bson:"message_count"`
				UpdatedAt    time.Time      `bson:"updated_at"`
				UserIP       string         `bson:"user_ip"`
				UserAgent    string         `bson:"user_agent"`
				Country      string         `bson:"country"`
				City         string         `bson:"city"`
				Referrer     string         `bson:"referrer"`
			}
			if err := cursor.Decode(&result); err != nil {
				continue
			}
			conversations = append(conversations, gin.H{
				"conversation_id": result.ID,
				"first_message":   result.FirstMessage.Message,
				"last_message":    result.LastMessage.Message,
				"message_count":   result.MessageCount,
				"updated_at":      result.UpdatedAt,
				"user_ip":         result.UserIP,
				"user_agent":      result.UserAgent,
				"country":         result.Country,
				"city":            result.City,
				"referrer":        result.Referrer,
			})
		}

		totalPages := (total + int64(limit) - 1) / int64(limit)

		c.JSON(http.StatusOK, gin.H{
			"conversations": conversations,
			"total":         total,
			"page":          page,
			"limit":         limit,
			"total_pages":   totalPages,
		})
	})

	// Get client quality metrics (admin-scoped)
	admin.GET("/client/:id/quality-metrics", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		// Get period (default: last 30 days)
		period := c.DefaultQuery("period", "30d")

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Calculate quality metrics using the same logic as client routes
		metrics, err := calculateQualityMetrics(ctx, db, clientID, period)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "calculation_error",
				"message":    "Failed to calculate quality metrics",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, metrics)
	})

	// Calculate quality metrics for a client (admin-scoped)
	admin.POST("/client/:id/quality-metrics/calculate", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to verify client",
			})
			return
		}

		var req struct {
			Period string `json:"period" binding:"required"` // "daily", "weekly", "monthly", "30d"
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
			})
			return
		}

		// Trigger async calculation
		go func() {
			calcCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			_, err := calculateQualityMetrics(calcCtx, db, clientID, req.Period)
			if err != nil {
				fmt.Printf("Failed to calculate quality metrics: %v\n", err)
			}
		}()

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Quality metrics calculation started",
		})
	})

	// Process unanalyzed feedback for a client (admin-scoped)
	admin.POST("/client/:id/feedback/process-unanalyzed", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to verify client",
			})
			return
		}

		// Process synchronously so we can return results
		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		err = processUnanalyzedFeedback(ctx, db, &clientID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "processing_error",
				"message":    "Failed to process unanalyzed feedback",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Unanalyzed feedback processed successfully",
		})
	})

	// Get client feedback insights (admin-scoped)
	admin.GET("/client/:id/feedback-insights", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Verify client exists
		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Get query parameters
		resolved := c.DefaultQuery("resolved", "false")
		severity := c.Query("severity")

		filter := bson.M{
			"client_id": clientID,
		}

		if resolved == "true" {
			filter["resolved"] = true
		} else {
			filter["resolved"] = false
		}

		if severity != "" {
			filter["severity"] = severity
		}

		insightsCollection := db.Collection("feedback_insights")
		cursor, err := insightsCollection.Find(ctx, filter, options.Find().SetSort(bson.M{"created_at": -1}))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve feedback insights",
			})
			return
		}
		defer cursor.Close(ctx)

		var insights []models.FeedbackInsight
		if err := cursor.All(ctx, &insights); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode feedback insights",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"insights": insights,
			"count":    len(insights),
		})
	})

	// Resolve client feedback insight (admin-scoped)
	admin.GET("/client/:id/feedback-insights/:insightId/resolve", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		insightID, err := primitive.ObjectIDFromHex(c.Param("insightId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_insight_id",
				"message":    "Invalid insight ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		insightsCollection := db.Collection("feedback_insights")
		filter := bson.M{
			"_id":       insightID,
			"client_id": clientID,
		}

		update := bson.M{
			"$set": bson.M{
				"resolved":   true,
				"updated_at": time.Now(),
			},
		}

		result, err := insightsCollection.UpdateOne(ctx, filter, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to resolve insight",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "insight_not_found",
				"message":    "Insight not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Insight resolved successfully",
		})
	})

	// Delete client feedback insight (admin-scoped)
	admin.DELETE("/client/:id/feedback-insights/:insightId", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		insightID, err := primitive.ObjectIDFromHex(c.Param("insightId"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_insight_id",
				"message":    "Invalid insight ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		insightsCollection := db.Collection("feedback_insights")
		filter := bson.M{
			"_id":       insightID,
			"client_id": clientID,
		}

		result, err := insightsCollection.DeleteOne(ctx, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to delete insight",
			})
			return
		}

		if result.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "insight_not_found",
				"message":    "Insight not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Insight deleted successfully",
		})
	})

}
