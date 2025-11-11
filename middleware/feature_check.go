package middleware

import (
	"context"
	"net/http"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/services"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// FeatureCheckMiddleware checks if a feature is enabled for the client
type FeatureCheckMiddleware struct {
	clientsCollection *mongo.Collection
}

// NewFeatureCheckMiddleware creates a new feature check middleware
func NewFeatureCheckMiddleware(clientsCollection *mongo.Collection) *FeatureCheckMiddleware {
	return &FeatureCheckMiddleware{
		clientsCollection: clientsCollection,
	}
}

// RequireFeature checks if a specific feature is enabled for the client
func (f *FeatureCheckMiddleware) RequireFeature(featureName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID, exists := c.Get("client_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Client ID not found in context",
			})
			c.Abort()
			return
		}

		clientOID, ok := clientID.(primitive.ObjectID)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Invalid client ID type",
			})
			c.Abort()
			return
		}

		// Get client permissions
		var client models.Client
		if err := f.clientsCollection.FindOne(context.Background(), bson.M{"_id": clientOID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client permissions",
			})
			c.Abort()
			return
		}

		// Check if feature is enabled
		// If enabledFeatures is empty, all features are enabled (backward compatible)
		if !services.HasFeature(client.Permissions.EnabledFeatures, featureName) {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "feature_disabled",
				"message":    "This feature is not enabled for your account. Please contact your administrator.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequireNavigationItem checks if a navigation item is allowed for the client
func (f *FeatureCheckMiddleware) RequireNavigationItem(itemName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientID, exists := c.Get("client_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Client ID not found in context",
			})
			c.Abort()
			return
		}

		clientOID, ok := clientID.(primitive.ObjectID)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Invalid client ID type",
			})
			c.Abort()
			return
		}

		// Get client permissions
		var client models.Client
		if err := f.clientsCollection.FindOne(context.Background(), bson.M{"_id": clientOID}).Decode(&client); err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "client_not_found",
					"message":    "Client not found",
				})
				c.Abort()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve client permissions",
			})
			c.Abort()
			return
		}

		// Check if navigation item is allowed
		// If allowedNavigationItems is empty, all items are allowed (backward compatible)
		if !services.HasNavigationItem(client.Permissions.AllowedNavigationItems, itemName) {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "navigation_item_disabled",
				"message":    "This feature is not enabled for your account. Please contact your administrator.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

