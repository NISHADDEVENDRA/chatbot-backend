package routes

import (
	"context"
	"net/http"

	"log"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func SetupEmbedRoutes(router *gin.Engine, cfg *config.Config, mongoClient *mongo.Client, authMiddleware *middleware.AuthMiddleware) {
	db := mongoClient.Database(cfg.DBName)
	clientsCollection := db.Collection("clients")
	alertsCollection := db.Collection("suspicious_activity_alerts")

	// Initialize domain auth middleware
	domainAuthMiddleware := middleware.NewDomainAuthMiddleware(clientsCollection, alertsCollection)

	// PUBLIC: Direct embed chat route - with domain authorization
	router.GET("/embed/chat/:clientId", domainAuthMiddleware.CheckDomainAuthorization(), func(c *gin.Context) {
		clientID := c.Param("clientId")

		log.Printf("🎯 Embed chat request for client: %s", clientID)

		// Validate client ID format
		clientObjID, err := primitive.ObjectIDFromHex(clientID)
		if err != nil {
			log.Printf("❌ Invalid client ID format: %s", clientID)
			c.HTML(http.StatusBadRequest, "error.html", gin.H{
				"Error": "Invalid client ID format",
			})
			return
		}

		// Check if client exists and allows embedding
		var client models.Client
		err = clientsCollection.FindOne(context.Background(), bson.M{"_id": clientObjID}).Decode(&client)
		if err != nil {
			log.Printf("❌ Client not found: %s", clientID)
			c.HTML(http.StatusNotFound, "error.html", gin.H{
				"Error": "Client not found",
			})
			return
		}

		// Check if embedding is allowed
		if !client.Branding.AllowEmbedding {
			log.Printf("❌ Embedding not allowed for client: %s", clientID)
			c.HTML(http.StatusForbidden, "error.html", gin.H{
				"Error": "Embedding not allowed for this client",
			})
			return
		}

		// Parse theme from query parameters
		theme := c.DefaultQuery("theme", "light")
		themeColor := c.Query("color")
		if themeColor == "" {
			themeColor = client.Branding.ThemeColor
		}

		// Prepare template data without auth token (public access)
		templateData := gin.H{
			"ClientID":       clientID,
			"ThemeColor":     themeColor,
			"LogoURL":        client.Branding.LogoURL,
			"WelcomeMessage": client.Branding.WelcomeMessage,
			"PreQuestions":   client.Branding.PreQuestions,
			"AuthToken":      "", // No auth token for public access
			"Theme":          theme,
		}

		log.Printf("✅ Rendering public chatframe for client: %s with theme: %s", clientID, theme)
		c.HTML(http.StatusOK, "chatframe.html", templateData)
	})

	// PUBLIC: Chat iframe page - with domain authorization (kept for backward compatibility)
	router.GET("/embed/chatframe/:clientId", domainAuthMiddleware.CheckDomainAuthorization(), func(c *gin.Context) {
		clientID := c.Param("clientId")

		log.Printf("🎯 Chatframe request for client: %s", clientID)

		// Validate client ID format
		clientObjID, err := primitive.ObjectIDFromHex(clientID)
		if err != nil {
			c.HTML(http.StatusBadRequest, "error.html", gin.H{
				"Error": "Invalid client ID format",
			})
			return
		}

		// Get client configuration
		var client models.Client
		err = clientsCollection.FindOne(context.Background(), bson.M{"_id": clientObjID}).Decode(&client)
		if err != nil {
			c.HTML(http.StatusNotFound, "error.html", gin.H{
				"Error": "Client not found",
			})
			return
		}

		// Parse theme from query parameters
		theme := c.DefaultQuery("theme", "light")
		themeColor := c.Query("color")
		if themeColor == "" {
			themeColor = client.Branding.ThemeColor
		}

		// Prepare template data without auth token (public access)
		templateData := gin.H{
			"ClientID":       clientID,
			"ThemeColor":     themeColor,
			"LogoURL":        client.Branding.LogoURL,
			"WelcomeMessage": client.Branding.WelcomeMessage,
			"PreQuestions":   client.Branding.PreQuestions,
			"AuthToken":      "", // No auth token for public access
			"Theme":          theme,
		}

		log.Printf("✅ Rendering public chatframe for client: %s with theme: %s", clientID, theme)
		c.HTML(http.StatusOK, "chatframe.html", templateData)
	})
}
