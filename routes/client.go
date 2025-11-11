package routes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"saas-chatbot-platform/internal/ai"
	"saas-chatbot-platform/internal/auth"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/internal/crawler"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/services"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/generative-ai-go/genai"
	"github.com/google/uuid"
	"github.com/ledongthuc/pdf"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/api/option"
)

// Constants for token-aware history management
const (
	MAX_HISTORY_TOKENS    = 2000 // Maximum tokens to keep in conversation history
	RECENT_MESSAGES_COUNT = 20   // Always keep last N messages
	SUMMARY_REFRESH_CYCLE = 5    // Regenerate summary every N uses
)

// ConversationSummary stores summary state for a conversation
type ConversationSummary struct {
	ConversationID      string             `bson:"conversation_id" json:"conversation_id"`
	ClientID            primitive.ObjectID `bson:"client_id" json:"client_id"`
	Summary             string             `bson:"summary" json:"summary"`
	LastMessageID       primitive.ObjectID `bson:"last_message_id,omitempty" json:"last_message_id,omitempty"`
	MessageCount        int                `bson:"message_count" json:"message_count"`
	TokenCount          int                `bson:"token_count" json:"token_count"`
	UseCount            int                `bson:"use_count" json:"use_count"`                         // How many times summary has been used
	SummaryRefreshCount int                `bson:"summary_refresh_count" json:"summary_refresh_count"` // How many times refreshed
	UpdatedAt           time.Time          `bson:"updated_at" json:"updated_at"`
	CreatedAt           time.Time          `bson:"created_at" json:"created_at"`
}

// ChatRequest represents a chat request from embedded widgets
type ChatRequest struct {
	ClientID  string `json:"client_id" binding:"required"`
	Message   string `json:"message" binding:"required"`
	SessionID string `json:"session_id" binding:"required"`
}

func SetupClientRoutes(router *gin.Engine, cfg *config.Config, mongoClient *mongo.Client, authMiddleware *middleware.AuthMiddleware, roleMiddleware *middleware.RoleMiddleware) {
	client := router.Group("/client")
	client.Use(authMiddleware.RequireAuth())
	client.Use(roleMiddleware.ClientGuard())

	db := mongoClient.Database(cfg.DBName)
	clientsCollection := db.Collection("clients")
	pdfsCollection := db.Collection("pdfs")
	messagesCollection := db.Collection("messages")
	crawlsCollection := db.Collection("crawls")
	imagesCollection := db.Collection("images")
	facebookPostsCollection := db.Collection("facebook_posts")
	instagramPostsCollection := db.Collection("instagram_posts")

	// Public routes (no authentication required)
	setupPublicRoutes(router, cfg, db, clientsCollection, pdfsCollection, messagesCollection, crawlsCollection, imagesCollection, facebookPostsCollection, instagramPostsCollection)

	// Authenticated client routes
	setupAuthenticatedRoutes(client, cfg, db, clientsCollection, pdfsCollection, messagesCollection, crawlsCollection, imagesCollection, facebookPostsCollection, instagramPostsCollection)
	
	// Client permissions endpoint - Get current client's permissions
	client.GET("/permissions", func(c *gin.Context) {
		clientID, exists := c.Get("client_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "Client ID not found in context",
			})
			return
		}

		// ClientID from JWT claims is a string, convert to ObjectID
		var clientIDStr string
		if str, ok := clientID.(string); ok {
			clientIDStr = str
		} else if oid, ok := clientID.(primitive.ObjectID); ok {
			clientIDStr = oid.Hex()
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Invalid client ID type",
			})
			return
		}

		// Convert string to ObjectID
		clientOID, err := primitive.ObjectIDFromHex(clientIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var client models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientOID}).Decode(&client); err != nil {
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

		// Debug logging
		if os.Getenv("GIN_MODE") != "release" {
			fmt.Printf("[DEBUG] Client Permissions for client %s: allowed_items=%v, enabled_features=%v\n",
				clientOID.Hex(), permissions.AllowedNavigationItems, permissions.EnabledFeatures)
		}

		c.JSON(http.StatusOK, gin.H{
			"allowed_navigation_items": permissions.AllowedNavigationItems,
			"enabled_features":         permissions.EnabledFeatures,
		})
	})
}

// setupPublicRoutes configures public endpoints for embedded widgets
func setupPublicRoutes(router *gin.Engine, cfg *config.Config, db *mongo.Database, clientsCollection, pdfsCollection, messagesCollection, crawlsCollection, imagesCollection, facebookPostsCollection, instagramPostsCollection *mongo.Collection) {
	// Initialize domain auth middleware
	alertsCollection := clientsCollection.Database().Collection("suspicious_activity_alerts")
	domainAuthMiddleware := middleware.NewDomainAuthMiddleware(clientsCollection, alertsCollection)

	// Public: branding for embed widget (no auth)
	router.GET("/public/branding/:client_id", handlePublicBranding(clientsCollection))

	// Public: images for embed widget (no auth)
	router.GET("/public/images/:client_id", handlePublicImages(imagesCollection))

	// Public: Calendly config for embed widget (no auth)
	router.GET("/public/calendly/:client_id", handlePublicCalendly(clientsCollection))

	// Public: QR Code config for embed widget (no auth)
	router.GET("/public/qr-code/:client_id", handlePublicQRCode(clientsCollection))

	// Public: WhatsApp QR Code config for embed widget (no auth)
	router.GET("/public/whatsapp-qr-code/:client_id", handlePublicWhatsAppQRCode(clientsCollection))

	// Public: Telegram QR Code config for embed widget (no auth)
	router.GET("/public/telegram-qr-code/:client_id", handlePublicTelegramQRCode(clientsCollection))

	// Public: Facebook posts for embed widget (no auth)
	router.GET("/public/facebook-posts/:client_id", handlePublicFacebookPosts(facebookPostsCollection))

	// Public: Facebook posts config for embed widget (no auth)
	router.GET("/public/facebook-posts-config/:client_id", handlePublicFacebookPostsConfig(clientsCollection))

	// Public: Instagram posts for embed widget (no auth)
	router.GET("/public/instagram-posts/:client_id", handlePublicInstagramPosts(instagramPostsCollection))

	// Public: Instagram posts config for embed widget (no auth)
	router.GET("/public/instagram-posts-config/:client_id", handlePublicInstagramPostsConfig(clientsCollection))

	// Public: Website embed config for embed widget (no auth)
	router.GET("/public/website-embed-config/:client_id", handlePublicWebsiteEmbedConfig(clientsCollection))

	// Public: chat endpoint for embed widget (no auth) - with domain authorization
	router.POST("/public/chat", domainAuthMiddleware.CheckDomainAuthorization(), handlePublicChat(cfg, db, clientsCollection, pdfsCollection, messagesCollection, crawlsCollection))
	// Public: quote/proposal endpoint for embed widget (no auth) - with domain authorization
	router.POST("/public/quote/:client_id", domainAuthMiddleware.CheckDomainAuthorization(), handlePublicQuote(cfg, clientsCollection))
	// ✅ Public: feedback endpoint for embed widget (no auth)
	router.POST("/public/feedback/:message_id", handlePublicFeedback(cfg, db, messagesCollection))
}

// setupAuthenticatedRoutes configures routes that require authentication
func setupAuthenticatedRoutes(client *gin.RouterGroup, cfg *config.Config, db *mongo.Database, clientsCollection, pdfsCollection, messagesCollection, crawlsCollection, imagesCollection, facebookPostsCollection, instagramPostsCollection *mongo.Collection) {
	// Branding management
	client.GET("/branding", handleGetBranding(clientsCollection))
	client.POST("/branding", handleUpdateBranding(clientsCollection))

	// PDF management
	client.POST("/upload", handlePDFUpload(cfg, pdfsCollection))
	client.GET("/pdfs", handleListPDFs(pdfsCollection))
	client.GET("/pdfs/:id/status", handlePDFStatus(pdfsCollection))

	// Embed chat history
	client.GET("/embed-chat-history", handleEmbedChatHistory(messagesCollection))
	client.GET("/embed-conversations/:id/messages", handleEmbedConversationMessages(messagesCollection))

	// Token usage
	client.GET("/tokens", handleGetTokens(clientsCollection))

	// Chat export functionality
	client.POST("/export/chats", handleExportChats(messagesCollection, clientsCollection))
	client.GET("/export/chats/download", handleDownloadExport(messagesCollection, clientsCollection))

	// ========== ADD THESE DELETE ROUTES ==========
	client.DELETE("/pdfs/:id", handleDeletePDF(pdfsCollection)) // Single PDF delete
	client.DELETE("/pdfs/bulk", handleBulkDeletePDFs(pdfsCollection))
	// PATCH /client/pdfs/:id/status - Update PDF status
	client.PATCH("/pdfs/:id/status", handleUpdatePDFStatus(pdfsCollection))
	// Bulk PDF delete

	// Analytics
	client.GET("/analytics", handleAnalytics(messagesCollection))

	// ✅ Quality monitoring endpoints
	client.GET("/quality-metrics", handleGetQualityMetrics(cfg, db))
	client.GET("/quality-metrics/:period", handleGetQualityMetricsByPeriod(cfg, db))
	client.GET("/feedback-insights", handleGetFeedbackInsights(cfg, db))
	client.GET("/feedback-insights/:id/resolve", handleResolveFeedbackInsight(cfg, db))
	client.DELETE("/feedback-insights/:id", handleDeleteFeedbackInsight(cfg, db))
	client.POST("/quality-metrics/calculate", handleCalculateQualityMetrics(cfg, db))
	client.POST("/feedback/process-unanalyzed", handleProcessUnanalyzedFeedback(cfg, db))
	client.POST("/quality-alerts/check", handleCheckQualityAlerts(cfg, db))

	// Fix contact collection for existing conversations
	client.POST("/fix-contact-collection", handleFixContactCollection(messagesCollection))

	// Extract names and emails from existing conversations
	client.POST("/extract-user-info", handleExtractUserInfo(messagesCollection))

	// Update existing messages with real names
	client.POST("/update-message-names", handleUpdateMessageNames(messagesCollection))

	// Real users chat history (completed contact collection)
	client.GET("/real-users-chat-history", handleRealUsersChatHistory(messagesCollection))

	// Debug endpoint to check contact collection state
	client.GET("/debug-contact-state", handleDebugContactState(messagesCollection))

	// Image management
	client.GET("/images", handleGetImages(imagesCollection))
	client.POST("/images", handleAddImage(imagesCollection))
	client.DELETE("/images/:id", handleDeleteImage(imagesCollection))

	// Calendly management
	client.GET("/calendly", handleGetCalendly(clientsCollection))
	client.POST("/calendly", handleUpdateCalendly(clientsCollection))

	// QR Code management
	client.GET("/qr-code", handleGetQRCode(clientsCollection))
	client.POST("/qr-code", handleUpdateQRCode(clientsCollection))

	// WhatsApp QR Code management
	client.GET("/whatsapp-qr-code", handleGetWhatsAppQRCode(clientsCollection))
	client.POST("/whatsapp-qr-code", handleUpdateWhatsAppQRCode(clientsCollection))

	// Telegram QR Code management
	client.GET("/telegram-qr-code", handleGetTelegramQRCode(clientsCollection))
	client.POST("/telegram-qr-code", handleUpdateTelegramQRCode(clientsCollection))

	// Facebook Posts management
	client.GET("/facebook-posts", handleGetFacebookPosts(facebookPostsCollection))
	client.POST("/facebook-posts", handleAddFacebookPost(facebookPostsCollection))
	client.DELETE("/facebook-posts/:id", handleDeleteFacebookPost(facebookPostsCollection))
	client.GET("/facebook-posts-config", handleGetFacebookPostsConfig(clientsCollection))
	client.POST("/facebook-posts-config", handleUpdateFacebookPostsConfig(clientsCollection))

	// Instagram Posts management
	client.GET("/instagram-posts", handleGetInstagramPosts(instagramPostsCollection))
	client.POST("/instagram-posts", handleAddInstagramPost(instagramPostsCollection))
	client.DELETE("/instagram-posts/:id", handleDeleteInstagramPost(instagramPostsCollection))
	client.GET("/instagram-posts-config", handleGetInstagramPostsConfig(clientsCollection))
	client.POST("/instagram-posts-config", handleUpdateInstagramPostsConfig(clientsCollection))

	// Website Embed configuration routes
	client.GET("/website-embed-config", handleGetWebsiteEmbedConfig(clientsCollection))
	client.POST("/website-embed-config", handleUpdateWebsiteEmbedConfig(clientsCollection))

	// Test name detection endpoint
	client.GET("/test-name-detection", handleTestNameDetection())

	// Test name extraction endpoint
	client.GET("/test-name-extraction", handleTestNameExtraction())

	// Crawling routes
	client.POST("/crawl/start", handleStartCrawl(cfg, crawlsCollection))
	client.POST("/crawl/bulk", handleBulkCrawl(cfg, crawlsCollection))
	client.GET("/crawls", handleListCrawls(crawlsCollection))
	client.GET("/crawls/:id", handleGetCrawl(crawlsCollection))
	client.GET("/crawls/:id/status", handleCrawlStatus(crawlsCollection))
	client.DELETE("/crawls/:id", handleDeleteCrawl(crawlsCollection))

	// Email templates management
	emailTemplatesCollection := clientsCollection.Database().Collection("email_templates")
	client.GET("/email-templates", handleGetEmailTemplates(emailTemplatesCollection))
	client.GET("/email-templates/:type", handleGetEmailTemplateByType(emailTemplatesCollection))
	client.POST("/email-templates", handleCreateEmailTemplate(emailTemplatesCollection))
	client.PUT("/email-templates/:id", handleUpdateEmailTemplate(emailTemplatesCollection))
	client.DELETE("/email-templates/:id", handleDeleteEmailTemplate(emailTemplatesCollection))

}

func handleUpdatePDFStatus(pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		var request struct {
			Status string `json:"status" binding:"required"`
		}

		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request body",
			})
			return
		}

		pdfID := c.Param("id")
		pdfObjID, err := primitive.ObjectIDFromHex(pdfID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_pdf_id",
				"message":    "Invalid PDF ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		update := bson.M{
			"$set": bson.M{
				"status":     request.Status,
				"updated_at": time.Now(),
			},
		}

		result, err := pdfsCollection.UpdateOne(
			context.Background(),
			bson.M{
				"_id":       pdfObjID,
				"client_id": clientObjID,
			},
			update,
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "update_failed",
				"message":    "Failed to update PDF status",
			})
			return
		}

		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "pdf_not_found",
				"message":    "PDF not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "PDF status updated successfully",
			"pdf_id":     pdfID,
			"new_status": request.Status,
			"updated_at": time.Now().UTC(),
		})
	}
}

// handleDeletePDF - Delete a single PDF document
func handleDeletePDF(pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		// Get PDF ID from URL parameter
		pdfID := c.Param("id")
		if pdfID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "missing_pdf_id",
				"message":    "PDF ID is required",
			})
			return
		}

		// Convert PDF ID to ObjectID
		pdfObjID, err := primitive.ObjectIDFromHex(pdfID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_pdf_id",
				"message":    "Invalid PDF ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Check if PDF exists and belongs to the client
		var pdfDoc models.PDF
		err = pdfsCollection.FindOne(context.Background(), bson.M{
			"_id":       pdfObjID,
			"client_id": clientObjID,
		}).Decode(&pdfDoc)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "pdf_not_found",
					"message":    "PDF not found or does not belong to this client",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to check PDF existence",
			})
			return
		}

		// Delete the PDF document
		deleteResult, err := pdfsCollection.DeleteOne(context.Background(), bson.M{
			"_id":       pdfObjID,
			"client_id": clientObjID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "delete_failed",
				"message":    "Failed to delete PDF",
			})
			return
		}

		if deleteResult.DeletedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "pdf_not_found",
				"message":    "PDF not found",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "PDF deleted successfully",
			"pdf_id":        pdfID,
			"filename":      pdfDoc.Filename,
			"deleted_at":    time.Now().UTC(),
			"deleted_count": deleteResult.DeletedCount,
		})
	}
}

// handleBulkDeletePDFs - Delete multiple PDF documents
func handleBulkDeletePDFs(pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
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

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Delete multiple PDFs
		deleteResult, err := pdfsCollection.DeleteMany(context.Background(), bson.M{
			"_id":       bson.M{"$in": pdfObjIDs},
			"client_id": clientObjID,
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "bulk_delete_failed",
				"message":    "Failed to delete PDFs",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":       "PDFs deleted successfully",
			"requested_ids": request.PdfIDs,
			"deleted_count": deleteResult.DeletedCount,
			"deleted_at":    time.Now().UTC(),
		})
	}
}

// =====================
// PUBLIC ROUTE HANDLERS
// =====================

// handlePublicBranding returns branding info for embed widgets
func handlePublicBranding(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		clientDoc, err := getClientConfig(ctx, clientsCollection, clientOID)
		if err != nil {
			handleClientError(c, err)
			return
		}

		if !clientDoc.Branding.AllowEmbedding {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "embedding_not_allowed",
				"message":    "Embedding not allowed for this client",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"name":            clientDoc.Name,
			"logo_url":        clientDoc.Branding.LogoURL,
			"theme_color":     clientDoc.Branding.ThemeColor,
			"welcome_message": clientDoc.Branding.WelcomeMessage,
			"pre_questions":   clientDoc.Branding.PreQuestions,
			"allow_embedding": clientDoc.Branding.AllowEmbedding,
			"show_powered_by": clientDoc.Branding.ShowPoweredBy,
			// Launcher configuration
			"launcher_color":      clientDoc.Branding.LauncherColor,
			"launcher_text":       clientDoc.Branding.LauncherText,
			"launcher_icon":       clientDoc.Branding.LauncherIcon,
			"launcher_image_url":  clientDoc.Branding.LauncherImageURL,
			"launcher_video_url":  clientDoc.Branding.LauncherVideoURL,
			"launcher_svg_url":    clientDoc.Branding.LauncherSVGURL,
			"launcher_icon_color": clientDoc.Branding.LauncherIconColor,
			// Cancel icon configuration
			"cancel_icon":       clientDoc.Branding.CancelIcon,
			"cancel_image_url":  clientDoc.Branding.CancelImageURL,
			"cancel_icon_color": clientDoc.Branding.CancelIconColor,
			// AI Avatar configuration
			"ai_avatar_type":      clientDoc.Branding.AIAvatarType,
			"show_welcome_avatar": clientDoc.Branding.ShowWelcomeAvatar,
			"show_chat_avatar":    clientDoc.Branding.ShowChatAvatar,
			"show_typing_avatar":  clientDoc.Branding.ShowTypingAvatar,
		})
	}
}

// handlePublicChat processes chat requests from embedded widgets with conversation memory
func handlePublicChat(cfg *config.Config, db *mongo.Database, clientsCollection, pdfsCollection, messagesCollection, crawlsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		// Validate and convert client ID
		clientOID, err := primitive.ObjectIDFromHex(req.ClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Retrieve client configuration
		clientDoc, err := getClientConfig(ctx, clientsCollection, clientOID)
		if err != nil {
			handleClientError(c, err)
			return
		}

		// ✅ CHECK CLIENT STATUS - If inactive, block chat
		if clientDoc.Status == "inactive" || clientDoc.Status == "suspended" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "client_inactive",
				"message":    "This client account is not active",
			})
			return
		}

		// Validate embedding permissions
		if !clientDoc.Branding.AllowEmbedding {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "embedding_not_allowed",
				"message":    "Embedding not allowed for this client",
			})
			return
		}

		// Check token budget
		if clientDoc.TokenUsed >= clientDoc.TokenLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error_code":  "token_limit_exceeded",
				"message":     "Token limit exceeded. Please upgrade your plan.",
				"tokens_used": clientDoc.TokenUsed,
				"token_limit": clientDoc.TokenLimit,
			})
			return
		}

		// Generate AI response with conversation memory
		response, tokenCost, latency, err := generateAIResponseWithMemory(ctx, cfg, db, pdfsCollection, messagesCollection, crawlsCollection, clientDoc, req.Message, req.SessionID)
		if err != nil {
			// ✅ Use user-friendly error mapping
			userFriendlyErr := mapToUserFriendlyError(err, "Failed to generate AI response")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "ai_generation_error",
				"message":    userFriendlyErr.UserMessage,
				"action":     userFriendlyErr.Action,
				"details":    userFriendlyErr.Technical, // Technical details for debugging
			})
			return
		}

		// Validate token budget again with actual cost
		if clientDoc.TokenUsed+tokenCost > clientDoc.TokenLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error_code":       "insufficient_tokens",
				"message":          "Insufficient tokens to complete this request",
				"required_tokens":  tokenCost,
				"available_tokens": clientDoc.TokenLimit - clientDoc.TokenUsed,
			})
			return
		}

		// ✅ Persist conversation with IP tracking and get message ID
		messageID, err := persistMessage(ctx, messagesCollection, clientDoc.ID, req, response, tokenCost, c.Request)
		if err != nil {
			// Log error but don't fail the request
			fmt.Printf("Failed to persist message: %v\n", err)
		}

		// Update token usage atomically + ALERT CHECK
		if err := updateTokenUsage(ctx, clientsCollection, clientDoc.ID, clientDoc.TokenLimit, tokenCost); err != nil {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error": map[string]interface{}{
					"code":    "token_update_failed",
					"message": "Failed to update token usage or insufficient tokens",
				},
			})
			return
		}

		// TRIGGER REAL-TIME ALERT EVALUATION (async)
		// go func() {
		//     alertCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		//     defer cancel()

		//     if alertEvaluator := getAlertEvaluator(); alertEvaluator != nil {
		//         if err := alertEvaluator.EvaluateAndNotify(alertCtx, clientDoc.ID); err != nil {
		//             log.Printf("Failed to evaluate alerts for client %s: %v", clientDoc.Name, err)
		//         }
		//     }
		// }()

		// Calculate remaining tokens AFTER database update
		remainingTokens := clientDoc.TokenLimit - (clientDoc.TokenUsed + tokenCost)
		if remainingTokens < 0 {
			remainingTokens = 0
		}

		// Return successful response with message ID for feedback
		c.JSON(http.StatusOK, gin.H{
			"reply":            response,
			"token_cost":       tokenCost,
			"remaining_tokens": remainingTokens,
			"conversation_id":  req.SessionID,
			"message_id":       messageID.Hex(), // ✅ Include message ID for feedback
			"latency_ms":       int(latency.Milliseconds()),
			"timestamp":        time.Now().Unix(),
		})
	}
}

// ✅ ADDED: handlePublicFeedback handles feedback submission from embed widget
func handlePublicFeedback(cfg *config.Config, db *mongo.Database, messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		messageID := c.Param("message_id")
		
		var req struct {
			FeedbackType  string `json:"feedback_type" binding:"required"` // "positive" or "negative"
			Comment       string `json:"comment,omitempty"`
			IssueCategory string `json:"issue_category,omitempty"` // "wrong_answer", "unclear", "incomplete", "irrelevant", "too_generic", "repetitive", "technical_error"
		}
		
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}
		
		// Validate feedback type
		if req.FeedbackType != "positive" && req.FeedbackType != "negative" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_feedback_type",
				"message":    "Feedback type must be 'positive' or 'negative'",
			})
			return
		}
		
		// Validate issue category if provided
		validIssueCategories := map[string]bool{
			"wrong_answer":   true,
			"unclear":        true,
			"incomplete":      true,
			"irrelevant":     true,
			"too_generic":    true,
			"repetitive":     true,
			"technical_error": true,
		}
		if req.IssueCategory != "" && !validIssueCategories[req.IssueCategory] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_issue_category",
				"message":    "Invalid issue category",
			})
			return
		}
		
		// Convert message ID
		messageOID, err := primitive.ObjectIDFromHex(messageID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_message_id",
				"message":    "Invalid message ID format",
			})
			return
		}
		
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		
		// Get message to retrieve client_id and conversation context
		var message models.Message
		err = messagesCollection.FindOne(ctx, bson.M{"_id": messageOID}).Decode(&message)
		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "message_not_found",
					"message":    "Message not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to retrieve message",
			})
			return
		}
		
		// Get conversation context (last 3 messages)
		var conversationContext string
		cursor, err := messagesCollection.Find(ctx, bson.M{
			"conversation_id": message.ConversationID,
			"client_id":       message.ClientID,
		}, options.Find().SetSort(bson.M{"timestamp": -1}).SetLimit(3))
		if err == nil {
			var recentMessages []models.Message
			cursor.All(ctx, &recentMessages)
			if len(recentMessages) > 0 {
				var contextBuilder strings.Builder
				for i := len(recentMessages) - 1; i >= 0; i-- {
					contextBuilder.WriteString(fmt.Sprintf("User: %s\nAI: %s\n", recentMessages[i].Message, recentMessages[i].Reply))
				}
				conversationContext = contextBuilder.String()
			}
		}
		
		// Store feedback
		feedbackCollection := db.Collection("message_feedback")
		feedback := models.MessageFeedback{
			ID:                 primitive.NewObjectID(),
			MessageID:          messageOID,
			FeedbackType:       req.FeedbackType,
			Comment:            req.Comment,
			IssueCategory:      req.IssueCategory,
			UserMessage:        message.Message,
			AIResponse:         message.Reply,
			Timestamp:          time.Now(),
			UserIP:             c.ClientIP(),
			SessionID:          message.SessionID,
			ClientID:           message.ClientID,
			ConversationID:     message.ConversationID,
			ConversationContext: conversationContext,
			Analyzed:           false,
		}
		
		_, err = feedbackCollection.InsertOne(ctx, feedback)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to store feedback",
			})
			return
		}
		
		// ✅ Trigger async feedback analysis
		go func() {
			analyzeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			analyzeFeedback(analyzeCtx, db, feedback.ID)
		}()
		
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Feedback submitted successfully",
		})
	}
}

// ==========================
// FEEDBACK ANALYSIS & QUALITY MONITORING
// ==========================

// analyzeFeedback analyzes a single feedback entry and categorizes issues
func analyzeFeedback(ctx context.Context, db *mongo.Database, feedbackID primitive.ObjectID) {
	feedbackCollection := db.Collection("message_feedback")
	messagesCollection := db.Collection("messages")
	
	var feedback models.MessageFeedback
	err := feedbackCollection.FindOne(ctx, bson.M{"_id": feedbackID}).Decode(&feedback)
	if err != nil {
		fmt.Printf("Failed to retrieve feedback for analysis: %v\n", err)
		return
	}
	
	// If already analyzed, skip
	if feedback.Analyzed {
		return
	}
	
	// If UserMessage or AIResponse are missing, try to get them from the message
	if (feedback.UserMessage == "" || feedback.AIResponse == "") && !feedback.MessageID.IsZero() {
		var message models.Message
		err := messagesCollection.FindOne(ctx, bson.M{"_id": feedback.MessageID}).Decode(&message)
		if err == nil {
			if feedback.UserMessage == "" {
				feedback.UserMessage = message.Message
			}
			if feedback.AIResponse == "" {
				feedback.AIResponse = message.Reply
			}
		}
	}
	
	// Auto-categorize issue if not provided and feedback is negative
	if feedback.FeedbackType == "negative" && feedback.IssueCategory == "" {
		feedback.IssueCategory = categorizeIssue(feedback.UserMessage, feedback.AIResponse, feedback.Comment)
		// If still empty after categorization, set a default
		if feedback.IssueCategory == "" {
			feedback.IssueCategory = "wrong_answer" // Default category
		}
	}
	
	// Calculate quality score
	qualityScore := calculateQualityScore(feedback)
	feedback.QualityScore = qualityScore
	
	// Mark as analyzed
	feedback.Analyzed = true
	feedback.AnalysisDate = time.Now()
	
	// Update feedback with all fields
	update := bson.M{
		"$set": bson.M{
			"issue_category": feedback.IssueCategory,
			"quality_score":  feedback.QualityScore,
			"analyzed":       true,
			"analysis_date":  feedback.AnalysisDate,
		},
	}
	
	// Also update UserMessage and AIResponse if they were missing
	if feedback.UserMessage != "" {
		update["$set"].(bson.M)["user_message"] = feedback.UserMessage
	}
	if feedback.AIResponse != "" {
		update["$set"].(bson.M)["ai_response"] = feedback.AIResponse
	}
	
	_, err = feedbackCollection.UpdateOne(ctx, bson.M{"_id": feedbackID}, update)
	if err != nil {
		fmt.Printf("Failed to update analyzed feedback: %v\n", err)
		return
	}
	
	// Generate insights if negative feedback and issue category is set
	// Only create insight if feedback hasn't been used to create an insight before
	if feedback.FeedbackType == "negative" && feedback.IssueCategory != "" && !feedback.InsightCreated {
		// Use a new context with timeout for insight generation
		insightCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		insightCreated := generateFeedbackInsight(insightCtx, db, feedback)
		
		// Mark feedback as having an insight created
		if insightCreated {
			update["$set"].(bson.M)["insight_created"] = true
			feedbackCollection.UpdateOne(ctx, bson.M{"_id": feedbackID}, bson.M{"$set": bson.M{"insight_created": true}})
		}
	}
}

// categorizeIssue automatically categorizes feedback issues based on content
func categorizeIssue(userMessage, aiResponse, comment string) string {
	text := strings.ToLower(userMessage + " " + aiResponse + " " + comment)
	
	// Issue category keywords
	issueKeywords := map[string][]string{
		"wrong_answer": {
			"wrong", "incorrect", "not right", "false", "mistake", "error", "not correct",
			"गलत", "सही नहीं", "गलत जवाब",
		},
		"unclear": {
			"unclear", "confusing", "don't understand", "not clear", "confused", "unclear",
			"समझ नहीं आया", "स्पष्ट नहीं", "कन्फ्यूज",
		},
		"incomplete": {
			"incomplete", "not complete", "missing", "partial", "not enough", "more information",
			"अधूरा", "पूरा नहीं", "कम जानकारी",
		},
		"irrelevant": {
			"irrelevant", "not related", "doesn't answer", "off topic", "not what I asked",
			"अप्रासंगिक", "संबंधित नहीं", "सवाल का जवाब नहीं",
		},
		"too_generic": {
			"too generic", "vague", "not specific", "general", "not detailed",
			"सामान्य", "विवरण नहीं", "स्पष्ट नहीं",
		},
		"repetitive": {
			"repetitive", "repeating", "same", "already said", "duplicate",
			"दोहराव", "पहले कहा", "वही",
		},
		"technical_error": {
			"error", "broken", "not working", "failed", "crash", "bug",
			"त्रुटि", "काम नहीं कर रहा", "गलती",
		},
	}
	
	// Score each category
	scores := make(map[string]int)
	for category, keywords := range issueKeywords {
		score := 0
		for _, keyword := range keywords {
			if strings.Contains(text, keyword) {
				score++
			}
		}
		scores[category] = score
	}
	
	// Find category with highest score
	maxScore := 0
	bestCategory := "wrong_answer" // Default
	for category, score := range scores {
		if score > maxScore {
			maxScore = score
			bestCategory = category
		}
	}
	
	return bestCategory
}

// calculateQualityScore calculates a quality score (0-1) for feedback
func calculateQualityScore(feedback models.MessageFeedback) float64 {
	score := 0.5 // Base score
	
	// Positive feedback = high score
	if feedback.FeedbackType == "positive" {
		score = 0.9
		// Bonus for detailed positive feedback
		if len(feedback.Comment) > 20 {
			score = 1.0
		}
		return score
	}
	
	// Negative feedback = low score, adjusted by issue category
	if feedback.FeedbackType == "negative" {
		score = 0.2
		
		// Adjust based on issue category severity
		severityMap := map[string]float64{
			"wrong_answer":   0.1, // Most severe
			"technical_error": 0.1,
			"irrelevant":     0.2,
			"incomplete":     0.3,
			"unclear":        0.3,
			"too_generic":    0.4,
			"repetitive":     0.4, // Least severe
		}
		
		if severity, exists := severityMap[feedback.IssueCategory]; exists {
			score = severity
		}
		
		// Penalty if no comment (less actionable)
		if len(feedback.Comment) == 0 {
			score -= 0.05
		}
		
		if score < 0 {
			score = 0
		}
	}
	
	return score
}

// generateFeedbackInsight generates insights from negative feedback
// Returns true if insight was created or updated, false otherwise
func generateFeedbackInsight(ctx context.Context, db *mongo.Database, feedback models.MessageFeedback) bool {
	// Validate required fields
	if feedback.IssueCategory == "" {
		fmt.Printf("Cannot generate insight: issue_category is empty for feedback %s\n", feedback.ID.Hex())
		return false
	}
	
	if feedback.ClientID.IsZero() {
		fmt.Printf("Cannot generate insight: client_id is empty for feedback %s\n", feedback.ID.Hex())
		return false
	}
	
	insightsCollection := db.Collection("feedback_insights")
	
	// Extract topic from user message
	topics := extractTopics(feedback.UserMessage)
	if len(topics) == 0 {
		topics = []string{"general"}
	}
	
	// Check if similar insight already exists
	filter := bson.M{
		"client_id":      feedback.ClientID,
		"issue_category": feedback.IssueCategory,
		"resolved":       false,
	}
	
	var existingInsight models.FeedbackInsight
	err := insightsCollection.FindOne(ctx, filter).Decode(&existingInsight)
	
	if err == nil {
		// Update existing insight
		update := bson.M{
			"$inc": bson.M{"feedback_count": 1},
			"$set": bson.M{"updated_at": time.Now()},
		}
		
		// Add example feedback (limit to 5 examples per insight)
		exampleFeedback := models.FeedbackExample{
			UserMessage: feedback.UserMessage,
			AIResponse:  feedback.AIResponse,
			Comment:     feedback.Comment,
			Timestamp:   feedback.Timestamp,
		}
		
		// Add to examples array (limit to 5 most recent)
		update["$push"] = bson.M{
			"example_feedbacks": bson.M{
				"$each": []models.FeedbackExample{exampleFeedback},
				"$slice": -5, // Keep only last 5 examples
			},
		}
		
		// Update severity if feedback count increases significantly
		if existingInsight.FeedbackCount >= 10 && existingInsight.Severity == "low" {
			update["$set"].(bson.M)["severity"] = "medium"
		}
		if existingInsight.FeedbackCount >= 20 && existingInsight.Severity == "medium" {
			update["$set"].(bson.M)["severity"] = "high"
		}
		if existingInsight.FeedbackCount >= 50 && existingInsight.Severity == "high" {
			update["$set"].(bson.M)["severity"] = "critical"
		}
		
		_, err = insightsCollection.UpdateOne(ctx, filter, update)
		if err != nil {
			fmt.Printf("Failed to update existing insight: %v\n", err)
			return false
		} else {
			fmt.Printf("Updated insight for issue category: %s, new count: %d\n", feedback.IssueCategory, existingInsight.FeedbackCount+1)
		}
		
		// Mark feedback as having insight created
		feedbackCollection := db.Collection("message_feedback")
		feedbackCollection.UpdateOne(ctx, bson.M{"_id": feedback.ID}, bson.M{"$set": bson.M{"insight_created": true}})
		
		return true
	}
	
	// Create new insight with example feedback
	exampleFeedback := models.FeedbackExample{
		UserMessage: feedback.UserMessage,
		AIResponse:  feedback.AIResponse,
		Comment:     feedback.Comment,
		Timestamp:   feedback.Timestamp,
	}
	
	insight := models.FeedbackInsight{
		ID:               primitive.NewObjectID(),
		ClientID:         feedback.ClientID,
		InsightType:      "common_issue",
		Title:            fmt.Sprintf("Common issue: %s", feedback.IssueCategory),
		Description:      fmt.Sprintf("Multiple users reported '%s' issues. Topic: %s", feedback.IssueCategory, topics[0]),
		Severity:         "low",
		AffectedTopics:   topics,
		IssueCategory:    feedback.IssueCategory,
		FeedbackCount:    1,
		Recommendation:   generateRecommendation(feedback.IssueCategory, topics[0]),
		ExampleFeedbacks: []models.FeedbackExample{exampleFeedback},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		Resolved:         false,
	}
	
	_, err = insightsCollection.InsertOne(ctx, insight)
	if err != nil {
		fmt.Printf("Failed to create insight: %v\n", err)
		return false
	} else {
		fmt.Printf("Created new insight for issue category: %s, topic: %s\n", feedback.IssueCategory, topics[0])
		
		// Mark feedback as having insight created
		feedbackCollection := db.Collection("message_feedback")
		feedbackCollection.UpdateOne(ctx, bson.M{"_id": feedback.ID}, bson.M{"$set": bson.M{"insight_created": true}})
		
		return true
	}
}

// generateRecommendation generates improvement recommendations based on issue category
func generateRecommendation(issueCategory, topic string) string {
	recommendations := map[string]string{
		"wrong_answer":   fmt.Sprintf("Review and improve context retrieval for '%s' topic. Ensure accurate information is provided.", topic),
		"unclear":        fmt.Sprintf("Improve response clarity for '%s' topic. Use simpler language and provide examples.", topic),
		"incomplete":     fmt.Sprintf("Provide more comprehensive answers for '%s' topic. Include all relevant details.", topic),
		"irrelevant":    fmt.Sprintf("Improve context relevance for '%s' topic. Ensure responses directly address user questions.", topic),
		"too_generic":    fmt.Sprintf("Make responses more specific for '%s' topic. Provide detailed, actionable information.", topic),
		"repetitive":     fmt.Sprintf("Reduce repetition in responses for '%s' topic. Vary language and provide new information.", topic),
		"technical_error": "Review system logs and fix technical issues. Check API connectivity and error handling.",
	}
	
	if rec, exists := recommendations[issueCategory]; exists {
		return rec
	}
	
	return fmt.Sprintf("Review and improve responses for '%s' topic.", topic)
}

// ==========================
// QUALITY MONITORING HANDLERS
// ==========================

// handleGetQualityMetrics returns quality metrics for the authenticated client
func handleGetQualityMetrics(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Get period (default: last 30 days)
		period := c.DefaultQuery("period", "30d")
		metrics, err := calculateQualityMetrics(ctx, db, clientObjID, period)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "calculation_error",
				"message":    "Failed to calculate quality metrics",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, metrics)
	}
}

// handleGetQualityMetricsByPeriod returns quality metrics for a specific period
func handleGetQualityMetricsByPeriod(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		period := c.Param("period") // "daily", "weekly", "monthly"
		if period != "daily" && period != "weekly" && period != "monthly" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_period",
				"message":    "Period must be 'daily', 'weekly', or 'monthly'",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		metrics, err := calculateQualityMetrics(ctx, db, clientObjID, period)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "calculation_error",
				"message":    "Failed to calculate quality metrics",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, metrics)
	}
}

// handleGetFeedbackInsights returns feedback insights for the authenticated client
func handleGetFeedbackInsights(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Get query parameters
		resolved := c.DefaultQuery("resolved", "false")
		severity := c.Query("severity") // Optional filter by severity

		filter := bson.M{
			"client_id": clientObjID,
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
	}
}

// handleResolveFeedbackInsight marks a feedback insight as resolved
func handleResolveFeedbackInsight(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		insightID := c.Param("id")
		insightOID, err := primitive.ObjectIDFromHex(insightID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_insight_id",
				"message":    "Invalid insight ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		insightsCollection := db.Collection("feedback_insights")
		filter := bson.M{
			"_id":       insightOID,
			"client_id": clientObjID,
		}

		update := bson.M{
			"$set": bson.M{
				"resolved":    true,
				"resolved_at": time.Now(),
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
	}
}

// handleDeleteFeedbackInsight deletes a feedback insight
func handleDeleteFeedbackInsight(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		insightID := c.Param("id")
		insightOID, err := primitive.ObjectIDFromHex(insightID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_insight_id",
				"message":    "Invalid insight ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		insightsCollection := db.Collection("feedback_insights")
		filter := bson.M{
			"_id":       insightOID,
			"client_id": clientObjID,
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
	}
}

// handleCalculateQualityMetrics manually triggers quality metrics calculation
func handleCalculateQualityMetrics(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		var req struct {
			Period string `json:"period" binding:"required"` // "daily", "weekly", "monthly"
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
			_, err := calculateQualityMetrics(calcCtx, db, clientObjID, req.Period)
			if err != nil {
				fmt.Printf("Failed to calculate quality metrics: %v\n", err)
			}
		}()

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Quality metrics calculation started",
		})
	}
}

// calculateQualityMetrics calculates quality metrics for a client and period
func calculateQualityMetrics(ctx context.Context, db *mongo.Database, clientID primitive.ObjectID, period string) (*models.QualityMetrics, error) {
	feedbackCollection := db.Collection("message_feedback")
	metricsCollection := db.Collection("quality_metrics")

	// Determine time range based on period
	var periodStart, periodEnd time.Time
	now := time.Now()

	switch period {
	case "daily":
		periodStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		periodEnd = now
	case "weekly":
		periodStart = now.AddDate(0, 0, -7)
		periodEnd = now
	case "monthly":
		periodStart = now.AddDate(0, 0, -30)
		periodEnd = now
	case "30d":
		periodStart = now.AddDate(0, 0, -30)
		periodEnd = now
	default:
		periodStart = now.AddDate(0, 0, -30)
		periodEnd = now
	}

	// Query feedback for the period
	filter := bson.M{
		"client_id": clientID,
		"timestamp": bson.M{
			"$gte": periodStart,
			"$lte": periodEnd,
		},
	}

	cursor, err := feedbackCollection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query feedback: %w", err)
	}
	defer cursor.Close(ctx)

	var feedbacks []models.MessageFeedback
	if err := cursor.All(ctx, &feedbacks); err != nil {
		return nil, fmt.Errorf("failed to decode feedback: %w", err)
	}

	// Calculate metrics
	totalFeedback := len(feedbacks)
	positiveFeedback := 0
	negativeFeedback := 0
	issueDistribution := make(map[string]int)
	topicDistribution := make(map[string]int)
	totalQualityScore := 0.0
	qualityScoreCount := 0

	for _, feedback := range feedbacks {
		if feedback.FeedbackType == "positive" {
			positiveFeedback++
		} else {
			negativeFeedback++
			if feedback.IssueCategory != "" {
				issueDistribution[feedback.IssueCategory]++
			}
		}

		// Extract topic from user message
		topics := extractTopics(feedback.UserMessage)
		if len(topics) > 0 {
			topicDistribution[topics[0]]++
		} else {
			topicDistribution["general"]++
		}

		// Calculate quality score if not already set
		if feedback.QualityScore > 0 {
			totalQualityScore += feedback.QualityScore
			qualityScoreCount++
		}
	}

	// Calculate satisfaction rate
	satisfactionRate := 0.0
	if totalFeedback > 0 {
		satisfactionRate = float64(positiveFeedback) / float64(totalFeedback)
	}

	// Calculate average quality score
	averageQualityScore := 0.0
	if qualityScoreCount > 0 {
		averageQualityScore = totalQualityScore / float64(qualityScoreCount)
	}

	// Create metrics object
	metrics := &models.QualityMetrics{
		ID:                  primitive.NewObjectID(),
		ClientID:            clientID,
		Period:              period,
		PeriodStart:         periodStart,
		PeriodEnd:           periodEnd,
		TotalFeedback:       totalFeedback,
		PositiveFeedback:   positiveFeedback,
		NegativeFeedback:    negativeFeedback,
		SatisfactionRate:    satisfactionRate,
		IssueDistribution:   issueDistribution,
		TopicDistribution:   topicDistribution,
		AverageQualityScore: averageQualityScore,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	// Store or update metrics
	upsertFilter := bson.M{
		"client_id":    clientID,
		"period":       period,
		"period_start": periodStart,
		"period_end":   periodEnd,
	}

	update := bson.M{
		"$set": bson.M{
			"total_feedback":        metrics.TotalFeedback,
			"positive_feedback":     metrics.PositiveFeedback,
			"negative_feedback":     metrics.NegativeFeedback,
			"satisfaction_rate":     metrics.SatisfactionRate,
			"issue_distribution":    metrics.IssueDistribution,
			"topic_distribution":    metrics.TopicDistribution,
			"average_quality_score": metrics.AverageQualityScore,
			"updated_at":            metrics.UpdatedAt,
		},
		"$setOnInsert": bson.M{
			"_id":          metrics.ID,
			"created_at":   metrics.CreatedAt,
			"period_start": metrics.PeriodStart,
			"period_end":   metrics.PeriodEnd,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err = metricsCollection.UpdateOne(ctx, upsertFilter, update, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to store metrics: %w", err)
	}

	return metrics, nil
}

// processUnanalyzedFeedback processes all unanalyzed feedback entries
func processUnanalyzedFeedback(ctx context.Context, db *mongo.Database, clientID *primitive.ObjectID) error {
	feedbackCollection := db.Collection("message_feedback")
	messagesCollection := db.Collection("messages")
	
	// Build filter - check for analyzed field being false or missing
	// Exclude feedback that already has an insight created (even if insight was deleted)
	filter := bson.M{
		"$and": []bson.M{
			{
				"$or": []bson.M{
					{"analyzed": false},
					{"analyzed": bson.M{"$exists": false}}, // Handle old feedback without analyzed field
				},
			},
			{
				"$or": []bson.M{
					{"insight_created": false},
					{"insight_created": bson.M{"$exists": false}}, // Handle old feedback without insight_created field
				},
			},
		},
	}
	
	if clientID != nil {
		filter["client_id"] = *clientID
	}
	
	fmt.Printf("Processing unanalyzed feedback for client: %s\n", clientID.Hex())
	
	cursor, err := feedbackCollection.Find(ctx, filter, options.Find().SetLimit(100))
	if err != nil {
		return fmt.Errorf("failed to query unanalyzed feedback: %w", err)
	}
	defer cursor.Close(ctx)
	
	var feedbacks []models.MessageFeedback
	if err := cursor.All(ctx, &feedbacks); err != nil {
		return fmt.Errorf("failed to decode feedback: %w", err)
	}
	
	fmt.Printf("Found %d unanalyzed feedback entries\n", len(feedbacks))
	
	processed := 0
	insightsCreated := 0
	
	if len(feedbacks) > 0 {
		for _, feedback := range feedbacks {
			fmt.Printf("Processing feedback ID: %s, Type: %s, IssueCategory: %s\n", 
				feedback.ID.Hex(), feedback.FeedbackType, feedback.IssueCategory)
			
			// Analyze feedback
			analyzeFeedback(ctx, db, feedback.ID)
			processed++
			
			// Check if insight was created (only for negative feedback)
			if feedback.FeedbackType == "negative" {
				insightsCreated++
			}
		}
	} else {
		// If no unanalyzed feedback, check if there are negative feedback without insights
		fmt.Printf("No unanalyzed feedback found, checking for negative feedback without insights...\n")
		
		negativeFilter := bson.M{
			"feedback_type": "negative",
			"$or": []bson.M{
				{"insight_created": false},
				{"insight_created": bson.M{"$exists": false}}, // Handle old feedback without insight_created field
			},
		}
		if clientID != nil {
			negativeFilter["client_id"] = *clientID
		}
		
		negativeCursor, err := feedbackCollection.Find(ctx, negativeFilter, options.Find().SetLimit(100))
		if err == nil {
			var negativeFeedbacks []models.MessageFeedback
			negativeCursor.All(ctx, &negativeFeedbacks)
			negativeCursor.Close(ctx)
			
			fmt.Printf("Found %d negative feedback entries\n", len(negativeFeedbacks))
			
			// Check which ones don't have insights
			insightsCollection := db.Collection("feedback_insights")
			for _, feedback := range negativeFeedbacks {
				// Skip feedback that already has an insight created (even if insight was deleted)
				if feedback.InsightCreated {
					fmt.Printf("Skipping feedback ID: %s - already used to create insight\n", feedback.ID.Hex())
					continue
				}
				
				// Ensure feedback is analyzed
				if !feedback.Analyzed {
					analyzeFeedback(ctx, db, feedback.ID)
					processed++
				}
				
				// Check if insight exists for this feedback
				insightFilter := bson.M{
					"client_id":      feedback.ClientID,
					"issue_category": feedback.IssueCategory,
					"resolved":       false,
				}
				if feedback.IssueCategory == "" {
					// Try to categorize if missing
					feedback.IssueCategory = categorizeIssue(feedback.UserMessage, feedback.AIResponse, feedback.Comment)
					if feedback.IssueCategory == "" {
						feedback.IssueCategory = "wrong_answer"
					}
					insightFilter["issue_category"] = feedback.IssueCategory
				}
				
				var existingInsight models.FeedbackInsight
				err := insightsCollection.FindOne(ctx, insightFilter).Decode(&existingInsight)
				if err != nil {
					// No insight exists, create one
					fmt.Printf("Creating insight for feedback ID: %s, Category: %s\n", 
						feedback.ID.Hex(), feedback.IssueCategory)
					
					// Ensure feedback has required fields before generating insight
					if feedback.UserMessage == "" || feedback.AIResponse == "" {
						// Try to get from message
						if !feedback.MessageID.IsZero() {
							var message models.Message
							err := messagesCollection.FindOne(ctx, bson.M{"_id": feedback.MessageID}).Decode(&message)
							if err == nil {
								if feedback.UserMessage == "" {
									feedback.UserMessage = message.Message
								}
								if feedback.AIResponse == "" {
									feedback.AIResponse = message.Reply
								}
							}
						}
					}
					
					insightCreated := generateFeedbackInsight(ctx, db, feedback)
					if insightCreated {
						// Mark feedback as having insight created
						feedbackCollection.UpdateOne(ctx, bson.M{"_id": feedback.ID}, bson.M{"$set": bson.M{"insight_created": true}})
						insightsCreated++
					}
				} else {
					// Insight exists, but add this feedback as an example if not already present
					exampleFeedback := models.FeedbackExample{
						UserMessage: feedback.UserMessage,
						AIResponse:  feedback.AIResponse,
						Comment:     feedback.Comment,
						Timestamp:   feedback.Timestamp,
					}
					
					// Get from message if missing
					if exampleFeedback.UserMessage == "" || exampleFeedback.AIResponse == "" {
						if !feedback.MessageID.IsZero() {
							var message models.Message
							err := messagesCollection.FindOne(ctx, bson.M{"_id": feedback.MessageID}).Decode(&message)
							if err == nil {
								if exampleFeedback.UserMessage == "" {
									exampleFeedback.UserMessage = message.Message
								}
								if exampleFeedback.AIResponse == "" {
									exampleFeedback.AIResponse = message.Reply
								}
							}
						}
					}
					
					// Add example to existing insight (limit to 5)
					update := bson.M{
						"$push": bson.M{
							"example_feedbacks": bson.M{
								"$each": []models.FeedbackExample{exampleFeedback},
								"$slice": -5,
							},
						},
					}
					insightsCollection.UpdateOne(ctx, insightFilter, update)
					
					// Mark feedback as having insight created
					feedbackCollection.UpdateOne(ctx, bson.M{"_id": feedback.ID}, bson.M{"$set": bson.M{"insight_created": true}})
				}
			}
		}
	}
	
	fmt.Printf("Processed %d feedback entries, created/updated %d insights\n", processed, insightsCreated)
	return nil
}

// checkQualityAlerts checks for quality issues and generates alerts
func checkQualityAlerts(ctx context.Context, db *mongo.Database, clientID primitive.ObjectID) error {
	// Get recent quality metrics
	metrics, err := calculateQualityMetrics(ctx, db, clientID, "30d")
	if err != nil {
		return fmt.Errorf("failed to calculate metrics: %w", err)
	}
	
	// Check alert thresholds
	alerts := []string{}
	
	// Low satisfaction rate alert
	if metrics.SatisfactionRate < 0.7 && metrics.TotalFeedback >= 10 {
		alerts = append(alerts, fmt.Sprintf("Low satisfaction rate: %.1f%% (threshold: 70%%)", metrics.SatisfactionRate*100))
	}
	
	// High negative feedback rate alert
	negativeRate := float64(metrics.NegativeFeedback) / float64(metrics.TotalFeedback)
	if negativeRate > 0.3 && metrics.TotalFeedback >= 10 {
		alerts = append(alerts, fmt.Sprintf("High negative feedback rate: %.1f%% (threshold: 30%%)", negativeRate*100))
	}
	
	// Critical issue alert
	if metrics.IssueDistribution["wrong_answer"] >= 5 {
		alerts = append(alerts, fmt.Sprintf("Multiple wrong answer issues: %d reports", metrics.IssueDistribution["wrong_answer"]))
	}
	
	// Low quality score alert
	if metrics.AverageQualityScore < 0.5 && metrics.TotalFeedback >= 10 {
		alerts = append(alerts, fmt.Sprintf("Low average quality score: %.2f (threshold: 0.5)", metrics.AverageQualityScore))
	}
	
	// Store alerts if any
	if len(alerts) > 0 {
		alertsCollection := db.Collection("quality_alerts")
		alert := bson.M{
			"_id":         primitive.NewObjectID(),
			"client_id":   clientID,
			"alerts":      alerts,
			"metrics":     metrics,
			"created_at":  time.Now(),
			"acknowledged": false,
		}
		
		_, err = alertsCollection.InsertOne(ctx, alert)
		if err != nil {
			fmt.Printf("Failed to store quality alerts: %v\n", err)
		} else {
			fmt.Printf("Generated %d quality alerts for client %s\n", len(alerts), clientID.Hex())
		}
	}
	
	return nil
}

// handleProcessUnanalyzedFeedback processes all unanalyzed feedback
func handleProcessUnanalyzedFeedback(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Process synchronously so we can return results
		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()
		
		err = processUnanalyzedFeedback(ctx, db, &clientObjID)
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
	}
}

// handleCheckQualityAlerts checks for quality issues and generates alerts
func handleCheckQualityAlerts(cfg *config.Config, db *mongo.Database) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		err = checkQualityAlerts(ctx, db, clientObjID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "alert_check_error",
				"message":    "Failed to check quality alerts",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Quality alerts checked successfully",
		})
	}
}

// ==========================
// AUTHENTICATED ROUTE HANDLERS
// ==========================

// handleGetBranding returns current client branding
func handleGetBranding(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		clientDoc, err := getClientConfig(ctx, clientsCollection, clientObjID)
		if err != nil {
			handleClientError(c, err)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"name":     clientDoc.Name,
			"branding": clientDoc.Branding,
		})
	}
}

// handleUpdateBranding updates client branding
func handleUpdateBranding(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		var branding models.Branding
		if err := c.ShouldBindJSON(&branding); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid branding data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		if len(branding.PreQuestions) > 5 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "too_many_questions",
				"message":    "Maximum 5 pre-questions allowed",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		update := bson.M{
			"$set": bson.M{
				"branding":   branding,
				"updated_at": time.Now(),
			},
		}

		result, err := clientsCollection.UpdateOne(ctx, bson.M{"_id": clientObjID}, update)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to update branding",
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

		// Fetch updated branding from database to ensure all fields are returned
		clientDoc, err := getClientConfig(ctx, clientsCollection, clientObjID)
		if err != nil {
			// If fetch fails, return the original branding (fallback)
			c.JSON(http.StatusOK, gin.H{
				"message":  "Branding updated successfully",
				"branding": branding,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":  "Branding updated successfully",
			"branding": clientDoc.Branding,
		})
	}
}

// handlePDFUpload processes PDF file uploads using the new PDF service
func handlePDFUpload(cfg *config.Config, pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" && !middleware.IsAdmin(c) {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required for upload",
			})
			return
		}

		// Parse multipart form with LIMITED memory (just for headers, not full file)
		// Use 32MB buffer - enough for form fields but keeps file streaming
		// IMPORTANT: This ensures files are streamed, not loaded into memory
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

		// Convert client ID
		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Create PDF service
		pdfService := services.NewPDFService(cfg, pdfsCollection)

		// Create secure upload request
		uploadReq := &services.SecureUploadRequest{
			File:     file,
			Header:   header,
			ClientID: clientObjID,
			UserID:   primitive.NilObjectID, // Public upload
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
	}
}

// handlePDFStatus returns the processing status of a PDF
func handlePDFStatus(pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		pdfID := c.Param("id")
		pdfObjID, err := primitive.ObjectIDFromHex(pdfID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_pdf_id",
				"message":    "Invalid PDF ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var pdfDoc models.PDF
		err = pdfsCollection.FindOne(ctx, bson.M{
			"_id":       pdfObjID,
			"client_id": clientObjID,
		}).Decode(&pdfDoc)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				c.JSON(http.StatusNotFound, gin.H{
					"error_code": "pdf_not_found",
					"message":    "PDF not found",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve PDF status",
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
	}
}

// handleGetTokens returns token usage information
func handleGetTokens(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		clientDoc, err := getClientConfig(ctx, clientsCollection, clientObjID)
		if err != nil {
			handleClientError(c, err)
			return
		}

		remaining := clientDoc.TokenLimit - clientDoc.TokenUsed
		if remaining < 0 {
			remaining = 0
		}

		usage := 0.0
		if clientDoc.TokenLimit > 0 {
			usage = float64(clientDoc.TokenUsed) / float64(clientDoc.TokenLimit) * 100
		}

		c.JSON(http.StatusOK, models.TokenUsage{
			Used:      clientDoc.TokenUsed,
			Limit:     clientDoc.TokenLimit,
			Remaining: remaining,
			Usage:     usage,
		})
	}
}

// handleListPDFs returns paginated list of uploaded PDFs
func handleListPDFs(pdfsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		skip := (page - 1) * limit

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := pdfsCollection.Find(ctx,
			bson.M{"client_id": clientObjID},
			&options.FindOptions{
				Skip:  &[]int64{int64(skip)}[0],
				Limit: &[]int64{int64(limit)}[0],
				Sort:  bson.M{"uploaded_at": -1},
			},
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve PDFs",
			})
			return
		}
		defer cursor.Close(ctx)

		var pdfs []models.PDF
		if err := cursor.All(ctx, &pdfs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to decode PDFs",
			})
			return
		}

		total, _ := pdfsCollection.CountDocuments(ctx, bson.M{"client_id": clientObjID})

		c.JSON(http.StatusOK, gin.H{
			"pdfs":        pdfs,
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		})
	}
}

// handleAnalytics returns client analytics data
func handleAnalytics(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Parse period parameter
		period := strings.ToLower(strings.TrimSpace(c.DefaultQuery("period", "30d")))
		dur := parsePeriod(period)

		end := time.Now()
		start := end.Add(-dur)

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		analytics, err := generateAnalytics(ctx, messagesCollection, clientObjID, start, end, period)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "analytics_error",
				"message":    "Failed to generate analytics",
				"details":    err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, analytics)
	}
}

// ===================
// ENHANCED AI RESPONSE WITH MEMORY
// ===================

// getDefaultPersona retrieves the default persona from system settings
func getDefaultPersona(ctx context.Context, db *mongo.Database) (*models.AIPersonaData, error) {
	systemSettingsCollection := db.Collection("system_settings")
	var settingDoc bson.M
	err := systemSettingsCollection.FindOne(ctx, bson.M{"key": "default_persona"}).Decode(&settingDoc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // No default persona set
		}
		return nil, err
	}

	// Extract persona data from document
	valueRaw, ok := settingDoc["value"]
	if !ok || valueRaw == nil {
		return nil, nil
	}

	// Convert to AIPersonaData
	var personaData models.AIPersonaData
	personaBytes, _ := bson.Marshal(valueRaw)
	bson.Unmarshal(personaBytes, &personaData)
	return &personaData, nil
}

// generateAIResponseWithMemory generates AI response with conversation history
func generateAIResponseWithMemory(ctx context.Context, cfg *config.Config, db *mongo.Database, pdfsCollection, messagesCollection, crawlsCollection *mongo.Collection, client *models.Client, message, sessionID string) (string, int, time.Duration, error) {
	// ✅ START: Performance tracking - start overall timer
	overallStart := time.Now()
	var phaseTimings models.PhaseTimings

	// Check contact collection state
	phase, chatDisabled, err := getContactCollectionState(ctx, messagesCollection, client.ID, sessionID)
	if err != nil {
		fmt.Printf("Warning: Failed to get contact collection state: %v\n", err)
		phase = "none"
		chatDisabled = false
	}

	// If chat is disabled, return completion message
	if chatDisabled {
		return "Thank you! Hamari team aapse jald hi contact karegi. Chat session completed.", 30, 0, nil
	}

	// Initialize Gemini client for token counting and summarization
	geminiClient, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to initialize Gemini client: %w", err)
	}
	defer geminiClient.Close()

	// Configure model
	model := configureGeminiModel(geminiClient)

	// Initialize SummarizationService
	aiGeminiClient, err := ai.NewGeminiClient(cfg.GeminiAPIKey, "free")
	if err != nil {
		return "", 0, 0, fmt.Errorf("failed to initialize AI Gemini client: %w", err)
	}
	defer aiGeminiClient.Close()
	summarizationService := services.NewSummarizationService(aiGeminiClient)

	// ✅ START: Context retrieval timing
	contextStart := time.Now()
	// Retrieve PDF context - prefer Atlas Search/Vector when enabled
	pdfChunks, err := retrievePDFContext(ctx, cfg, pdfsCollection, client.ID, message, 8)
	if err != nil {
		fmt.Printf("Warning: Failed to retrieve PDF context: %v\n", err)
	} else {
		// PDF chunks retrieved for context
	}

	// ✅ Retrieve crawled content context from completed crawl jobs
	crawledChunks, err := retrieveCrawledContext(ctx, crawlsCollection, client.ID, message, 8)
	if err != nil {
		fmt.Printf("Warning: Failed to retrieve crawled context: %v\n", err)
	} else {
		// Crawled chunks retrieved for context
	}
	phaseTimings.ContextRetrievalMs = int(time.Since(contextStart).Milliseconds())

	// Combine PDF and crawled chunks
	var allContextChunks []models.ContentChunk
	allContextChunks = append(allContextChunks, pdfChunks...)
	allContextChunks = append(allContextChunks, crawledChunks...)
	// Total context chunks prepared

	// ✅ Check if client has any documents - critical for new clients
	hasDocuments := len(allContextChunks) > 0
	if !hasDocuments {
		// Client has no documents - using persona information only
	}

	// ✅ START: History loading timing
	historyStart := time.Now()
	// ✅ Token-aware history retrieval with summarization
	conversationHistory, historySummary, tokensBefore, tokensAfter, summarized, summaryRefreshCount, err := getTokenAwareHistory(
		ctx, messagesCollection, client.ID, sessionID, model, summarizationService,
	)
	if err != nil {
		fmt.Printf("Warning: Token-aware history retrieval failed, falling back to simple retrieval: %v\n", err)
		// Fallback to simple history retrieval
		conversationHistory, err = getConversationHistory(ctx, messagesCollection, client.ID, sessionID, 100)
		if err != nil {
			fmt.Printf("Warning: Failed to retrieve conversation history: %v\n", err)
		}
		historySummary = ""
		tokensBefore = 0
		tokensAfter = 0
		summarized = false
		summaryRefreshCount = 0
	}
	phaseTimings.HistoryLoadingMs = int(time.Since(historyStart).Milliseconds())
	
	// Summarization timing (if summarized)
	if summarized {
		phaseTimings.SummarizationMs = phaseTimings.HistoryLoadingMs / 2 // Approximate
	}

	// Build enhanced context with conversation history and summary
	contextStr := buildContextWithHistory(allContextChunks, conversationHistory, historySummary)

	// ✅ ADD AI PERSONA CONTENT TO CONTEXT
	// Layer 2: Client-specific persona (highest priority)
	if client.AIPersona != nil && client.AIPersona.Content != "" {
		// Adding Client Persona (Layer 2) content to context
		personaContext := fmt.Sprintf("AI PERSONALITY & KNOWLEDGE:\n%s\n\n---\n\n", client.AIPersona.Content)
		contextStr = personaContext + contextStr
	} else {
		// Layer 1: Default persona (fallback if client doesn't have one)
		// ✅ Use default persona when client has no documents - this is the expected behavior
		// The default persona should contain generic instructions, not client-specific information
		defaultPersona, err := getDefaultPersona(ctx, db)
		if err != nil {
			fmt.Printf("Warning: Failed to retrieve default persona: %v\n", err)
		} else if defaultPersona != nil && defaultPersona.Content != "" {
			// Adding Default Persona (Layer 1) content to context
			personaContext := fmt.Sprintf("AI PERSONALITY & KNOWLEDGE:\n%s\n\n---\n\n", defaultPersona.Content)
			contextStr = personaContext + contextStr
		}
	}

	// ✅ START: Prompt building timing
	promptStart := time.Now()
	// Generate enhanced prompt with conversation context
	// ✅ Pass hasDocuments flag to ensure proper handling when no documents exist
	prompt := buildPromptWithHistory(client.Name, contextStr, conversationHistory, message, hasDocuments)
	phaseTimings.PromptBuildingMs = int(time.Since(promptStart).Milliseconds())

	// ✅ START: AI generation timing
	aiStart := time.Now()
	// Generate response with timing
	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	aiLatency := time.Since(aiStart)
	phaseTimings.AIGenerationMs = int(aiLatency.Milliseconds())

	if err != nil {
		userFriendlyErr := mapToUserFriendlyError(err, "AI generation failed")
		// Store performance metrics for error case
		go storePerformanceMetrics(db, client.ID, sessionID, phaseTimings, int(time.Since(overallStart).Milliseconds()), 
			0, "error", userFriendlyErr.UserMessage, len(message), 0)
		return "", 0, time.Since(overallStart), fmt.Errorf("generation failed: %w", err)
	}

	// Extract response text
	replyText, err := extractResponseText(resp)
	if err != nil {
		userFriendlyErr := mapToUserFriendlyError(err, "Failed to extract AI response")
		// Store performance metrics for error case
		go storePerformanceMetrics(db, client.ID, sessionID, phaseTimings, 0, 0, "error", userFriendlyErr.UserMessage, len(message), 0)
		return "", 0, time.Since(overallStart), fmt.Errorf("generation failed: %w", err)
	}

	// ✅ START: Response length validation
	validationStart := time.Now()
	topicDepth := getTopicDepth(conversationHistory, message)
	valid, validatedText, action := validateResponseLength(replyText, topicDepth)
	if !valid {
		fmt.Printf("Warning: Response length validation failed (depth=%d, word_count=%d, action=%s)\n", 
			topicDepth, countWords(replyText), action)
		// If too short and we can regenerate, try once more
		if action == "expand" {
			// Try to expand the response
			expandedPrompt := prompt + "\n\nIMPORTANT: The previous response was too short. Please provide a more detailed and comprehensive answer."
			aiStart2 := time.Now()
			resp2, err2 := model.GenerateContent(ctx, genai.Text(expandedPrompt))
			if err2 == nil {
				replyText2, err2 := extractResponseText(resp2)
				if err2 == nil && countWords(replyText2) > countWords(replyText) {
					replyText = replyText2
					phaseTimings.AIGenerationMs += int(time.Since(aiStart2).Milliseconds())
					fmt.Printf("Successfully expanded response from %d to %d words\n", countWords(validatedText), countWords(replyText))
				}
			}
		} else if action == "condense" {
			// Truncate if too long (keep first N words based on depth)
			maxWords := getMaxWordsForDepth(topicDepth)
			words := strings.Fields(replyText)
			if len(words) > maxWords {
				replyText = strings.Join(words[:maxWords], " ") + "..."
				fmt.Printf("Truncated response from %d to %d words\n", len(words), maxWords)
			}
		}
	}
	phaseTimings.ValidationMs = int(time.Since(validationStart).Milliseconds())

	// Calculate token cost including conversation history
	allParts := []genai.Part{
		genai.Text(message),
		genai.Text(replyText),
		genai.Text(contextStr),
	}

	tokenCost, err := calculateAccurateTokens(ctx, model, allParts...)
	if err != nil {
		// Fallback to estimation if accurate calculation fails
		fmt.Printf("Warning: Accurate token calculation failed, using estimation: %v\n", err)
		tokenCost = estimateTokenCostWithHistory(message, replyText, len(allContextChunks), len(conversationHistory))
	}

	// Log detailed token usage and metrics for observability
	fmt.Printf("[tokens] input_parts=%d token_cost=%d latency_ms=%d session=%s client=%s tokens_before=%d tokens_after=%d summarized=%t summary_refresh_count=%d\n",
		len(allParts), tokenCost, int(time.Since(overallStart).Milliseconds()), sessionID, client.ID.Hex(), tokensBefore, tokensAfter, summarized, summaryRefreshCount)

	// Handle contact collection state management
	newPhase := phase
	var userName, userEmail string
	var shouldDisableChat bool

	// Check if this is a contact query and we're not already in collection mode
	if isContactQuery(message) && phase == "none" {
		newPhase = "awaiting_name"
	}

	// Check if user provided name (awaiting_name phase)
	if phase == "awaiting_name" && !isContactQuery(message) {
		// Try to extract name from the message
		extractedName := extractNameFromMessage(message)
		if extractedName != "" {
			userName = extractedName
			newPhase = "awaiting_email"
			// Name detected, updating contact collection phase
		}
	}

	// Check if user provided email (awaiting_email phase)
	if phase == "awaiting_email" && isEmailProvided(message) {
		userEmail = strings.TrimSpace(message)
		newPhase = "completed"
		shouldDisableChat = true
		// Email detected, updating contact collection phase
	}

	// Check if user provided both name and email in one message
	if phase == "awaiting_name" && isEmailProvided(message) {
		// Extract name and email from the message
		extractedName := extractNameFromMessage(message)
		if extractedName != "" {
			userName = extractedName
		}

		// Extract email
		parts := strings.Fields(message)
		for _, part := range parts {
			if isEmailProvided(part) {
				userEmail = part
				break
			}
		}

		if userName != "" && userEmail != "" {
			newPhase = "completed"
			shouldDisableChat = true
		}
	}

	// Check if AI response indicates completion (fallback)
	if strings.Contains(replyText, "Hamari team aapse jald hi contact karegi") && phase != "none" {
		newPhase = "completed"
		shouldDisableChat = true
		// If we're completing, we need to get the user name and email from the conversation
		if userName == "" || userEmail == "" {
			// Get the latest user name and email from the conversation
			filter := bson.M{
				"client_id":       client.ID,
				"conversation_id": sessionID,
				"is_embed_user":   true,
			}
			opts := options.FindOne().SetSort(bson.M{"timestamp": -1})
			var latestMessage models.Message
			err := messagesCollection.FindOne(ctx, filter, opts).Decode(&latestMessage)
			if err == nil {
				if userName == "" && latestMessage.UserName != "" {
					userName = latestMessage.UserName
				}
				if userEmail == "" && latestMessage.UserEmail != "" {
					userEmail = latestMessage.UserEmail
				}
			}
		}
	}

	// Update contact collection state if it changed
	if newPhase != phase || userName != "" || userEmail != "" {
		fmt.Printf("Contact collection state update: phase=%s->%s, userName=%s, userEmail=%s, chatDisabled=%v\n",
			phase, newPhase, userName, userEmail, shouldDisableChat)
		err := updateContactCollectionState(ctx, messagesCollection, client.ID, sessionID, newPhase, userName, userEmail, shouldDisableChat)
		if err != nil {
			fmt.Printf("Warning: Failed to update contact collection state: %v\n", err)
		} else {
			fmt.Printf("Successfully updated contact collection state\n")
		}

		// ✅ NEW: Store the name by IP for future conversations
		if userName != "" {
			go func() {
				storeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				// Get user IP from the request context (we need to pass it from the calling function)
				// For now, we'll get it from the latest message
				filter := bson.M{
					"client_id":       client.ID,
					"conversation_id": sessionID,
					"is_embed_user":   true,
				}
				opts := options.FindOne().SetSort(bson.M{"timestamp": -1})
				var latestMessage models.Message
				err := messagesCollection.FindOne(storeCtx, filter, opts).Decode(&latestMessage)
				if err == nil && latestMessage.UserIP != "" {
					err := storeUserNameByIP(storeCtx, messagesCollection, latestMessage.UserIP, userName, userEmail, client.ID)
					if err != nil {
						fmt.Printf("Warning: Failed to store name by IP: %v\n", err)
					} else {
						fmt.Printf("Stored name '%s' for IP %s from contact collection\n", userName, latestMessage.UserIP)
					}
				}
			}()
		}
	}

	// ✅ NEW: Update conversation state when demo is confirmed
	isDemoConfirmed := checkDemoConfirmed(conversationHistory, message)
	demoTime := extractDemoTime(conversationHistory, message)
	if isDemoConfirmed || demoTime != "" {
		stateUpdates := map[string]interface{}{}
		if isDemoConfirmed {
			stateUpdates["demo_scheduled"] = true
			stateUpdates["ready_to_schedule"] = true
		}
		if demoTime != "" {
			stateUpdates["demo_time"] = demoTime
		}

		if len(stateUpdates) > 0 {
			go func() {
				stateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()

				err := updateConversationState(stateCtx, messagesCollection, client.ID, sessionID, stateUpdates)
				if err != nil {
					fmt.Printf("Warning: Failed to update conversation state: %v\n", err)
				} else {
					fmt.Printf("Successfully updated conversation state: %+v\n", stateUpdates)
				}
			}()
		}
	}

	// Debug: Log current state for troubleshooting
	// Contact collection phase check
	// Removed debug logging for production readiness

	// ✅ Store performance metrics asynchronously
	totalLatency := time.Since(overallStart)
	go storePerformanceMetrics(db, client.ID, sessionID, phaseTimings, int(totalLatency.Milliseconds()), 
		tokenCost, "success", "", len(message), countWords(replyText))

	return replyText, tokenCost, totalLatency, nil
}

// getConversationHistory retrieves recent conversation history
func getConversationHistory(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, sessionID string, limit int) ([]models.Message, error) {
	var messages []models.Message

	cursor, err := collection.Find(ctx,
		bson.M{
			"client_id":       clientID,
			"conversation_id": sessionID,
		},
		&options.FindOptions{
			Sort:  bson.M{"timestamp": -1}, // Latest first
			Limit: &[]int64{int64(limit)}[0],
		},
	)
	if err != nil {
		return messages, err
	}
	defer cursor.Close(ctx)

	if err := cursor.All(ctx, &messages); err != nil {
		return messages, err
	}

	// Reverse to get chronological order (oldest first)
	for i := len(messages)/2 - 1; i >= 0; i-- {
		opp := len(messages) - 1 - i
		messages[i], messages[opp] = messages[opp], messages[i]
	}

	return messages, nil
}

// calculateHistoryTokens calculates total token count for conversation history
func calculateHistoryTokens(ctx context.Context, model *genai.GenerativeModel, messages []models.Message) (int, error) {
	if len(messages) == 0 {
		return 0, nil
	}

	// Build text representation of history for token counting
	var historyText strings.Builder
	for _, msg := range messages {
		historyText.WriteString(fmt.Sprintf("User: %s\nAssistant: %s\n\n", msg.Message, msg.Reply))
	}

	// Use accurate token counting
	tokenCount, err := calculateAccurateTokens(ctx, model, genai.Text(historyText.String()))
	if err != nil {
		// Fallback to estimation if accurate calculation fails
		return len(historyText.String()) / 4, nil
	}

	return tokenCount, nil
}

// getTokenAwareHistory retrieves conversation history with token-aware truncation and summarization
func getTokenAwareHistory(
	ctx context.Context,
	messagesCollection *mongo.Collection,
	clientID primitive.ObjectID,
	sessionID string,
	model *genai.GenerativeModel,
	summarizationService *services.SummarizationService,
) (recentMessages []models.Message, summary string, tokensBefore int, tokensAfter int, summarized bool, summaryRefreshCount int, err error) {
	// Get all messages (up to a reasonable limit)
	allMessages, err := getConversationHistory(ctx, messagesCollection, clientID, sessionID, 1000)
	if err != nil {
		return nil, "", 0, 0, false, 0, fmt.Errorf("failed to get conversation history: %w", err)
	}

	if len(allMessages) == 0 {
		return nil, "", 0, 0, false, 0, nil
	}

	// Calculate total tokens in history
	tokensBefore, err = calculateHistoryTokens(ctx, model, allMessages)
	if err != nil {
		return nil, "", 0, 0, false, 0, fmt.Errorf("failed to calculate history tokens: %w", err)
	}

	// If within limit, return all messages without summarization
	if tokensBefore <= MAX_HISTORY_TOKENS {
		return allMessages, "", tokensBefore, tokensBefore, false, 0, nil
	}

	// Need truncation/summarization - split into recent and old messages
	// Always keep recent messages
	if len(allMessages) <= RECENT_MESSAGES_COUNT {
		// Not enough messages to split, but still over token limit
		// Keep all but mark as needing truncation (this is an edge case)
		return allMessages, "", tokensBefore, tokensBefore, false, 0, nil
	}

	recentMessages = allMessages[len(allMessages)-RECENT_MESSAGES_COUNT:]
	oldMessages := allMessages[:len(allMessages)-RECENT_MESSAGES_COUNT]

	// Calculate tokens for recent messages
	recentTokens, err := calculateHistoryTokens(ctx, model, recentMessages)
	if err != nil {
		return nil, "", 0, 0, false, 0, fmt.Errorf("failed to calculate recent message tokens: %w", err)
	}

	// Try to get or create summary for old messages
	summary, summaryRefreshCount, err = getOrCreateConversationSummary(
		ctx, messagesCollection, clientID, sessionID, oldMessages, summarizationService,
	)
	if err != nil {
		// Fallback: just use recent messages without summary
		fmt.Printf("Warning: Failed to get/create summary, using only recent messages: %v\n", err)
		tokensAfter = recentTokens
		return recentMessages, "", tokensBefore, tokensAfter, false, 0, nil
	}

	// Calculate final token count (recent messages + summary)
	summaryTokens := len(summary) / 4 // Estimation for summary tokens
	tokensAfter = recentTokens + summaryTokens
	summarized = true

	return recentMessages, summary, tokensBefore, tokensAfter, summarized, summaryRefreshCount, nil
}

// getOrCreateConversationSummary retrieves or creates a conversation summary with refresh mechanism
func getOrCreateConversationSummary(
	ctx context.Context,
	messagesCollection *mongo.Collection,
	clientID primitive.ObjectID,
	sessionID string,
	oldMessages []models.Message,
	summarizationService *services.SummarizationService,
) (string, int, error) {
	// Build text from old messages
	var oldText strings.Builder
	for _, msg := range oldMessages {
		oldText.WriteString(fmt.Sprintf("User: %s\nAssistant: %s\n\n", msg.Message, msg.Reply))
	}
	oldMessagesText := oldText.String()

	// Try to get existing summary from database
	summaryCollection := messagesCollection.Database().Collection("conversation_summaries")
	filter := bson.M{
		"conversation_id": sessionID,
		"client_id":       clientID,
	}

	var existingSummary ConversationSummary
	findErr := summaryCollection.FindOne(ctx, filter).Decode(&existingSummary)

	shouldRefresh := false
	summaryExists := (findErr == nil)

	if summaryExists {
		// Summary exists - check if we need to refresh
		existingSummary.UseCount++
		if existingSummary.UseCount >= SUMMARY_REFRESH_CYCLE {
			shouldRefresh = true
			existingSummary.SummaryRefreshCount++
			existingSummary.UseCount = 0
		}
	}

	if summaryExists && !shouldRefresh {
		// Use existing summary and update use count
		update := bson.M{
			"$set": bson.M{
				"use_count":  existingSummary.UseCount,
				"updated_at": time.Now(),
			},
		}
		summaryCollection.UpdateOne(ctx, filter, update)
		return existingSummary.Summary, existingSummary.SummaryRefreshCount, nil
	}

	// Need to create or refresh summary
	result, err := summarizationService.SummarizeText(ctx, oldMessagesText)
	if err != nil {
		// If summarization fails but we have an old summary, use it as fallback
		if summaryExists && existingSummary.Summary != "" {
			fmt.Printf("Warning: Summarization failed, using old summary as fallback: %v\n", err)
			return existingSummary.Summary, existingSummary.SummaryRefreshCount, nil
		}
		return "", 0, fmt.Errorf("summarization failed: %w", err)
	}

	// Get last message ID for tracking
	lastMessageID := primitive.NilObjectID
	if len(oldMessages) > 0 {
		lastMessageID = oldMessages[len(oldMessages)-1].ID
	}

	// Store or update summary
	summaryRefreshCount := 0
	if summaryExists {
		// If we're refreshing, the count was already incremented above
		// Otherwise, it's a new refresh
		if shouldRefresh {
			summaryRefreshCount = existingSummary.SummaryRefreshCount // Already incremented
		} else {
			summaryRefreshCount = existingSummary.SummaryRefreshCount + 1
		}
	} else {
		summaryRefreshCount = 1
	}

	summaryDoc := ConversationSummary{
		ConversationID:      sessionID,
		ClientID:            clientID,
		Summary:             result.Summary,
		LastMessageID:       lastMessageID,
		MessageCount:        len(oldMessages),
		TokenCount:          result.TokenCount,
		UseCount:            0,
		SummaryRefreshCount: summaryRefreshCount,
		UpdatedAt:           time.Now(),
	}

	if summaryExists {
		// Update existing
		update := bson.M{
			"$set": bson.M{
				"summary":               summaryDoc.Summary,
				"last_message_id":       summaryDoc.LastMessageID,
				"message_count":         summaryDoc.MessageCount,
				"token_count":           summaryDoc.TokenCount,
				"use_count":             0,
				"summary_refresh_count": summaryDoc.SummaryRefreshCount,
				"updated_at":            summaryDoc.UpdatedAt,
			},
		}
		summaryCollection.UpdateOne(ctx, filter, update)
	} else {
		// Create new
		summaryDoc.CreatedAt = time.Now()
		summaryCollection.InsertOne(ctx, summaryDoc)
	}

	return result.Summary, summaryDoc.SummaryRefreshCount, nil
}

// getTopicDepth determines the depth of the current topic based on conversation history
func getTopicDepth(history []models.Message, currentMessage string) int {
	// Identify current topic using extractTopics
	currentTopics := extractTopics(currentMessage)
	if len(currentTopics) == 0 {
		return 1 // Default depth
	}

	// Use the first topic found
	currentTopic := currentTopics[0]

	// Check if current message is asking about this topic
	isRelevant := false
	for _, t := range currentTopics {
		if strings.Contains(strings.ToLower(currentMessage), strings.ToLower(t)) {
			isRelevant = true
			break
		}
	}

	if !isRelevant {
		return 1 // Basic response
	}

	// Count how many times this topic appeared in history
	count := countTopicOccurrences(currentTopic, history)
	if count == 0 {
		return 1 // Basic
	} else if count == 1 {
		return 2 // Detailed
	} else {
		return 3 // Comprehensive
	}
}

// extractTopics extracts key topics from a message with enhanced keyword detection
func extractTopics(message string) []string {
	message = strings.ToLower(message)
	topics := []string{}

	// ✅ ENHANCED: Expanded topic keywords with synonyms, related terms, and multi-language support
	topicGroups := map[string][]string{
		"pricing": {
			"price", "pricing", "cost", "costs", "costing", "fee", "fees", "charge", "charges",
			"rate", "rates", "tariff", "tariffs", "quote", "quotation", "quotes", "billing",
			"invoice", "invoices", "pricing", "costing", "charges", "rates", "budget",
			// Hindi/English mixed
			"कीमत", "दाम", "मूल्य", "rate kitna hai", "kitna charge", "kitna hai", "price kya hai",
			"cost kya hai", "kitna paisa", "kitna rupee",
		},
		"database": {
			"database", "data", "databases", "contacts", "contact", "numbers", "number", "phone",
			"phones", "mobile", "mobiles", "records", "record", "list", "lists", "leads",
			"lead", "customer", "customers", "client", "clients",
			// Hindi/English mixed
			"database", "data kitna hai", "kitne contacts", "kitne numbers", "phone numbers",
		},
		"delivery": {
			"delivery", "deliver", "ratio", "delivery ratio", "delivery rate", "reach", "reaching",
			"delivered", "deliveries", "success rate", "delivery success", "delivery percentage",
			"delivery guarantee", "delivery assurance",
			// Hindi/English mixed
			"delivery kitna hai", "kitna delivery", "delivery ratio kya hai",
		},
		"conversion": {
			"conversion", "conversions", "convert", "converting", "cta", "call to action",
			"leads", "lead", "roi", "return on investment", "response", "responses", "reply",
			"replies", "click", "clicks", "click-through", "engagement", "engaged",
			// Hindi/English mixed
			"conversion kitna hai", "kitne leads", "kitna conversion",
		},
		"demo": {
			"demo", "demonstration", "demonstrate", "sample", "trial", "test", "gmeet",
			"meeting", "meetings", "schedule", "scheduled", "appointment", "appointments",
			"live demo", "video call", "zoom", "google meet", "meet", "call",
			// Hindi/English mixed
			"demo chahiye", "demo kitna hai", "demo de sakte ho", "demo dene ka",
		},
		"package": {
			"package", "packages", "plan", "plans", "planning", "pkg", "pkgs", "scheme",
			"schemes", "deal", "deals", "offer", "offers", "option", "options",
			// Hindi/English mixed
			"package kitna hai", "kitne packages", "plan kya hai",
		},
		"messaging": {
			"message", "messages", "messaging", "send", "sending", "sms", "whatsapp",
			"bulk", "bulk messaging", "campaign", "campaigns", "marketing", "promotional",
			// Hindi/English mixed
			"message kaise bhejte ho", "kitne messages", "messaging kaise hota hai",
		},
		"how_it_works": {
			"how", "how it works", "how does it work", "process", "procedure", "steps",
			"step", "workflow", "method", "methods", "way", "ways", "explain", "explanation",
			"understand", "understandable", "guide", "tutorial", "help", "helps",
			// Hindi/English mixed
			"kaise kaam karta hai", "kaise hota hai", "process kya hai", "kaise use karein",
		},
		"minimum": {
			"minimum", "min", "smallest", "least", "lowest", "small", "few", "fewer",
			"minimum order", "minimum quantity", "minimum messages", "starting", "start",
			// Hindi/English mixed
			"minimum kitna hai", "kitna minimum", "kam se kam",
		},
	}

	// Check for each topic group
	seen := make(map[string]bool)
	for topic, keywords := range topicGroups {
		for _, keyword := range keywords {
			// Check if keyword exists in message (case-insensitive, word boundary aware)
			if strings.Contains(message, keyword) && !seen[topic] {
				// Avoid false positives (e.g., "price" in "appreciate")
				if topic == "pricing" && (strings.Contains(message, "appreciate") || 
					strings.Contains(message, "precious") || strings.Contains(message, "precise")) {
					continue
				}
				topics = append(topics, topic)
				seen[topic] = true
				break // Found a keyword for this topic, move to next topic
			}
		}
	}

	// If no topics found, return general
	if len(topics) == 0 {
		topics = []string{"general"}
	}

	return topics
}

// calculateTopicSimilarity calculates similarity between two sets of topics
func calculateTopicSimilarity(topics1, topics2 []string) float64 {
	if len(topics1) == 0 && len(topics2) == 0 {
		return 1.0
	}
	if len(topics1) == 0 || len(topics2) == 0 {
		return 0.0
	}

	matches := 0
	for _, t1 := range topics1 {
		for _, t2 := range topics2 {
			if t1 == t2 {
				matches++
				break
			}
		}
	}

	maxLen := len(topics1)
	if len(topics2) > maxLen {
		maxLen = len(topics2)
	}

	return float64(matches) / float64(maxLen)
}

// detectRepeatedQuestion checks if the current question is similar to a previously asked question
func detectRepeatedQuestion(currentMessage string, history []models.Message) (bool, int, string) {
	currentTopics := extractTopics(currentMessage)

	// Check last 5 user messages
	checkLimit := 5
	if len(history) < checkLimit {
		checkLimit = len(history)
	}

	for i := len(history) - 1; i >= len(history)-checkLimit && i >= 0; i-- {
		historyTopics := extractTopics(history[i].Message)
		similarity := calculateTopicSimilarity(currentTopics, historyTopics)

		if similarity > 0.6 { // 60% similarity threshold
			return true, len(history) - i, history[i].Message
		}
	}

	return false, 0, ""
}

// detectSimpleAnswer checks if the user's message is a simple answer (like a city name) to a previous question
func detectSimpleAnswer(currentMessage string, history []models.Message) (bool, string) {
	// Normalize the current message
	currentMsg := strings.TrimSpace(strings.ToLower(currentMessage))
	
	// Check if it's a simple input (short, few words)
	if len(currentMsg) > 30 || len(strings.Fields(currentMsg)) > 3 {
		return false, ""
	}

	// Check if there's a recent question in the conversation history
	if len(history) == 0 {
		return false, ""
	}

	// Check the last AI response for a question mark or question pattern
	lastAIResponse := ""
	for i := len(history) - 1; i >= 0 && i >= len(history)-3; i-- {
		if history[i].Reply != "" {
			lastAIResponse = history[i].Reply
			break
		}
	}

	if lastAIResponse == "" {
		return false, ""
	}

	// Check if the last AI response contains a question
	hasQuestion := strings.Contains(lastAIResponse, "?") || 
		strings.Contains(strings.ToLower(lastAIResponse), "which") ||
		strings.Contains(strings.ToLower(lastAIResponse), "what") ||
		strings.Contains(strings.ToLower(lastAIResponse), "how") ||
		strings.Contains(strings.ToLower(lastAIResponse), "where") ||
		strings.Contains(strings.ToLower(lastAIResponse), "when")

	if hasQuestion {
		return true, lastAIResponse
	}

	return false, ""
}

// isRepeatedSimpleInput checks if the user provided the same simple input (like a city name) multiple times
func isRepeatedSimpleInput(currentMessage string, history []models.Message) bool {
	// Normalize the current message (trim, lowercase)
	currentMsg := strings.TrimSpace(strings.ToLower(currentMessage))
	
	// Skip if the message is too long (likely a full question, not a simple input)
	if len(currentMsg) > 30 || len(strings.Fields(currentMsg)) > 3 {
		return false
	}

	// Check if this exact input appears in recent user messages (last 5 messages)
	checkLimit := 5
	if len(history) < checkLimit {
		checkLimit = len(history)
	}

	count := 0
	for i := len(history) - 1; i >= len(history)-checkLimit && i >= 0; i-- {
		historyMsg := strings.TrimSpace(strings.ToLower(history[i].Message))
		// Exact match (normalized)
		if historyMsg == currentMsg {
			count++
		}
	}

	// If the same simple input appears 2+ times, it's repeated
	return count >= 1
}

// countTopicOccurrences counts how many times a topic has been discussed
func countTopicOccurrences(topic string, history []models.Message) int {
	count := 0
	topicLower := strings.ToLower(topic)

	for _, msg := range history {
		msgLower := strings.ToLower(msg.Message)
		topics := extractTopics(msg.Message)
		for _, t := range topics {
			if t == topicLower || strings.Contains(msgLower, topicLower) {
				count++
				break
			}
		}
	}

	return count
}

// detectLastTopic detects the main topic from conversation history
func detectLastTopic(history []models.Message, currentMessage string) string {
	topics := map[string][]string{
		"pricing":    {"charge", "price", "cost", "rate", "package"},
		"database":   {"database", "data", "contacts", "numbers"},
		"delivery":   {"delivery", "ratio", "rate", "reach"},
		"conversion": {"conversion", "cta", "leads", "roi"},
		"demo":       {"demo", "sample", "test", "gmeet", "meeting"},
	}

	// Check current message first
	messageLower := strings.ToLower(currentMessage)
	for topic, keywords := range topics {
		for _, keyword := range keywords {
			if strings.Contains(messageLower, keyword) {
				return topic
			}
		}
	}

	// Check history (most recent first)
	for i := len(history) - 1; i >= 0 && i >= len(history)-5; i-- {
		msgLower := strings.ToLower(history[i].Message)
		for topic, keywords := range topics {
			for _, keyword := range keywords {
				if strings.Contains(msgLower, keyword) {
					return topic
				}
			}
		}
	}

	return "general"
}

// detectRepeatedPhrase checks if a specific phrase appears in AI responses multiple times
func detectRepeatedPhrase(phrase string, history []models.Message, threshold int) (bool, int) {
	count := 0
	phraseLower := strings.ToLower(phrase)

	// Check last 10 AI responses
	checkLimit := 10
	if len(history) < checkLimit {
		checkLimit = len(history)
	}

	for i := len(history) - 1; i >= len(history)-checkLimit && i >= 0; i-- {
		// Check AI replies for the phrase
		if strings.Contains(strings.ToLower(history[i].Reply), phraseLower) {
			count++
			if count >= threshold {
				return true, count
			}
		}
	}

	return false, count
}

// detectRepeatedCTA detects if the same call-to-action phrase appears multiple times in AI responses
func detectRepeatedCTA(history []models.Message) (bool, string, int) {
	// Common CTA phrases to track
	ctaPhrases := []string{
		"shall we proceed with scheduling",
		"would you like to schedule",
		"can we schedule a demo",
		"would you like a demo",
		"shall we proceed",
		"ready to schedule",
		"would you like to know more about",
		"can i help you with anything else",
		"would you prefer a whatsapp call or gmeet",
		"during the demo, we can also discuss",
		"can we proceed",
		"shall we continue",
		"would you like me to",
	}

	for _, phrase := range ctaPhrases {
		isRepeated, count := detectRepeatedPhrase(phrase, history, 2)
		if isRepeated {
			return true, phrase, count
		}
	}

	return false, "", 0
}

// checkDemoConfirmed checks if the user has confirmed scheduling a demo
func checkDemoConfirmed(history []models.Message, currentMessage string) bool {
	currentLower := strings.ToLower(currentMessage)

	// Check current message for confirmations
	confirmations := []string{
		"yes", "yup", "yeah", "sure", "ok", "okay", "alright", "fine",
		"schedule", "scheduled", "confirm", "confirmed", "done",
		"haan", "haan", "thik hai", "theek hai",
	}

	for _, confirm := range confirmations {
		if strings.Contains(currentLower, confirm) {
			// Also check if demo-related context exists
			demoKeywords := []string{"demo", "meeting", "call", "schedule", "7", "pm", "clock", "time"}
			for _, keyword := range demoKeywords {
				if strings.Contains(currentLower, keyword) {
					return true
				}
			}
			// Check if previous messages were about demo
			if len(history) > 0 {
				lastReply := strings.ToLower(history[len(history)-1].Reply)
				for _, keyword := range demoKeywords {
					if strings.Contains(lastReply, keyword) {
						return true
					}
				}
			}
		}
	}

	// Check history for confirmations
	for _, msg := range history {
		msgLower := strings.ToLower(msg.Message)
		for _, confirm := range confirmations {
			if strings.Contains(msgLower, confirm) {
				// Check if demo context exists in nearby messages
				demoKeywords := []string{"demo", "meeting", "call", "schedule", "gmeet"}
				for _, keyword := range demoKeywords {
					if strings.Contains(msgLower, keyword) {
						return true
					}
				}
				// Check AI reply for demo context
				replyLower := strings.ToLower(msg.Reply)
				for _, keyword := range demoKeywords {
					if strings.Contains(replyLower, keyword) {
						return true
					}
				}
			}
		}
	}

	return false
}

// extractDemoTime extracts demo time from conversation history and current message
func extractDemoTime(history []models.Message, currentMessage string) string {
	currentLower := strings.ToLower(currentMessage)

	// Time patterns to look for
	timePatterns := []string{
		"7 pm", "7pm", "7 o clock", "7 o'clock", "7 oclock",
		"7:00 pm", "7:00pm", "seven pm", "seven o clock",
		"evening", "tonight", "today",
	}

	// Check current message first
	for _, pattern := range timePatterns {
		if strings.Contains(currentLower, pattern) {
			// Try to extract a more complete time string
			if idx := strings.Index(currentLower, pattern); idx >= 0 {
				start := idx - 10
				if start < 0 {
					start = 0
				}
				end := idx + len(pattern) + 10
				if end > len(currentLower) {
					end = len(currentLower)
				}
				extracted := strings.TrimSpace(currentMessage[start:end])
				if len(extracted) > 0 {
					return extracted
				}
			}
		}
	}

	// Check history for time mentions
	for _, msg := range history {
		msgLower := strings.ToLower(msg.Message)
		for _, pattern := range timePatterns {
			if strings.Contains(msgLower, pattern) {
				// Return the message containing the time
				if idx := strings.Index(msgLower, pattern); idx >= 0 {
					start := idx - 10
					if start < 0 {
						start = 0
					}
					end := idx + len(pattern) + 10
					if end > len(msg.Message) {
						end = len(msg.Message)
					}
					extracted := strings.TrimSpace(msg.Message[start:end])
					if len(extracted) > 0 {
						return extracted
					}
				}
			}
		}
	}

	return ""
}

// buildContextWithHistory creates context string including conversation history and optional summary
func buildContextWithHistory(chunks []models.ContentChunk, history []models.Message, historySummary string) string {
	var contextStr strings.Builder

	// Add PDF context first (more important for company info)
	if len(chunks) > 0 {
		// Building context with PDF chunks
		contextStr.WriteString("COMPANY INFORMATION:\n\n")
		for _, chunk := range chunks {
			contextStr.WriteString(fmt.Sprintf("%s\n\n", chunk.Text))
		}
		contextStr.WriteString("---\n\n")
	} else {
		// No PDF chunks available for context
	}

	// Add conversation summary if available (older messages)
	if historySummary != "" {
		contextStr.WriteString("Conversation Summary (earlier messages):\n")
		contextStr.WriteString(historySummary)
		contextStr.WriteString("\n\n---\n\n")
	}

	// Add recent conversation history if available
	if len(history) > 0 {
		contextStr.WriteString("Recent conversation context:\n")
		for _, msg := range history {
			contextStr.WriteString(fmt.Sprintf("User: %s\n", msg.Message))
			contextStr.WriteString(fmt.Sprintf("Assistant: %s\n\n", msg.Reply))
		}
		contextStr.WriteString("---\n\n")
	}

	// Context prepared for AI generation
	return contextStr.String()
}

func buildPromptWithHistory(clientName, contextStr string, history []models.Message, currentMessage string, hasDocuments bool) string {
	hasHistory := len(history) > 0
	var prompt strings.Builder

	// ========================================
	// 🚨 CRITICAL: CLIENT DATA ISOLATION
	// ========================================
	prompt.WriteString("🔒 CLIENT DATA ISOLATION PROTOCOL:\n")
	prompt.WriteString("You are serving a SPECIFIC client with UNIQUE data. Follow these STRICT rules:\n")
	prompt.WriteString("1. Use ONLY the persona and documents provided below for THIS client\n")
	prompt.WriteString("2. NEVER reference data from other clients, previous conversations with different clients, or generic examples\n")
	prompt.WriteString("3. NEVER use placeholder data (555-xxx-xxxx, info@company.com, etc.)\n")
	prompt.WriteString("4. If information is NOT in the client's persona or documents, say: 'I don't have that information for our company'\n")
	prompt.WriteString("5. CRITICAL: This client's data is SACRED - treat it as the ONLY source of truth\n\n")

	// ========================================
	// ✅ CHECK FOR AI PERSONA
	// ========================================
	hasPersona := strings.Contains(contextStr, "AI PERSONALITY & KNOWLEDGE:")

	// ========================================
	// 🎯 PERSONA-FIRST ARCHITECTURE
	// ========================================
	if contextStr != "" {
		if hasPersona {
			prompt.WriteString("🎯 YOUR IDENTITY & KNOWLEDGE BASE:\n")
			prompt.WriteString("The following section contains YOUR UNIQUE PERSONALITY and ALL INFORMATION you know.\n")
			prompt.WriteString("This is NOT generic data - this is YOUR CLIENT'S SPECIFIC identity, services, and knowledge.\n\n")
		}

		// ========================================
		// 📚 INJECT CLIENT-SPECIFIC KNOWLEDGE
		// ========================================
		prompt.WriteString("=== YOUR COMPLETE KNOWLEDGE BASE ===\n")
		prompt.WriteString(contextStr)
		prompt.WriteString("\n=== END OF KNOWLEDGE BASE ===\n\n")

		// ========================================
		// 🚨 NO DOCUMENTS MODE - PERSONA ONLY
		// ========================================
		if !hasDocuments && hasPersona {
			prompt.WriteString("⚠️ INFORMATION AVAILABILITY STATUS:\n")
			prompt.WriteString("• This client has NO uploaded documents or PDFs\n")
			prompt.WriteString("• Your ENTIRE knowledge comes from the 'AI PERSONALITY & KNOWLEDGE' section above\n")
			prompt.WriteString("• DO NOT reference company documents, policies, or detailed specifications unless explicitly stated in the persona\n")
			prompt.WriteString("• If payment details, pricing, contact info, or services ARE in the persona above, PROVIDE them completely\n")
			prompt.WriteString("• If asked about details NOT in the persona, respond: 'I don't have that specific information available'\n\n")

			prompt.WriteString("PERSONA-ONLY MODE RULES:\n")
			prompt.WriteString("1. The persona section above is your ONLY information source\n")
			prompt.WriteString("2. NEVER invent company-specific details not mentioned in the persona\n")
			prompt.WriteString("3. If persona contains pricing/services/contact info, SHARE it confidently\n")
			prompt.WriteString("4. If persona lacks specific details, acknowledge the limitation honestly\n")
			prompt.WriteString(fmt.Sprintf("5. When asked about company name, use: '%s' (unless persona specifies otherwise)\n", clientName))
			prompt.WriteString("6. DO NOT reference 'documents', 'PDFs', or 'knowledge base' in responses\n\n")
		} else if hasPersona {
			prompt.WriteString("PERSONA + DOCUMENTS MODE:\n")
			prompt.WriteString("• You have BOTH persona guidelines AND company documents\n")
			prompt.WriteString("• Persona defines HOW you communicate (tone, style, priorities)\n")
			prompt.WriteString("• Documents contain WHAT information you can share (services, policies, details)\n")
			prompt.WriteString("• Use persona to guide your responses, documents to provide specific information\n")
			prompt.WriteString("• If information exists in EITHER source, share it confidently\n\n")
		} else {
			prompt.WriteString("DOCUMENTS-ONLY MODE:\n")
			prompt.WriteString("• You have company documents/PDFs with detailed information\n")
			prompt.WriteString("• Use ONLY the information from these documents\n")
			prompt.WriteString("• Maintain a professional, helpful support representative tone\n")
			prompt.WriteString("• If information is not in the documents, acknowledge the limitation\n\n")
		}
	} else {
		// ========================================
		// ❌ ZERO KNOWLEDGE STATE
		// ========================================
		prompt.WriteString("⚠️ LIMITED INFORMATION MODE:\n")
		prompt.WriteString(fmt.Sprintf("You are a customer support representative for %s.\n", clientName))
		prompt.WriteString("Currently, you don't have access to detailed company information.\n")
		prompt.WriteString("Politely inform customers you'll connect them with the team for specific details.\n")
		prompt.WriteString(fmt.Sprintf("CRITICAL: Use company name '%s' consistently. Do NOT use any other company name.\n\n", clientName))
	}

	// ========================================
	// 🌐 MULTI-LANGUAGE SUPPORT
	// ========================================
	prompt.WriteString("LANGUAGE DETECTION & RESPONSE:\n")
	prompt.WriteString("• DETECT user's language automatically (English, Hindi, Marathi, etc.)\n")
	prompt.WriteString("• RESPOND in the SAME language they use\n")
	prompt.WriteString("• Support Hindi: है, हैं, क्या, कैसे | Marathi: आहे, आहेत, का, कसे\n\n")

	// ========================================
	// ✅ INFORMATION ACCURACY RULES
	// ========================================
	prompt.WriteString("INFORMATION SHARING PROTOCOL:\n")
	prompt.WriteString("✅ WHEN TO SHARE:\n")
	prompt.WriteString("• If pricing/payment info EXISTS in your knowledge → PROVIDE it completely\n")
	prompt.WriteString("• If contact details EXIST in your knowledge → SHARE them fully (phone, email, address)\n")
	prompt.WriteString("• If services/features EXIST in your knowledge → DESCRIBE them confidently\n")
	prompt.WriteString("• Always cite from YOUR knowledge base - never invent\n")
	prompt.WriteString("• SEARCH your knowledge base FIRST before responding:\n")
	prompt.WriteString("  - For CONTACT questions: Look for phone numbers, emails, addresses in persona/PDF\n")
	prompt.WriteString("  - For PAYMENT questions: Look for payment methods, banking details in persona/PDF\n")
	prompt.WriteString("  - Extract the EXACT information from your knowledge base\n\n")

	prompt.WriteString("❌ WHEN TO REFUSE:\n")
	prompt.WriteString("• If information is NOT in your knowledge base → Say: 'I don't have that information available'\n")
	prompt.WriteString("• NEVER create fake contact details (555-xxx-xxxx, generic emails)\n")
	prompt.WriteString("• NEVER describe services not mentioned in your knowledge\n")
	prompt.WriteString("• NEVER use examples from other companies or generic templates\n\n")

	// ========================================
	// 💬 CONVERSATION STYLE
	// ========================================
	prompt.WriteString("COMMUNICATION STYLE:\n")
	prompt.WriteString("• Sound natural and conversational - like a helpful team member\n")
	prompt.WriteString("• Use 'we' and 'our company' when referring to the business\n")
	prompt.WriteString("• Be confident about information you DO have\n")
	prompt.WriteString("• Be honest about information you DON'T have\n")
	prompt.WriteString("• Use markdown **bold** for key terms (2-4 per message)\n")
	prompt.WriteString("• End with context-specific follow-up questions (not generic)\n\n")

	// ========================================
	// 📊 PROGRESSIVE DISCLOSURE & FOLLOW-UP QUESTIONS
	// ========================================
	prompt.WriteString("PROGRESSIVE INFORMATION DISCLOSURE:\n")
	prompt.WriteString("When user asks about the SAME topic multiple times, expand your answers:\n")
	prompt.WriteString("• Depth 1 (First time): Basic answer with key facts\n")
	prompt.WriteString("• Depth 2 (Second time): Add details, examples, or specific use cases\n")
	prompt.WriteString("• Depth 3 (Third+ time): Comprehensive answer with metrics, case studies, or offer expert connection\n")
	prompt.WriteString("DO NOT repeat the exact same answer word-for-word when topic repeats\n\n")

	prompt.WriteString("CONTEXT-SPECIFIC FOLLOW-UP QUESTIONS:\n")
	prompt.WriteString("❌ NEVER use generic questions like:\n")
	prompt.WriteString("   - 'Would you like to know more about the features and benefits?'\n")
	prompt.WriteString("   - 'Do you have any other questions?'\n")
	prompt.WriteString("   - 'Is there anything else I can help with?'\n\n")

	// Detect last topic and provide context-specific follow-up
	lastTopic := detectLastTopic(history, currentMessage)
	topicDepth := getTopicDepth(history, currentMessage)

	// Context-specific follow-up map
	contextMap := map[string]string{
		"pricing":    "For a 1 lac campaign at ₹60,000, that's 60 paisa per message. What's your target cost per acquisition?",
		"database":   "Which cities/states should we prioritize for your campaigns? I can check our database availability.",
		"delivery":   "With 80% delivery on 1 lac messages, that's 80,000 potential customers. What conversion rate are you targeting?",
		"conversion": "Our real estate clients typically see 3-5% lead conversion. What would 3,000-4,000 qualified leads mean for your business?",
		"demo":       "I can arrange a 5-minute live demo today. Morning (11 AM-1 PM) or evening (5-7 PM) - which suits you?",
		"messaging":  "What scale are you planning for? This helps me suggest the best package and delivery timeline.",
		"general":    "What specific aspect would you like to explore next?",
	}

	if followUp, exists := contextMap[lastTopic]; exists {
		prompt.WriteString(fmt.Sprintf("✅ USE THIS FOLLOW-UP (based on last topic '%s'):\n", lastTopic))
		prompt.WriteString(fmt.Sprintf("   '%s'\n\n", followUp))
	} else {
		prompt.WriteString("✅ ALWAYS use context-specific questions based on the topic discussed:\n")
		prompt.WriteString("   - After pricing: 'Would you like a detailed ROI breakdown for a 1 lac message campaign?'\n")
		prompt.WriteString("   - After database info: 'Which cities/salary ranges should we target for your real estate projects?'\n")
		prompt.WriteString("   - After delivery ratio: 'With 80% delivery, that's 80,000 potential customers. What's your conversion goal?'\n")
		prompt.WriteString("   - After conversion info: 'What's your target for lead generation? I can show you how our CTA buttons achieve 15-25% click-through rates.'\n")
		prompt.WriteString("   - After demo discussion: 'What time works best for you? I can schedule a 5-minute demo to show you the platform.'\n\n")
	}

	// Add topic depth information
	prompt.WriteString(fmt.Sprintf("CURRENT TOPIC DEPTH: %d (provide depth-%d answer)\n", topicDepth, topicDepth))
	prompt.WriteString("- Depth 1: Basic answer (60 words)\n")
	prompt.WriteString("- Depth 2: Detailed answer with examples/metrics (100-150 words)\n")
	prompt.WriteString("- Depth 3: Comprehensive answer + offer expert connection (150+ words)\n\n")

	// ========================================
	// 📞 CONTACT COLLECTION FLOW
	// ========================================
	prompt.WriteString("CONTACT INFORMATION COLLECTION:\n")
	prompt.WriteString("TRIGGER: Only when user explicitly asks for contact details (phone, email, 'how to contact', etc.)\n")
	prompt.WriteString("FLOW:\n")
	prompt.WriteString("1. Provide available contact info + ask: 'May I have your name?'\n")
	prompt.WriteString("2. Thank them + ask: 'Could you share your email ID?'\n")
	prompt.WriteString("3. Confirm: 'Thank you! Our team will contact you shortly.' (END)\n")
	prompt.WriteString("DO NOT trigger for general questions, pricing, services, or non-contact queries\n\n")

	// ========================================
	// 🔄 CONVERSATION CONTEXT
	// ========================================
	if hasHistory {
		prompt.WriteString("PREVIOUS CONVERSATION:\n")
		for _, msg := range history {
			prompt.WriteString(fmt.Sprintf("Customer: %s\n", msg.Message))
			prompt.WriteString(fmt.Sprintf("You: %s\n\n", msg.Reply))
		}
		prompt.WriteString("CONTEXT RETENTION:\n")
		prompt.WriteString("• REMEMBER what the user already told you\n")
		prompt.WriteString("• DO NOT re-introduce yourself or repeat welcome messages\n")
		prompt.WriteString("• DO NOT ask for information they already provided\n")
		prompt.WriteString("• Reference previous topics naturally when relevant\n\n")

		// ========================================
		// 🚨 CRITICAL: ANTI-REPETITION ENFORCEMENT
		// ========================================
		hasRepeatedCTA, ctaPhrase, ctaCount := detectRepeatedCTA(history)
		if hasRepeatedCTA {
			prompt.WriteString("🚨 CRITICAL: PHRASE BLOCKING ENFORCEMENT:\n")
			prompt.WriteString(fmt.Sprintf("The following phrase has been USED %d TIMES. It is now BANNED:\n", ctaCount))
			prompt.WriteString(fmt.Sprintf("❌ BANNED PHRASE: '%s'\n\n", ctaPhrase))

			// Generate variation warnings
			variations := []string{}
			if strings.Contains(ctaPhrase, "shall we proceed") {
				variations = append(variations, "let's proceed", "would you like to proceed", "can we proceed", "shall we continue")
			} else if strings.Contains(ctaPhrase, "would you like") {
				variations = append(variations, "do you want", "are you interested in", "shall we", "can we")
			} else if strings.Contains(ctaPhrase, "can we") {
				variations = append(variations, "shall we", "would you like to", "let's")
			}

			if len(variations) > 0 {
				prompt.WriteString("❌ Also AVOID these variations:\n")
				for _, variation := range variations {
					prompt.WriteString(fmt.Sprintf("   - '%s'\n", variation))
				}
				prompt.WriteString("\n")
			}

			prompt.WriteString("✅ INSTEAD, use these alternatives:\n")
			prompt.WriteString("   - 'What time works best for you?'\n")
			prompt.WriteString("   - 'I'll set that up - what's your preferred contact method?'\n")
			prompt.WriteString("   - 'Great! Let me confirm those details.'\n")
			prompt.WriteString("   - 'Perfect! What else would you like to know before we begin?'\n")
			prompt.WriteString("   - 'Excellent! Here's what happens next...'\n\n")

			prompt.WriteString("CRITICAL RULES:\n")
			prompt.WriteString("- DO NOT use the banned phrase OR its variations\n")
			prompt.WriteString("- If user already agreed to something (demo, pricing, etc.), STOP asking and MOVE FORWARD\n")
			prompt.WriteString("- After user says 'yes' or confirms something, ask for NEXT required information, not the same question\n")
			prompt.WriteString("- Once demo is confirmed → Switch to next step (collecting details for the meeting)\n")
			prompt.WriteString("- Skip the CTA entirely and provide new value instead\n\n")
		}

		// Check for conversation state (demo scheduled, user confirmations)
		isDemoConfirmed := checkDemoConfirmed(history, currentMessage)
		demoTime := extractDemoTime(history, currentMessage)

		if isDemoConfirmed {
			prompt.WriteString("✅ CONVERSATION STATE: Demo has been confirmed by the user\n")
			if demoTime != "" {
				prompt.WriteString(fmt.Sprintf("✅ USER PROVIDED DEMO TIME: %s\n", demoTime))
			}
			prompt.WriteString("- DO NOT ask again about scheduling the demo\n")
			prompt.WriteString("- Move forward with next steps (collect meeting details, confirm time, etc.)\n")
			prompt.WriteString("- Focus on preparing for the scheduled demo rather than re-offering it\n\n")
		} else if demoTime != "" {
			prompt.WriteString(fmt.Sprintf("✅ USER PROVIDED DEMO TIME: %s\n", demoTime))
			prompt.WriteString("- Acknowledge the time and move forward\n")
			prompt.WriteString("- DO NOT ask again about the time\n")
			prompt.WriteString("- Proceed with confirming other details or next steps\n\n")
		}
	} else {
		prompt.WriteString("FIRST MESSAGE:\n")
		prompt.WriteString("• Briefly introduce yourself (max 2 sentences)\n")
		prompt.WriteString("• Keep response under 60 words\n")
		prompt.WriteString("• Immediately address their question\n\n")
	}

	// ========================================
	// ❓ CURRENT USER MESSAGE
	// ========================================
	prompt.WriteString(fmt.Sprintf("USER'S CURRENT MESSAGE: \"%s\"\n\n", currentMessage))

	// ========================================
	// 🎯 RESPONSE TASK
	// ========================================
	prompt.WriteString("YOUR RESPONSE TASK:\n")
	prompt.WriteString("1. DETECT user's language and respond in the SAME language\n")
	prompt.WriteString("2. Use ONLY information from YOUR knowledge base (above)\n")
	prompt.WriteString("3. If information EXISTS in your knowledge → SHARE it confidently\n")
	prompt.WriteString("4. If information DOESN'T EXIST → Say honestly: 'I don't have that information'\n")
	prompt.WriteString("5. NEVER use data from other clients, generic templates, or placeholder text\n")
	prompt.WriteString("6. Structure: ANSWER (1-2 sentences) → ADD VALUE (1 sentence) → OFFER NEXT STEP (context-specific)\n")
	prompt.WriteString("7. Use **bold** for key terms, end with relevant follow-up question\n")
	prompt.WriteString("8. Keep responses 50-100 words unless explaining complex information\n\n")

	// ========================================
	// 🚫 PROHIBITED BEHAVIORS
	// ========================================
	prompt.WriteString("ABSOLUTELY PROHIBITED:\n")
	prompt.WriteString("❌ Creating fake contact details (555-xxx-xxxx, generic@company.com)\n")
	prompt.WriteString("❌ Using services/products not in YOUR knowledge base\n")
	prompt.WriteString("❌ Referencing 'documents', 'PDFs', or 'knowledge base' in responses\n")
	prompt.WriteString("❌ Repeating introductions in ongoing conversations\n")
	prompt.WriteString("❌ REPEATING information you already provided in previous messages (this is CRITICAL)\n")
	prompt.WriteString("❌ Repeating descriptions, explanations, or facts you already mentioned\n")
	prompt.WriteString("❌ CONFUSING different question types - DO NOT give payment methods when user asks 'how to connect'\n")
	prompt.WriteString("❌ CONFUSING different question types - DO NOT give contact info when user asks 'what payment methods'\n")
	prompt.WriteString("❌ REPEATING the same answer when user asks follow-up questions - if user asks 'what will be the cost' after you gave rate, CALCULATE the cost, don't repeat the rate\n")
	prompt.WriteString("❌ NOT performing calculations when asked for cost - if user asks 'what will be the cost for X messages', CALCULATE it (quantity × rate), don't just repeat the rate\n")
	prompt.WriteString("❌ Using data from other clients or generic examples\n")
	prompt.WriteString("❌ Inventing pricing, policies, or company details\n")
	prompt.WriteString("❌ Refusing to share information that EXISTS in your knowledge\n\n")

	prompt.WriteString("REMEMBER: You serve ONE client with UNIQUE data. Treat their persona and documents as your ONLY source of truth.\n")

	return prompt.String()
}

// estimateTokenCostWithHistory provides token cost estimation including conversation history
func estimateTokenCostWithHistory(userMessage, aiReply string, contextChunks, historyCount int) int {
	userTokens := len(userMessage) / 4
	replyTokens := len(aiReply) / 4
	contextTokens := contextChunks * 50
	historyTokens := historyCount * 100 // Rough estimate for conversation history

	total := userTokens + replyTokens + contextTokens + historyTokens

	if total < 20 {
		total = 20
	}

	return total
}

// ===================
// CONTACT COLLECTION STATE MANAGEMENT
// ===================

// getContactCollectionState retrieves the current contact collection state for a conversation
func getContactCollectionState(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, sessionID string) (string, bool, error) {
	filter := bson.M{
		"client_id":       clientID,
		"conversation_id": sessionID,
		"is_embed_user":   true,
	}

	opts := options.FindOne().SetSort(bson.M{"timestamp": -1})
	var message models.Message
	err := collection.FindOne(ctx, filter, opts).Decode(&message)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "none", false, nil // Default state
		}
		return "none", false, err
	}

	phase := message.ContactCollectionPhase
	if phase == "" {
		phase = "none"
	}

	return phase, message.ChatDisabled, nil
}

// updateContactCollectionState updates the contact collection state for a conversation
func updateContactCollectionState(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, sessionID string, phase string, userName, userEmail string, chatDisabled bool) error {
	filter := bson.M{
		"client_id":       clientID,
		"conversation_id": sessionID,
		"is_embed_user":   true,
	}

	update := bson.M{
		"$set": bson.M{
			"contact_collection_phase": phase,
			"chat_disabled":            chatDisabled,
		},
	}

	// Add user details if provided
	if userName != "" {
		update["$set"].(bson.M)["user_name"] = userName
		update["$set"].(bson.M)["from_name"] = userName // Also update from_name
	}
	if userEmail != "" {
		update["$set"].(bson.M)["user_email"] = userEmail
	}

	// Update the most recent message
	opts := options.FindOneAndUpdate().SetSort(bson.M{"timestamp": -1})
	var updatedMessage models.Message
	err := collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&updatedMessage)
	if err != nil {
		return fmt.Errorf("failed to update contact collection state: %w", err)
	}

	// If we have a userName, update all previous messages in this conversation
	if userName != "" {
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			updateFilter := bson.M{
				"client_id":       clientID,
				"conversation_id": sessionID,
				"is_embed_user":   true,
				"from_name":       "Embed User", // Only update messages that still have "Embed User"
			}

			updateAll := bson.M{
				"$set": bson.M{
					"from_name": userName,
					"user_name": userName,
				},
			}

			result, err := collection.UpdateMany(updateCtx, updateFilter, updateAll)
			if err != nil {
				fmt.Printf("Warning: Failed to update previous messages with name: %v\n", err)
			} else {
				fmt.Printf("Updated %d previous messages with name: %s\n", result.ModifiedCount, userName)
			}
		}()
	}

	return nil
}

// isContactQuery checks if the message contains contact-related keywords
func isContactQuery(message string) bool {
	contactKeywords := []string{
		"contact number", "phone number", "email", "how to contact", "reach you",
		"get in touch", "support contact", "customer service", "helpline", "call",
		"write to", "aapka contact", "aapka phone", "aapka email", "kaise contact kare",
		"customer care", "support", "help", "office ka number", "business ka number",
		"how i can connect", "how can i connect", "how to connect", "connect with you",
		"connect with", "can i connect", "want to connect", "i want to connect",
		"reach out", "contact you", "speak with", "talk to", "get in touch with",
	}

	messageLower := strings.ToLower(message)
	for _, keyword := range contactKeywords {
		if strings.Contains(messageLower, keyword) {
			return true
		}
	}
	return false
}

// isNameProvided checks if the message looks like a name
func isNameProvided(message string) bool {
	message = strings.TrimSpace(message)
	if len(message) < 2 || len(message) > 50 {
		return false
	}

	// If it contains an email, it's not just a name
	if isEmailProvided(message) {
		return false
	}

	// Check for common non-name words (exact matches only)
	nonNameWords := []string{
		"email", "phone", "contact", "number", "address", "help", "question", "problem", "issue",
		"email id", "phone number", "contact number", "mobile number", "address", "pata", "janna",
		"batayein", "batao", "bataiye", "help", "madad", "sahayata", "problem", "masla", "issue",
		"question", "sawal", "puchna", "puchta", "puchti", "puchte", "puchta hun", "puchti hun",
		"thank", "thanks", "dhanyavaad", "ok", "okay", "yes", "no", "hi", "hello", "hey",
		"how can i contact", "support", "reach out", "get in touch",
	}

	messageLower := strings.ToLower(message)
	for _, word := range nonNameWords {
		if strings.Contains(messageLower, word) {
			return false
		}
	}

	// Check if it looks like a name (contains letters and possibly spaces)
	hasLetters := false
	hasNumbers := false
	for _, char := range message {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') {
			hasLetters = true
		}
		if char >= '0' && char <= '9' {
			hasNumbers = true
		}
	}

	// If it has numbers but no letters, it's not a name
	if hasNumbers && !hasLetters {
		return false
	}

	// If it has letters, it could be a name
	if hasLetters {
		// Additional check: if it's a single word or two words, likely a name
		words := strings.Fields(message)
		if len(words) == 1 || len(words) == 2 {
			return true
		}
		// For longer messages, be more strict
		if len(words) <= 3 {
			return true
		}
	}

	return false
}

// extractNameFromMessage extracts a name from a message that contains name patterns
func extractNameFromMessage(message string) string {
	message = strings.TrimSpace(message)

	// Common name introduction patterns
	namePatterns := []string{
		"my name is",
		"i am",
		"i'm",
		"mera naam",
		"main",
		"name is",
		"i am called",
		"call me",
		"mujhe",
		"maine",
	}

	messageLower := strings.ToLower(message)

	// Check for name introduction patterns
	for _, pattern := range namePatterns {
		if strings.Contains(messageLower, pattern) {
			// Find the position of the pattern
			patternIndex := strings.Index(messageLower, pattern)
			if patternIndex != -1 {
				// Extract text after the pattern
				afterPattern := message[patternIndex+len(pattern):]
				afterPattern = strings.TrimSpace(afterPattern)

				// Split by common separators and take the first part
				separators := []string{",", ".", " and ", " aur ", " or ", " ya ", " hun", " hai", " kehte hain"}
				name := afterPattern
				for _, sep := range separators {
					if strings.Contains(strings.ToLower(name), sep) {
						parts := strings.Split(strings.ToLower(name), sep)
						if len(parts) > 0 {
							name = strings.TrimSpace(parts[0])
							break
						}
					}
				}

				// For "call me" pattern, take up to 2 words
				if pattern == "call me" {
					words := strings.Fields(name)
					if len(words) > 2 {
						name = strings.Join(words[:2], " ")
					}
				}

				// For "mujhe" pattern, take up to 2 words before "kehte hain"
				if pattern == "mujhe" {
					words := strings.Fields(name)
					if len(words) > 2 {
						name = strings.Join(words[:2], " ")
					}
				}

				// Validate if it looks like a name
				if isNameProvided(name) {
					return name
				}
			}
		}
	}

	// If no pattern found, check if the entire message is a name
	if isNameProvided(message) {
		return message
	}

	return ""
}

// isEmailProvided checks if the message contains an email
func isEmailProvided(message string) bool {
	emailRegex := `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`
	matched, _ := regexp.MatchString(emailRegex, message)
	return matched
}

// ===================
// IP-BASED USER NAME PERSISTENCE
// ===================

// storeUserNameByIP stores or updates user name by IP address
func storeUserNameByIP(ctx context.Context, collection *mongo.Collection, userIP, userName, userEmail string, clientID primitive.ObjectID) error {
	filter := bson.M{
		"user_ip":   userIP,
		"client_id": clientID,
	}

	update := bson.M{
		"$set": bson.M{
			"user_name": userName,
			"last_seen": time.Now(),
		},
		"$inc": bson.M{
			"count": 1,
		},
	}

	// Add email if provided
	if userEmail != "" {
		update["$set"].(bson.M)["user_email"] = userEmail
	}

	// Set first_seen only if this is a new record
	update["$setOnInsert"] = bson.M{
		"first_seen": time.Now(),
	}

	opts := options.Update().SetUpsert(true)
	_, err := collection.UpdateOne(ctx, filter, update, opts)
	return err
}

// getUserNameByIP retrieves user name by IP address
func getUserNameByIP(ctx context.Context, collection *mongo.Collection, userIP string, clientID primitive.ObjectID) (string, string, error) {
	filter := bson.M{
		"user_ip":   userIP,
		"client_id": clientID,
	}

	var userRecord models.UserNameByIP
	err := collection.FindOne(ctx, filter).Decode(&userRecord)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "", "", nil // No name found for this IP
		}
		return "", "", err
	}

	return userRecord.UserName, userRecord.UserEmail, nil
}

// calculateIntentScore calculates buying intent based on conversation history
func calculateIntentScore(history []models.Message, currentMessage string) int {
	score := 0

	// Keywords that indicate buying intent
	intentKeywords := map[string]int{
		"demo": 3, "demonstration": 3, "show": 2,
		"package": 2, "packages": 2, "plan": 2,
		"pricing": 2, "price": 2, "cost": 2, "charges": 2, "rate": 2,
		"minimum": 2, "smallest": 1,
		"quote": 3, "quotation": 3,
		"start": 2, "begin": 2, "get started": 3,
		"book": 3, "schedule": 2, "appointment": 2,
		"buy": 3, "purchase": 3, "order": 2,
	}

	// Check current message
	currentLower := strings.ToLower(currentMessage)
	for keyword, points := range intentKeywords {
		if strings.Contains(currentLower, keyword) {
			score += points
		}
	}

	// Check history
	for _, msg := range history {
		msgLower := strings.ToLower(msg.Message)
		for keyword, points := range intentKeywords {
			if strings.Contains(msgLower, keyword) {
				score += points
			}
		}
	}

	// Bonus for number of questions asked (shows engagement)
	if len(history) >= 4 {
		score += 2
	}
	if len(history) >= 6 {
		score += 1
	}

	return score
}

// getContextSpecificFollowUp generates a context-specific follow-up based on the question answered
func getContextSpecificFollowUp(currentMessage string, history []models.Message) string {
	currentLower := strings.ToLower(currentMessage)

	// Pricing/Charges related
	if strings.Contains(currentLower, "charg") || strings.Contains(currentLower, "price") || strings.Contains(currentLower, "cost") || strings.Contains(currentLower, "rate") {
		return "Would you like to see package details with discounts, or get a personalized quote?"
	}

	// Features/How it works
	if strings.Contains(currentLower, "how") || strings.Contains(currentLower, "work") || strings.Contains(currentLower, "process") {
		return "Would a quick 5-minute demo help, or do you have other questions?"
	}

	// Delivery related
	if strings.Contains(currentLower, "deliver") || strings.Contains(currentLower, "ratio") {
		return "Are you ready to discuss your campaign goals, or need more details?"
	}

	// Database related
	if strings.Contains(currentLower, "database") || strings.Contains(currentLower, "data") {
		return "What specific targeting criteria do you need? I can check if we have matching data."
	}

	// Messaging/Scale related
	if strings.Contains(currentLower, "message") || strings.Contains(currentLower, "send") || strings.Contains(currentLower, "number") {
		return "What scale are you planning for? This helps me suggest the best package."
	}

	// Demo related
	if strings.Contains(currentLower, "demo") || strings.Contains(currentLower, "sample") {
		return "Would you like me to schedule your demo, or do you have questions about the process?"
	}

	// Default - only use generic if truly no context
	return "Is there anything specific you'd like to know more about?"
}

// updateConversationState updates conversation state in the database
func updateConversationState(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, sessionID string, state map[string]interface{}) error {
	filter := bson.M{
		"client_id":       clientID,
		"conversation_id": sessionID,
		"is_embed_user":   true,
	}

	// Convert state keys to BSON field names
	bsonState := bson.M{}
	for key, value := range state {
		switch key {
		case "demo_scheduled":
			bsonState["demo_scheduled"] = value
		case "demo_time":
			bsonState["demo_time"] = value
		case "business_name":
			bsonState["business_name"] = value
		case "industry":
			bsonState["industry"] = value
		case "pricing_discussed":
			bsonState["pricing_discussed"] = value
		case "ready_to_schedule":
			bsonState["ready_to_schedule"] = value
		default:
			bsonState[key] = value
		}
	}

	update := bson.M{
		"$set": bsonState,
	}

	opts := options.Update().SetUpsert(false)
	result, err := collection.UpdateMany(ctx, filter, update, opts)
	if err != nil {
		return fmt.Errorf("failed to update conversation state: %w", err)
	}

	if result.MatchedCount == 0 {
		// No messages found - state will be updated when the next message is created
		// This is fine - the state fields will be set on the next message in the conversation
		fmt.Printf("Warning: No messages found to update conversation state for session %s. State will be applied to next message.\n", sessionID)
	}

	return nil
}

// ===================
// UTILITY FUNCTIONS
// ===================

// fixContactCollectionForExistingConversations fixes contact collection state for existing conversations
func fixContactCollectionForExistingConversations(ctx context.Context, collection *mongo.Collection) error {
	// Find conversations where AI said completion message but state wasn't updated
	filter := bson.M{
		"reply": bson.M{
			"$regex":   "Hamari team aapse jald hi contact karegi",
			"$options": "i",
		},
		"is_embed_user":            true,
		"contact_collection_phase": bson.M{"$ne": "completed"},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var messages []models.Message
	if err := cursor.All(ctx, &messages); err != nil {
		return err
	}

	for _, message := range messages {
		// Update the message to completed state
		update := bson.M{
			"$set": bson.M{
				"contact_collection_phase": "completed",
				"chat_disabled":            true,
			},
		}

		_, err := collection.UpdateOne(ctx, bson.M{"_id": message.ID}, update)
		if err != nil {
			fmt.Printf("Failed to update message %s: %v\n", message.ID.Hex(), err)
		} else {
			fmt.Printf("Updated message %s to completed state\n", message.ID.Hex())
		}
	}

	return nil
}

// handleFixContactCollection fixes contact collection state for existing conversations
func handleFixContactCollection(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		err := fixContactCollectionForExistingConversations(ctx, messagesCollection)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to fix contact collection state",
				"details": err.Error(),
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "Contact collection state fixed successfully",
		})
	}
}

// handleRealUsersChatHistory returns real users chat conversations (completed contact collection)
func handleRealUsersChatHistory(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Build filter for real users only (completed contact collection)
		filter := bson.M{
			"client_id":     clientObjID,
			"is_embed_user": true,
			"$or": []bson.M{
				// Option 1: Completed contact collection phase
				{
					"contact_collection_phase": "completed",
					"user_name":                bson.M{"$ne": ""},
					"user_email":               bson.M{"$ne": ""},
				},
				// Option 2: Has both name and email (fallback)
				{
					"user_name":  bson.M{"$ne": ""},
					"user_email": bson.M{"$ne": ""},
				},
			},
		}

		// Filter for real users (completed contact collection)

		// Add search filter if provided
		if search != "" {
			searchFilter := bson.M{
				"$or": []bson.M{
					{"message": bson.M{"$regex": search, "$options": "i"}},
					{"reply": bson.M{"$regex": search, "$options": "i"}},
					{"user_name": bson.M{"$regex": search, "$options": "i"}},
					{"user_email": bson.M{"$regex": search, "$options": "i"}},
					{"user_ip": bson.M{"$regex": search, "$options": "i"}},
					{"country": bson.M{"$regex": search, "$options": "i"}},
					{"city": bson.M{"$regex": search, "$options": "i"}},
				},
			}
			filter["$and"] = []bson.M{filter, searchFilter}
		}

		// Get total count
		total, err := messagesCollection.CountDocuments(ctx, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to count messages",
			})
			return
		}

		// Get conversations grouped by session_id
		pipeline := mongo.Pipeline{
			{{Key: "$match", Value: filter}},
			{{Key: "$sort", Value: bson.D{{Key: "timestamp", Value: -1}}}},
			{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$session_id"},
				{Key: "conversation_id", Value: bson.D{{Key: "$first", Value: "$conversation_id"}}},
				{Key: "first_message", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
				{Key: "last_message", Value: bson.D{{Key: "$last", Value: "$$ROOT"}}},
				{Key: "message_count", Value: bson.D{{Key: "$sum", Value: 1}}},
				{Key: "total_tokens", Value: bson.D{{Key: "$sum", Value: "$token_cost"}}},
				{Key: "user_ip", Value: bson.D{{Key: "$first", Value: "$user_ip"}}},
				{Key: "user_agent", Value: bson.D{{Key: "$first", Value: "$user_agent"}}},
				{Key: "country", Value: bson.D{{Key: "$first", Value: "$country"}}},
				{Key: "city", Value: bson.D{{Key: "$first", Value: "$city"}}},
				{Key: "referrer", Value: bson.D{{Key: "$first", Value: "$referrer"}}},
				{Key: "user_name", Value: bson.D{{Key: "$last", Value: "$user_name"}}},
				{Key: "user_email", Value: bson.D{{Key: "$last", Value: "$user_email"}}},
			}}},
			{{Key: "$sort", Value: bson.D{{Key: "last_message.timestamp", Value: -1}}}},
			{{Key: "$skip", Value: (page - 1) * limit}},
			{{Key: "$limit", Value: limit}},
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
				ID             string         `bson:"_id"`
				ConversationID string         `bson:"conversation_id"`
				FirstMessage   models.Message `bson:"first_message"`
				LastMessage    models.Message `bson:"last_message"`
				MessageCount   int            `bson:"message_count"`
				TotalTokens    int            `bson:"total_tokens"`
				UserIP         string         `bson:"user_ip"`
				UserAgent      string         `bson:"user_agent"`
				Country        string         `bson:"country"`
				City           string         `bson:"city"`
				Referrer       string         `bson:"referrer"`
				UserName       string         `bson:"user_name"`
				UserEmail      string         `bson:"user_email"`
			}

			if err := cursor.Decode(&result); err != nil {
				continue
			}

			conversations = append(conversations, gin.H{
				"session_id":      result.ID,
				"conversation_id": result.ConversationID,
				"first_message":   result.FirstMessage.Message,
				"last_message":    result.LastMessage.Message,
				"message_count":   result.MessageCount,
				"total_tokens":    result.TotalTokens,
				"user_ip":         result.UserIP,
				"user_agent":      result.UserAgent,
				"country":         result.Country,
				"city":            result.City,
				"referrer":        result.Referrer,
				"user_name":       result.UserName,
				"user_email":      result.UserEmail,
				"started_at":      result.FirstMessage.Timestamp,
				"last_activity":   result.LastMessage.Timestamp,
			})
		}

		totalPages := (total + int64(limit) - 1) / int64(limit)

		c.JSON(http.StatusOK, gin.H{
			"conversations": conversations,
			"pagination": gin.H{
				"page":        page,
				"limit":       limit,
				"total":       total,
				"total_pages": totalPages,
			},
		})
	}
}

// handleDebugContactState debug endpoint to check contact collection state
func handleDebugContactState(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Get all messages for this client
		filter := bson.M{
			"client_id":     clientObjID,
			"is_embed_user": true,
		}

		cursor, err := messagesCollection.Find(ctx, filter, options.Find().SetSort(bson.M{"timestamp": -1}).SetLimit(10))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to fetch messages",
			})
			return
		}
		defer cursor.Close(ctx)

		var messages []models.Message
		if err := cursor.All(ctx, &messages); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to decode messages",
			})
			return
		}

		// Count by phase
		phaseCounts := make(map[string]int)
		hasNameEmail := 0
		completedPhase := 0

		for _, msg := range messages {
			phase := msg.ContactCollectionPhase
			if phase == "" {
				phase = "none"
			}
			phaseCounts[phase]++

			if msg.UserName != "" && msg.UserEmail != "" {
				hasNameEmail++
			}
			if phase == "completed" {
				completedPhase++
			}
		}

		// Get recent messages (max 5)
		recentCount := 5
		if len(messages) < recentCount {
			recentCount = len(messages)
		}
		recentMessages := messages[:recentCount]

		// Get detailed info about recent messages
		var detailedMessages []gin.H
		for _, msg := range recentMessages {
			detailedMessages = append(detailedMessages, gin.H{
				"message":       msg.Message,
				"reply":         msg.Reply,
				"user_name":     msg.UserName,
				"user_email":    msg.UserEmail,
				"contact_phase": msg.ContactCollectionPhase,
				"chat_disabled": msg.ChatDisabled,
				"timestamp":     msg.Timestamp,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"total_messages":  len(messages),
			"phase_counts":    phaseCounts,
			"has_name_email":  hasNameEmail,
			"completed_phase": completedPhase,
			"recent_messages": detailedMessages,
		})
	}
}

// handleExtractUserInfo extracts names and emails from existing conversations
func handleExtractUserInfo(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		// Get all conversations for this client
		filter := bson.M{
			"client_id":     clientObjID,
			"is_embed_user": true,
		}

		cursor, err := messagesCollection.Find(ctx, filter, options.Find().SetSort(bson.M{"timestamp": 1}))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to fetch messages",
			})
			return
		}
		defer cursor.Close(ctx)

		var messages []models.Message
		if err := cursor.All(ctx, &messages); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to decode messages",
			})
			return
		}

		// Group messages by session_id
		sessions := make(map[string][]models.Message)
		for _, msg := range messages {
			sessions[msg.SessionID] = append(sessions[msg.SessionID], msg)
		}

		updatedCount := 0
		for sessionID, sessionMessages := range sessions {
			// Extract name and email from the conversation
			var userName, userEmail string

			// Look for name and email in the messages
			for _, msg := range sessionMessages {
				// Check if this message looks like a name
				if isNameProvided(msg.Message) && userName == "" {
					userName = strings.TrimSpace(msg.Message)
				}
				// Check if this message contains an email
				if isEmailProvided(msg.Message) && userEmail == "" {
					userEmail = strings.TrimSpace(msg.Message)
				}
			}

			// If we found email (with or without name), update the conversation
			if userEmail != "" {
				// If no name found, use email prefix as name
				if userName == "" {
					emailParts := strings.Split(userEmail, "@")
					if len(emailParts) > 0 {
						userName = emailParts[0]
					}
				}

				// Update all messages in this session
				updateFilter := bson.M{
					"client_id":     clientObjID,
					"session_id":    sessionID,
					"is_embed_user": true,
				}

				update := bson.M{
					"$set": bson.M{
						"user_name":                userName,
						"user_email":               userEmail,
						"contact_collection_phase": "completed",
						"chat_disabled":            true,
					},
				}

				result, err := messagesCollection.UpdateMany(ctx, updateFilter, update)
				if err != nil {
					fmt.Printf("Failed to update session %s: %v\n", sessionID, err)
					continue
				}

				updatedCount += int(result.ModifiedCount)
				fmt.Printf("Updated session %s: userName=%s, userEmail=%s, modified=%d\n",
					sessionID, userName, userEmail, result.ModifiedCount)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"message":          "User information extraction completed",
			"updated_messages": updatedCount,
			"total_sessions":   len(sessions),
		})
	}
}

// handleTestNameDetection tests the name detection function
func handleTestNameDetection() gin.HandlerFunc {
	return func(c *gin.Context) {
		testMessages := []string{
			"rahul",
			"John Doe",
			"aliz@gmail.com",
			"foofoo@gmail.com",
			"How can I contact support?",
			"thank you",
			"yes",
			"ok",
			"hello",
			"hi there",
			"rahul kumar",
			"123456",
			"test@example.com",
		}

		results := make(map[string]bool)
		for _, msg := range testMessages {
			results[msg] = isNameProvided(msg)
		}

		c.JSON(http.StatusOK, gin.H{
			"test_results": results,
		})
	}
}

// handleTestNameExtraction tests the name extraction function
func handleTestNameExtraction() gin.HandlerFunc {
	return func(c *gin.Context) {
		testMessages := []string{
			"my name is sabit ali",
			"i am John Doe",
			"i'm Sarah",
			"mera naam Ahmed hai",
			"main Rajesh hun",
			"name is Michael",
			"call me Priya",
			"mujhe Suresh kehte hain",
			"my name is David, and I need help",
			"i am Maria. Can you help me?",
			"i'm Alex and I have a question",
			"mera naam Vikram hai aur main yahan hun",
			"main Anjali hun, please help",
			"name is Robert, I need assistance",
			"call me Lisa, I have a problem",
			"mujhe Arjun kehte hain, help me",
			"John",
			"Sarah Smith",
			"test@example.com",
			"hello, how are you?",
			"help me please",
		}

		results := make(map[string]string)
		for _, msg := range testMessages {
			extractedName := extractNameFromMessage(msg)
			results[msg] = extractedName
		}

		c.JSON(http.StatusOK, gin.H{
			"extraction_results": results,
		})
	}
}

// handleUpdateMessageNames updates existing messages with real names
func handleUpdateMessageNames(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		// Get all messages for this client that have user names
		filter := bson.M{
			"client_id":     clientObjID,
			"is_embed_user": true,
			"user_name":     bson.M{"$ne": ""},
		}

		cursor, err := messagesCollection.Find(ctx, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to fetch messages",
			})
			return
		}
		defer cursor.Close(ctx)

		var messages []models.Message
		if err := cursor.All(ctx, &messages); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to decode messages",
			})
			return
		}

		updatedCount := 0
		for _, msg := range messages {
			// Update the from_name field with the real name
			update := bson.M{
				"$set": bson.M{
					"from_name": msg.UserName,
				},
			}

			result, err := messagesCollection.UpdateOne(ctx, bson.M{"_id": msg.ID}, update)
			if err != nil {
				fmt.Printf("Failed to update message %s: %v\n", msg.ID.Hex(), err)
				continue
			}

			if result.ModifiedCount > 0 {
				updatedCount++
				fmt.Printf("Updated message %s: from_name = %s\n", msg.ID.Hex(), msg.UserName)
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"message":          "Message names updated successfully",
			"updated_messages": updatedCount,
			"total_messages":   len(messages),
		})
	}
}

// ===================
// HELPER FUNCTIONS
// ===================

// getClientConfig retrieves client configuration from database
func getClientConfig(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID) (*models.Client, error) {
	var clientDoc models.Client
	err := collection.FindOne(ctx, bson.M{"_id": clientID}).Decode(&clientDoc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("client_not_found")
		}
		return nil, fmt.Errorf("database_error")
	}
	return &clientDoc, nil
}

// handleClientError handles client-related errors
func handleClientError(c *gin.Context, err error) {
	switch err.Error() {
	case "client_not_found":
		c.JSON(http.StatusNotFound, gin.H{
			"error_code": "client_not_found",
			"message":    "Client not found",
		})
	case "database_error":
		c.JSON(http.StatusInternalServerError, gin.H{
			"error_code": "database_error",
			"message":    "Database error occurred",
		})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{
			"error_code": "internal_error",
			"message":    "An internal error occurred",
		})
	}
}

// persistMessage saves the conversation to database and returns the message ID
func persistMessage(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, req ChatRequest, response string, tokenCost int, r *http.Request) (primitive.ObjectID, error) {
	// Extract user information from request
	userIP := utils.GetClientIP(r)
	userAgent := utils.GetUserAgent(r)
	referrer := utils.GetReferrer(r)

	// Get comprehensive geolocation data
	geoData := utils.GetGeolocationData(userIP)
	ipType := utils.GetIPType(geoData)

	// ✅ NEW: First check if we have a stored name for this IP address
	var userName, userEmail string
	storedName, storedEmail, err := getUserNameByIP(ctx, collection, userIP, clientID)
	if err != nil {
		fmt.Printf("Warning: Failed to get stored name by IP: %v\n", err)
	} else if storedName != "" {
		userName = storedName
		userEmail = storedEmail
		fmt.Printf("DEBUG: Found stored name for IP %s: '%s'\n", userIP, userName)
	}

	// Check if we have user name from contact collection (if no stored name found)
	if userName == "" {
		phase, _, err := getContactCollectionState(ctx, collection, clientID, req.SessionID)
		if err != nil {
			fmt.Printf("Warning: Failed to get contact collection state: %v\n", err)
			phase = "none"
		}

		// Get the latest user name if available
		if phase != "none" {
			filter := bson.M{
				"client_id":       clientID,
				"conversation_id": req.SessionID,
				"is_embed_user":   true,
			}
			opts := options.FindOne().SetSort(bson.M{"timestamp": -1})
			var latestMessage models.Message
			err := collection.FindOne(ctx, filter, opts).Decode(&latestMessage)
			if err == nil && latestMessage.UserName != "" {
				userName = latestMessage.UserName
			}
		}

		// ✅ NEW: Try to extract name from current message if no name found yet
		if userName == "" {
			extractedName := extractNameFromMessage(req.Message)
			if extractedName != "" {
				userName = extractedName
				fmt.Printf("DEBUG: Extracted name from message: '%s'\n", userName)
			}
		}
	}

	// ✅ NEW: Store the name by IP for future conversations
	if userName != "" {
		go func() {
			storeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err := storeUserNameByIP(storeCtx, collection, userIP, userName, userEmail, clientID)
			if err != nil {
				fmt.Printf("Warning: Failed to store name by IP: %v\n", err)
			} else {
				fmt.Printf("Stored name '%s' for IP %s\n", userName, userIP)
			}
		}()
	}

	// Determine the display name
	displayName := "Embed User"
	if userName != "" {
		displayName = userName
	}

	message := models.Message{
		FromUserID:     primitive.NilObjectID, // public user
		FromName:       displayName,           // Use real name if available
		Message:        req.Message,
		Reply:          response,
		Timestamp:      time.Now(),
		ClientID:       clientID,
		ConversationID: req.SessionID,
		TokenCost:      tokenCost,
		UserIP:         userIP,
		UserAgent:      userAgent,
		Referrer:       referrer,
		SessionID:      req.SessionID,
		IsEmbedUser:    true,
		UserName:       userName, // Include collected/extracted user name

		// Enhanced geolocation data
		Country:      geoData.Country,
		CountryCode:  geoData.CountryCode,
		Region:       geoData.Region,
		RegionName:   geoData.RegionName,
		City:         geoData.City,
		Latitude:     geoData.Latitude,
		Longitude:    geoData.Longitude,
		Timezone:     geoData.Timezone,
		ISP:          geoData.ISP,
		Organization: geoData.Organization,
		IPType:       string(ipType),
	}

	result, err := collection.InsertOne(ctx, message)
	if err != nil {
		return primitive.NilObjectID, err
	}
	return result.InsertedID.(primitive.ObjectID), nil
}

// updateTokenUsage atomically updates client token usage
func updateTokenUsage(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, tokenLimit, tokenCost int) error {
	updateResult, err := collection.UpdateOne(ctx,
		bson.M{
			"_id":        clientID,
			"token_used": bson.M{"$lte": tokenLimit - tokenCost},
		},
		bson.M{
			"$inc": bson.M{"token_used": tokenCost},
			"$set": bson.M{"updated_at": time.Now()},
		},
	)

	if err != nil {
		return err
	}

	if updateResult.MatchedCount == 0 {
		return fmt.Errorf("token update failed or insufficient tokens")
	}

	return nil
}

// handleEmbedChatHistory returns embed chat conversations with IP tracking data
func handleEmbedChatHistory(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Build filter for embed users only
		filter := bson.M{
			"client_id":     clientObjID,
			"is_embed_user": true,
		}

		// Add search filter if provided
		if search != "" {
			filter["$or"] = []bson.M{
				{"message": bson.M{"$regex": search, "$options": "i"}},
				{"reply": bson.M{"$regex": search, "$options": "i"}},
				{"user_ip": bson.M{"$regex": search, "$options": "i"}},
				{"country": bson.M{"$regex": search, "$options": "i"}},
				{"city": bson.M{"$regex": search, "$options": "i"}},
			}
		}

		// Get total count
		total, err := messagesCollection.CountDocuments(ctx, filter)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to count messages",
			})
			return
		}

		// Get conversations grouped by session_id
		pipeline := mongo.Pipeline{
			{{Key: "$match", Value: filter}},
			{{Key: "$sort", Value: bson.D{{Key: "timestamp", Value: -1}}}},
			{{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$session_id"},
				{Key: "conversation_id", Value: bson.D{{Key: "$first", Value: "$conversation_id"}}},
				{Key: "first_message", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
				{Key: "last_message", Value: bson.D{{Key: "$last", Value: "$$ROOT"}}},
				{Key: "message_count", Value: bson.D{{Key: "$sum", Value: 1}}},
				{Key: "total_tokens", Value: bson.D{{Key: "$sum", Value: "$token_cost"}}},
				{Key: "user_ip", Value: bson.D{{Key: "$first", Value: "$user_ip"}}},
				{Key: "user_agent", Value: bson.D{{Key: "$first", Value: "$user_agent"}}},
				{Key: "country", Value: bson.D{{Key: "$first", Value: "$country"}}},
				{Key: "city", Value: bson.D{{Key: "$first", Value: "$city"}}},
				{Key: "referrer", Value: bson.D{{Key: "$first", Value: "$referrer"}}},
				{Key: "user_name", Value: bson.D{{Key: "$last", Value: "$user_name"}}}, // Get the latest user name
			}}},
			{{Key: "$sort", Value: bson.D{{Key: "last_message.timestamp", Value: -1}}}},
			{{Key: "$skip", Value: (page - 1) * limit}},
			{{Key: "$limit", Value: limit}},
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
				ID             string         `bson:"_id"`
				ConversationID string         `bson:"conversation_id"`
				FirstMessage   models.Message `bson:"first_message"`
				LastMessage    models.Message `bson:"last_message"`
				MessageCount   int            `bson:"message_count"`
				TotalTokens    int            `bson:"total_tokens"`
				UserIP         string         `bson:"user_ip"`
				UserAgent      string         `bson:"user_agent"`
				Country        string         `bson:"country"`
				City           string         `bson:"city"`
				Referrer       string         `bson:"referrer"`
				UserName       string         `bson:"user_name"`
			}

			if err := cursor.Decode(&result); err != nil {
				continue
			}

			conversations = append(conversations, gin.H{
				"session_id":      result.ID,
				"conversation_id": result.ConversationID,
				"first_message":   result.FirstMessage.Message,
				"last_message":    result.LastMessage.Message,
				"message_count":   result.MessageCount,
				"total_tokens":    result.TotalTokens,
				"user_ip":         result.UserIP,
				"user_agent":      result.UserAgent,
				"country":         result.Country,
				"city":            result.City,
				"referrer":        result.Referrer,
				"user_name":       result.UserName,
				"started_at":      result.FirstMessage.Timestamp,
				"last_activity":   result.LastMessage.Timestamp,
			})
		}

		totalPages := (total + int64(limit) - 1) / int64(limit)

		c.JSON(http.StatusOK, gin.H{
			"conversations": conversations,
			"pagination": gin.H{
				"page":        page,
				"limit":       limit,
				"total":       total,
				"total_pages": totalPages,
			},
		})
	}
}

// handleEmbedConversationMessages returns messages for a specific embed conversation
func handleEmbedConversationMessages(messagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		conversationID := c.Param("id")
		if conversationID == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_conversation_id",
				"message":    "Conversation ID required",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()

		// Find messages for this conversation
		filter := bson.M{
			"client_id":       clientObjID,
			"conversation_id": conversationID,
			"is_embed_user":   true,
		}

		cursor, err := messagesCollection.Find(
			ctx,
			filter,
			options.Find().SetSort(bson.M{"timestamp": 1}), // Sort by timestamp ascending
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

		c.JSON(http.StatusOK, gin.H{
			"conversation_id": conversationID,
			"messages":        messages,
			"total_tokens":    totalTokens,
			"message_count":   len(messages),
			"created_at":      createdAt,
			"updated_at":      updatedAt,
		})
	}
}

// configureGeminiModel sets up Gemini model with FREE TIER settings
func configureGeminiModel(client *genai.Client) *genai.GenerativeModel {
	// 🆓 FREE TIER MODEL (with version)
	model := client.GenerativeModel("gemini-2.0-flash")

	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockMediumAndAbove,
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockMediumAndAbove,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockMediumAndAbove,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockMediumAndAbove,
		},
	}

	model.GenerationConfig = genai.GenerationConfig{
		Temperature:     float32Ptr(0.7),
		TopP:            float32Ptr(0.8),
		TopK:            int32Ptr(40),
		MaxOutputTokens: int32Ptr(2000),
	}

	return model
}

// extractResponseText extracts text from Gemini response
func extractResponseText(resp *genai.GenerateContentResponse) (string, error) {
	if len(resp.Candidates) == 0 || resp.Candidates[0] == nil || resp.Candidates[0].Content == nil {
		return "I apologize, but I couldn't generate a proper response. Please try again.", nil
	}

	var reply strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if txt, ok := part.(genai.Text); ok {
			reply.WriteString(string(txt))
		}
	}

	replyText := strings.TrimSpace(reply.String())
	if replyText == "" {
		replyText = "I apologize, but I couldn't generate a proper response. Please try again."
	}

	return replyText, nil
}

// calculateAccurateTokens uses the Gemini CountTokens API
func calculateAccurateTokens(ctx context.Context, model *genai.GenerativeModel, parts ...genai.Part) (int, error) {
	resp, err := model.CountTokens(ctx, parts...)
	if err != nil {
		return 0, fmt.Errorf("count tokens failed: %w", err)
	}
	return int(resp.TotalTokens), nil
}

// parsePeriod parses period string into duration
func parsePeriod(period string) time.Duration {
	switch period {
	case "7d":
		return 7 * 24 * time.Hour
	case "30d", "month":
		return 30 * 24 * time.Hour
	case "90d":
		return 90 * 24 * time.Hour
	case "1y", "year":
		return 365 * 24 * time.Hour
	default:
		// try to parse like "15d"
		if strings.HasSuffix(period, "d") {
			if n, err := strconv.Atoi(strings.TrimSuffix(period, "d")); err == nil && n > 0 {
				return time.Duration(n) * 24 * time.Hour
			}
		}
		return 30 * 24 * time.Hour // default
	}
}

// generateAnalytics generates comprehensive analytics data
func generateAnalytics(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, start, end time.Time, period string) (gin.H, error) {
	match := bson.M{
		"client_id": clientID,
		"timestamp": bson.M{"$gte": start, "$lte": end},
	}

	// Get total messages
	totalMessages, err := collection.CountDocuments(ctx, match)
	if err != nil {
		return nil, fmt.Errorf("failed to count messages: %w", err)
	}

	// Get total tokens
	tokPipe := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.M{
			"_id": nil,
			"tokens": bson.M{"$sum": bson.M{
				"$toInt": bson.M{"$ifNull": bson.A{"$token_cost", 0}},
			}},
		}}},
	}

	var totalTokens int64
	if cur, err := collection.Aggregate(ctx, tokPipe); err == nil {
		var r []struct {
			Tokens int64 `bson:"tokens"`
		}
		if err := cur.All(ctx, &r); err == nil && len(r) > 0 {
			totalTokens = r[0].Tokens
		}
	}

	// Get active users
	var activeUsers int
	if vals, err := collection.Distinct(ctx, "from_user_id", match); err == nil {
		activeUsers = len(vals)
	}

	// Get conversations
	var convIDs []interface{}
	if vals, err := collection.Distinct(ctx, "conversation_id", match); err == nil {
		convIDs = vals
	}
	totalConversations := len(convIDs)

	// Calculate averages
	avgMessagesPerConversation := 0.0
	if totalConversations > 0 {
		avgMessagesPerConversation = float64(totalMessages) / float64(totalConversations)
	}

	// Get time series data
	timeSeries, err := getTimeSeriesData(ctx, collection, match)
	if err != nil {
		return nil, fmt.Errorf("failed to get time series: %w", err)
	}

	// Get previous period data for comparison
	prevData, err := getPreviousPeriodData(ctx, collection, clientID, start, end)
	if err != nil {
		return nil, fmt.Errorf("failed to get previous period data: %w", err)
	}

	return gin.H{
		"client_id":                     clientID.Hex(),
		"period":                        period,
		"start_date":                    start.Format(time.RFC3339),
		"end_date":                      end.Format(time.RFC3339),
		"total_messages":                int(totalMessages),
		"total_tokens":                  int(totalTokens),
		"active_users":                  activeUsers,
		"total_conversations":           totalConversations,
		"avg_messages_per_conversation": avgMessagesPerConversation,
		"avg_conversation_length":       avgMessagesPerConversation,
		"avg_response_time":             0, // not tracked yet
		"time_series":                   timeSeries,
		"usage_by_period":               timeSeries, // alias
		"previous_period":               prevData,
	}, nil
}

// getTimeSeriesData retrieves time series analytics data
func getTimeSeriesData(ctx context.Context, collection *mongo.Collection, match bson.M) ([]gin.H, error) {
	seriesPipe := mongo.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"day": bson.M{"$dateToString": bson.M{
					"format":   "%Y-%m-%d",
					"date":     "$timestamp",
					"timezone": "UTC",
				}},
			},
			"total_messages": bson.M{"$sum": 1},
			"total_tokens": bson.M{"$sum": bson.M{
				"$toInt": bson.M{"$ifNull": bson.A{"$token_cost", 0}},
			}},
			"users": bson.M{"$addToSet": "$from_user_id"},
			"convs": bson.M{"$addToSet": "$conversation_id"},
		}}},
		{{Key: "$project", Value: bson.M{
			"date":                "_id.day",
			"total_messages":      1,
			"total_tokens":        1,
			"active_users":        bson.M{"$size": "$users"},
			"total_conversations": bson.M{"$size": "$convs"},
			"_id":                 0,
		}}},
		{{Key: "$sort", Value: bson.M{"date": 1}}},
	}

	cur, err := collection.Aggregate(ctx, seriesPipe)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var timeSeries []gin.H
	for cur.Next(ctx) {
		var doc bson.M
		if err := cur.Decode(&doc); err != nil {
			continue
		}
		timeSeries = append(timeSeries, gin.H{
			"period":              "day",
			"date":                doc["date"],
			"total_messages":      doc["total_messages"],
			"total_tokens":        doc["total_tokens"],
			"active_users":        doc["active_users"],
			"total_conversations": doc["total_conversations"],
		})
	}

	return timeSeries, nil
}

// getPreviousPeriodData retrieves data from the previous period for comparison
func getPreviousPeriodData(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, start, end time.Time) (gin.H, error) {
	dur := end.Sub(start)
	prevStart := start.Add(-dur)
	prevEnd := start.Add(-time.Nanosecond)

	prevMatch := bson.M{
		"client_id": clientID,
		"timestamp": bson.M{"$gte": prevStart, "$lte": prevEnd},
	}

	prevMsgs, _ := collection.CountDocuments(ctx, prevMatch)

	// Get previous tokens
	var prevTokens int64
	tokPipe := mongo.Pipeline{
		{{Key: "$match", Value: prevMatch}},
		{{Key: "$group", Value: bson.M{
			"_id": nil,
			"tokens": bson.M{"$sum": bson.M{
				"$toInt": bson.M{"$ifNull": bson.A{"$token_cost", 0}},
			}},
		}}},
	}

	if cur, err := collection.Aggregate(ctx, tokPipe); err == nil {
		var r []struct {
			Tokens int64 `bson:"tokens"`
		}
		if err := cur.All(ctx, &r); err == nil && len(r) > 0 {
			prevTokens = r[0].Tokens
		}
	}

	// Get previous active users
	var prevUsers int
	if vals, err := collection.Distinct(ctx, "from_user_id", prevMatch); err == nil {
		prevUsers = len(vals)
	}

	return gin.H{
		"total_messages": int(prevMsgs),
		"total_tokens":   int(prevTokens),
		"active_users":   prevUsers,
	}, nil
}

// retrievePDFContext retrieves relevant PDF chunks for the given query
func retrievePDFContext(ctx context.Context, cfg *config.Config, pdfsCollection *mongo.Collection, clientID primitive.ObjectID, query string, maxChunks int) ([]models.ContentChunk, error) {
	// Prefer Atlas Vector/Text Search when enabled; fall back to keyword scoring
	if cfg != nil && (cfg.VectorSearchEnabled || cfg.AtlasTextSearchEnabled) {
		if chunks, err := searchRelevantChunks(ctx, pdfsCollection.Database(), clientID, query, maxChunks, cfg); err == nil && len(chunks) > 0 {
			return chunks, nil
		}
	}
	// Check if any PDFs exist for this client
	_, err := pdfsCollection.CountDocuments(ctx, bson.M{"client_id": clientID})
	if err != nil {
		// Log error but continue
		_ = err
	}

	cursor, err := pdfsCollection.Find(ctx, bson.M{"client_id": clientID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var pdfs []models.PDF
	if err := cursor.All(ctx, &pdfs); err != nil {
		return nil, err
	}

	if len(pdfs) == 0 {
		return nil, nil
	}

	queryLower := strings.ToLower(query)

	// ✅ BASIC COMPANY QUESTIONS - Return ALL content (but not for simple greetings)
	basicQuestions := []string{
		"company name", "what is", "who are", "about", "services", "contact",
		"phone", "email", "address", "location", "tell me", "show me",
		"information", "details", "business",
	}

	isBasicQuestion := false
	for _, basic := range basicQuestions {
		if strings.Contains(queryLower, basic) {
			isBasicQuestion = true
			break
		}
	}

	var allChunks []models.ContentChunk
	totalChunks := 0
	for _, pdf := range pdfs {
		allChunks = append(allChunks, pdf.ContentChunks...)
		totalChunks += len(pdf.ContentChunks)
		fmt.Printf("Debug: PDF %s has %d chunks\n", pdf.Filename, len(pdf.ContentChunks))
	}

	fmt.Printf("Debug: Total chunks available: %d\n", totalChunks)

	// ✅ For greetings, return minimal chunks (first 3 only for introduction)
	greetings := []string{"hello", "hi", "hey", "good morning", "good afternoon", "good evening"}
	queryLowerLower := strings.ToLower(queryLower)
	for _, g := range greetings {
		if strings.Contains(queryLowerLower, g) {
			fmt.Printf("Debug: Detected greeting: %s\n", g)
			// Return only first 3 chunks for greeting (company intro)
			if len(allChunks) > 0 {
				if len(allChunks) < 3 {
					return allChunks[:], nil
				}
				return allChunks[:3], nil
			}
			return []models.ContentChunk{}, nil
		}
	}

	// ✅ If basic question or no specific keywords, return LIMITED chunks (not all)
	if isBasicQuestion || len(allChunks) <= maxChunks {
		// Sort by order to maintain document structure
		sort.Slice(allChunks, func(i, j int) bool {
			return allChunks[i].Order < allChunks[j].Order
		})

		if len(allChunks) <= maxChunks {
			fmt.Printf("Debug: Returning all %d chunks for basic question\n", len(allChunks))
			return allChunks, nil
		}
		fmt.Printf("Debug: Returning first %d chunks for basic question\n", maxChunks)
		return allChunks[:maxChunks], nil
	}

	// ✅ ADVANCED KEYWORD SEARCH for specific questions
	queryWords := strings.Fields(queryLower)

	type scoredChunk struct {
		chunk models.ContentChunk
		score int
	}

	var scored []scoredChunk

	for _, chunk := range allChunks {
		chunkLower := strings.ToLower(chunk.Text)
		score := 0

		// Enhanced scoring system
		for _, word := range queryWords {
			if len(word) > 2 {
				// Higher score for exact word matches
				if strings.Contains(chunkLower, word) {
					score += strings.Count(chunkLower, word) * 2
				}

				// Partial scoring for similar words
				if strings.Contains(chunkLower, word[:len(word)-1]) {
					score += 1
				}
			}
		}

		// Always include chunks with any score, or if no scored chunks found
		scored = append(scored, scoredChunk{chunk: chunk, score: score})
	}

	// Sort by score (descending) then by order (ascending)
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].chunk.Order < scored[j].chunk.Order
		}
		return scored[i].score > scored[j].score
	})

	// ✅ FALLBACK: If no good matches, return first chunks (company intro)
	hasGoodMatches := false
	for _, s := range scored {
		if s.score > 0 {
			hasGoodMatches = true
			break
		}
	}

	var relevantChunks []models.ContentChunk
	limit := maxChunks
	if len(scored) < limit {
		limit = len(scored)
	}

	if !hasGoodMatches {
		// Return first few chunks which usually contain company intro
		fmt.Printf("Debug: No good keyword matches, returning first %d chunks\n", limit)
		for i := 0; i < limit && i < len(scored); i++ {
			relevantChunks = append(relevantChunks, scored[i].chunk)
		}
	} else {
		// Return best matching chunks
		goodMatches := 0
		for i := 0; i < limit && i < len(scored) && scored[i].score > 0; i++ {
			relevantChunks = append(relevantChunks, scored[i].chunk)
			goodMatches++
		}

		fmt.Printf("Debug: Found %d good keyword matches\n", goodMatches)

		// If we don't have enough good matches, add some general chunks
		if len(relevantChunks) < maxChunks {
			needed := maxChunks - len(relevantChunks)
			for i := len(relevantChunks); i < len(scored) && needed > 0; i++ {
				relevantChunks = append(relevantChunks, scored[i].chunk)
				needed--
			}
		}
	}

	fmt.Printf("Debug: Returning %d relevant chunks\n", len(relevantChunks))
	return relevantChunks, nil
}

// searchRelevantChunks uses Atlas Vector Search ($vectorSearch) or Atlas Text Search ($search)
// against the denormalized 'pdf_chunks' collection.
func searchRelevantChunks(ctx context.Context, db *mongo.Database, clientID primitive.ObjectID, query string, limit int, cfg *config.Config) ([]models.ContentChunk, error) {
	col := db.Collection("pdf_chunks")

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.M{"client_id": clientID}}},
	}

	useVector := cfg.VectorSearchEnabled
	var vec []float32
	var err error
	if useVector {
		vec, err = ai.GenerateEmbedding(ctx, cfg, query)
		if err != nil {
			useVector = false
		}
	}

	if useVector {
		// Using vector search for retrieval
		pipeline = append(pipeline,
			bson.D{{Key: "$vectorSearch", Value: bson.M{
				"index":         cfg.VectorIndexName,
				"path":          "vector",
				"queryVector":   vec,
				"numCandidates": 200,
				"limit":         limit,
			}}},
			bson.D{{Key: "$project", Value: bson.M{
				"text": 1, "order": 1, "chunk_id": 1, "score": bson.M{"$meta": "vectorSearchScore"},
			}}},
		)
	} else if cfg.AtlasTextSearchEnabled {
		// Using text search for retrieval
		pipeline = append(pipeline,
			bson.D{{Key: "$search", Value: bson.M{
				"index": cfg.SearchIndexName,
				"text": bson.M{
					"query": query,
					"path":  []string{"text", "keywords"},
				},
			}}},
			bson.D{{Key: "$limit", Value: limit}},
			bson.D{{Key: "$project", Value: bson.M{"text": 1, "order": 1, "chunk_id": 1}}},
		)
	} else {
		// Using fallback keyword search
		return []models.ContentChunk{}, nil
	}

	cur, err := col.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	results := []models.ContentChunk{}
	for cur.Next(ctx) {
		var r struct {
			Text    string `bson:"text"`
			Order   int    `bson:"order"`
			ChunkID string `bson:"chunk_id"`
		}
		if err := cur.Decode(&r); err != nil {
			continue
		}
		results = append(results, models.ContentChunk{
			ChunkID: r.ChunkID,
			Text:    r.Text,
			Order:   r.Order,
		})
	}
	return results, nil
}

// retrieveCrawledContext retrieves relevant crawled page content for the given query
func retrieveCrawledContext(ctx context.Context, crawlsCollection *mongo.Collection, clientID primitive.ObjectID, query string, maxChunks int) ([]models.ContentChunk, error) {
	// Get only completed crawl jobs for this client
	count, err := crawlsCollection.CountDocuments(ctx, bson.M{
		"client_id": clientID,
		"status":    models.CrawlStatusCompleted,
	})
	if err != nil {
		fmt.Printf("Debug: Error counting crawls: %v\n", err)
	} else {
		fmt.Printf("Debug: Found %d completed crawls for client %s\n", count, clientID.Hex())
	}

	cursor, err := crawlsCollection.Find(ctx, bson.M{
		"client_id": clientID,
		"status":    models.CrawlStatusCompleted,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var crawlJobs []models.CrawlJob
	if err := cursor.All(ctx, &crawlJobs); err != nil {
		return nil, err
	}

	if len(crawlJobs) == 0 {
		fmt.Printf("Debug: No completed crawls found for client %s\n", clientID.Hex())
		return nil, nil
	}

	fmt.Printf("Debug: Processing %d completed crawls with query: %s\n", len(crawlJobs), query)

	queryLower := strings.ToLower(query)

	// Collect all crawled pages content
	var allCrawledPages []models.CrawledPage
	for _, job := range crawlJobs {
		allCrawledPages = append(allCrawledPages, job.CrawledPages...)
	}

	if len(allCrawledPages) == 0 {
		fmt.Printf("Debug: No crawled pages found in completed crawls\n")
		return nil, nil
	}

	fmt.Printf("Debug: Total crawled pages available: %d\n", len(allCrawledPages))

	// Convert crawled pages to content chunks for scoring
	var allChunks []models.ContentChunk
	for i, page := range allCrawledPages {
		// Convert crawled page content to chunks (similar to PDF chunking)
		// Split long content into smaller chunks if needed
		content := strings.TrimSpace(page.Content)
		if len(content) == 0 {
			continue
		}

		// For crawled pages, create chunks based on word count (max ~500 words per chunk)
		words := strings.Fields(content)
		chunkSize := 500
		if len(words) <= chunkSize {
			// Single chunk if content is small
			allChunks = append(allChunks, models.ContentChunk{
				Text:  fmt.Sprintf("%s\n\nSource: %s", content, page.URL),
				Order: i,
			})
		} else {
			// Split into multiple chunks
			for start := 0; start < len(words); start += chunkSize {
				end := start + chunkSize
				if end > len(words) {
					end = len(words)
				}
				chunkText := strings.Join(words[start:end], " ")
				allChunks = append(allChunks, models.ContentChunk{
					Text:  fmt.Sprintf("%s\n\nSource: %s", chunkText, page.URL),
					Order: i*1000 + start/chunkSize, // Ensure unique ordering
				})
			}
		}
	}

	fmt.Printf("Debug: Created %d chunks from crawled pages\n", len(allChunks))

	// Apply same relevance scoring as PDF chunks
	// ✅ BASIC COMPANY QUESTIONS - Return ALL content (but not for simple greetings)
	basicQuestions := []string{
		"company name", "what is", "who are", "about", "services", "contact",
		"phone", "email", "address", "location", "tell me", "show me",
		"information", "details", "business",
	}

	isBasicQuestion := false
	for _, basic := range basicQuestions {
		if strings.Contains(queryLower, basic) {
			isBasicQuestion = true
			break
		}
	}

	// ✅ For greetings, return minimal chunks
	greetings := []string{"hello", "hi", "hey", "good morning", "good afternoon", "good evening"}
	for _, g := range greetings {
		if strings.Contains(queryLower, g) {
			if len(allChunks) > 0 {
				if len(allChunks) < 3 {
					return allChunks, nil
				}
				return allChunks[:3], nil
			}
			return []models.ContentChunk{}, nil
		}
	}

	// ✅ If basic question or no specific keywords, return LIMITED chunks
	if isBasicQuestion || len(allChunks) <= maxChunks {
		sort.Slice(allChunks, func(i, j int) bool {
			return allChunks[i].Order < allChunks[j].Order
		})

		if len(allChunks) <= maxChunks {
			return allChunks, nil
		}
		return allChunks[:maxChunks], nil
	}

	// ✅ ADVANCED KEYWORD SEARCH for specific questions
	queryWords := strings.Fields(queryLower)

	type scoredChunk struct {
		chunk models.ContentChunk
		score int
	}

	var scored []scoredChunk

	for _, chunk := range allChunks {
		chunkLower := strings.ToLower(chunk.Text)
		score := 0

		// Enhanced scoring system
		for _, word := range queryWords {
			if len(word) > 2 {
				// Higher score for exact word matches
				if strings.Contains(chunkLower, word) {
					score += strings.Count(chunkLower, word) * 2
				}

				// Partial scoring for similar words
				if len(word) > 3 && strings.Contains(chunkLower, word[:len(word)-1]) {
					score += 1
				}
			}
		}

		scored = append(scored, scoredChunk{chunk: chunk, score: score})
	}

	// Sort by score (descending) then by order (ascending)
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].chunk.Order < scored[j].chunk.Order
		}
		return scored[i].score > scored[j].score
	})

	// ✅ Return best matching chunks
	var relevantChunks []models.ContentChunk
	limit := maxChunks
	if len(scored) < limit {
		limit = len(scored)
	}

	hasGoodMatches := false
	for _, s := range scored {
		if s.score > 0 {
			hasGoodMatches = true
			break
		}
	}

	if !hasGoodMatches {
		// Return first few chunks which usually contain company intro
		for i := 0; i < limit && i < len(scored); i++ {
			relevantChunks = append(relevantChunks, scored[i].chunk)
		}
	} else {
		// Return best matching chunks
		goodMatches := 0
		for i := 0; i < limit && i < len(scored) && scored[i].score > 0; i++ {
			relevantChunks = append(relevantChunks, scored[i].chunk)
			goodMatches++
		}

		// If we don't have enough good matches, add some general chunks
		if len(relevantChunks) < maxChunks {
			needed := maxChunks - len(relevantChunks)
			for i := len(relevantChunks); i < len(scored) && needed > 0; i++ {
				relevantChunks = append(relevantChunks, scored[i].chunk)
				needed--
			}
		}
	}

	fmt.Printf("Debug: Returning %d relevant crawled chunks\n", len(relevantChunks))
	return relevantChunks, nil
}

// =========================
// PDF EXTRACTION FUNCTIONS
// =========================

// extractTextSmart uses multiple extraction methods with fallbacks
func extractTextSmart(fileContent []byte, originalName, apiKey, model string) (string, int, error) {
	// 1) Try ledongthuc/pdf first
	text1, pages1, err1 := extractWithGoPDF(fileContent)
	if qualityOK(text1) {
		return sanitize(text1), pages1, nil
	}

	// 2) Try Poppler (pdftotext) if available
	if hasBinary("pdftotext") {
		if txt, err := extractWithPoppler(fileContent); err == nil && qualityOK(txt) {
			pages := pages1
			if pages == 0 {
				pages = guessPagesFromMarkers(txt)
			}
			return sanitize(txt), pages, nil
		}
	}

	// 3) Fallback to Gemini File API
	txt, err := processPDFWithGeminiBytes(fileContent, originalName, apiKey, model)
	if err != nil {
		return "", 0, fmt.Errorf("goPDF err: %v; gemini err: %w", err1, err)
	}
	pages := guessPagesFromMarkers(txt)
	return sanitize(txt), pages, nil
}

// extractTextSmartWithContext uses multiple extraction methods with fallbacks and respects context timeout
func extractTextSmartWithContext(ctx context.Context, fileContent []byte, originalName, apiKey, model string) (string, int, error) {
	// Check context before starting
	select {
	case <-ctx.Done():
		return "", 0, fmt.Errorf("context cancelled before processing: %w", ctx.Err())
	default:
	}

	// For small files, try local extraction first and avoid Gemini if possible
	fileSize := len(fileContent)
	fmt.Printf("Processing file %s (size: %d bytes)\n", originalName, fileSize)

	// ✅ PRIORITIZE GEMINI EXTRACTION FOR BETTER QUALITY
	// For most PDFs, Gemini provides much better text extraction than local libraries

	fmt.Printf("Starting PDF extraction for file %s (%d bytes)\n", originalName, fileSize)

	// 1) Try Gemini first for files under 10MB (Gemini handles complex PDFs better)
	if fileSize < 10*1024*1024 { // Less than 10MB
		fmt.Printf("Trying Gemini extraction first for file %s\n", originalName)
		geminiStart := time.Now()
		geminiText, err := processPDFWithGeminiBytesWithContext(ctx, fileContent, originalName, apiKey, model)
		geminiDuration := time.Since(geminiStart)
		fmt.Printf("Gemini extraction completed in %v for file %s\n", geminiDuration, originalName)

		if err == nil && qualityOK(geminiText) {
			pages := guessPagesFromMarkers(geminiText)
			if pages == 0 {
				pages = 1 // Default to 1 page if can't determine
			}
			fmt.Printf("✅ Gemini extraction successful for file %s (%d pages, %d chars)\n", originalName, pages, len(geminiText))
			return sanitize(geminiText), pages, nil
		}
		fmt.Printf("❌ Gemini extraction failed for file %s: %v\n", originalName, err)
	}

	// 2) Try Go PDF as fallback (faster but often corrupted)
	fmt.Printf("Trying Go PDF extraction as fallback for file %s\n", originalName)
	extractStart := time.Now()
	text1, pages1, err1 := extractWithGoPDF(fileContent)
	extractDuration := time.Since(extractStart)
	fmt.Printf("Go PDF extraction completed in %v for file %s\n", extractDuration, originalName)

	if qualityOK(text1) {
		fmt.Printf("✅ Go PDF extraction successful for file %s (%d pages, %d chars)\n", originalName, pages1, len(text1))
		return sanitize(text1), pages1, nil
	}
	fmt.Printf("❌ Go PDF extraction failed quality check for file %s: %v (extracted %d chars)\n", originalName, err1, len(text1))

	// 3) Try Poppler if available
	if hasBinary("pdftotext") {
		fmt.Printf("Trying Poppler extraction for file %s\n", originalName)
		if txt, err := extractWithPoppler(fileContent); err == nil && qualityOK(txt) {
			pages := guessPagesFromMarkers(txt)
			if pages == 0 {
				pages = pages1
				if pages == 0 {
					pages = 1
				}
			}
			fmt.Printf("✅ Poppler extraction successful for file %s (%d pages)\n", originalName, pages)
			return sanitize(txt), pages, nil
		}
		fmt.Printf("❌ Poppler extraction failed for file %s\n", originalName)
	}

	// 4) Force Gemini for larger files if other methods failed
	if fileSize >= 10*1024*1024 {
		fmt.Printf("Forcing Gemini extraction for large file %s\n", originalName)
		txt, err := processPDFWithGeminiBytesWithContext(ctx, fileContent, originalName, apiKey, model)
		if err == nil {
			pages := guessPagesFromMarkers(txt)
			if pages == 0 {
				pages = max(pages1, 1)
			}
			fmt.Printf("✅ Gemini extraction successful for large file %s (%d pages)\n", originalName, pages)
			return sanitize(txt), pages, nil
		}
		fmt.Printf("❌ Gemini extraction failed for large file %s: %v\n", originalName, err)
	}

	// 5) Last resort: try to use whatever text we extracted, even if corrupted
	fmt.Printf("⚠️ WARNING: All extraction methods failed for %s, using best available text\n", originalName)
	fmt.Printf("Final result: %d pages, %d chars for file %s\n", max(pages1, 1), len(text1), originalName)

	// Use the best text we have, even if it's not perfect
	bestText := text1
	if len(bestText) < 50 {
		// If we have very little text, try Gemini one more time without quality check
		fmt.Printf("Text too short (%d chars), trying Gemini without quality check\n", len(bestText))
		if geminiText, err := processPDFWithGeminiBytesWithContext(ctx, fileContent, originalName, apiKey, model); err == nil && len(geminiText) > len(bestText) {
			bestText = geminiText
			fmt.Printf("Got better text from Gemini: %d chars\n", len(bestText))
		}
	}

	// Clean up the text but don't replace with generic message
	cleanedText := cleanCorruptedText(bestText)
	if len(cleanedText) < 50 {
		// Only use generic message if we truly have nothing useful
		cleanedText = "This document contains company information that could not be properly extracted. Please contact us directly for detailed information about our services and contact details."
	}

	return sanitize(cleanedText), max(pages1, 1), nil
}

// extractWithGoPDF extracts text using the Go PDF library
func extractWithGoPDF(fileContent []byte) (string, int, error) {
	reader, err := pdf.NewReader(bytes.NewReader(fileContent), int64(len(fileContent)))
	if err != nil {
		return "", 0, err
	}

	var b strings.Builder
	pages := reader.NumPage()

	for i := 1; i <= pages; i++ {
		p := reader.Page(i)
		if p.V.IsNull() {
			continue
		}
		fonts := make(map[string]*pdf.Font)
		t, err := p.GetPlainText(fonts)
		if err != nil {
			continue
		}
		b.WriteString(fmt.Sprintf("\n\n[[PAGE %d]]\n", i))
		b.WriteString(t)
	}

	return b.String(), pages, nil
}

// hasBinary checks if a binary exists in PATH
func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// extractWithPoppler extracts text using pdftotext
func extractWithPoppler(fileContent []byte) (string, error) {
	cmd := exec.Command("pdftotext", "-layout", "-", "-")
	cmd.Stdin = bytes.NewReader(fileContent)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pdftotext error: %v (%s)", err, errb.String())
	}

	return out.String(), nil
}

// processPDFWithGeminiBytes processes PDF using Gemini File API
func processPDFWithGeminiBytes(data []byte, filename, apiKey, modelName string) (string, error) {
	tmpDir := os.TempDir()
	if filename == "" {
		filename = "upload.pdf"
	}

	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(filename)))
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	return processPDFWithGemini(tmpPath, apiKey, modelName)
}

// processPDFWithGeminiBytesWithContext processes PDF using Gemini File API with context timeout
func processPDFWithGeminiBytesWithContext(ctx context.Context, data []byte, filename, apiKey, modelName string) (string, error) {
	tmpDir := os.TempDir()
	if filename == "" {
		filename = "upload.pdf"
	}

	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(filename)))
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	defer os.Remove(tmpPath)

	return processPDFWithGeminiWithContext(ctx, tmpPath, apiKey, modelName)
}

// processPDFWithGemini processes PDF file using Gemini File API
func processPDFWithGemini(filePath, apiKey, modelName string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return processPDFWithGeminiWithContext(ctx, filePath, apiKey, modelName)
}

// processPDFWithGeminiWithContext processes PDF file using Gemini File API with context timeout
func processPDFWithGeminiWithContext(ctx context.Context, filePath, apiKey, modelName string) (string, error) {
	// Check context before starting
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("context cancelled before Gemini processing: %w", ctx.Err())
	default:
	}

	fmt.Printf("Creating Gemini client for file %s\n", filePath)
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %v", err)
	}
	defer client.Close()

	fmt.Printf("Uploading file %s to Gemini\n", filePath)
	file, err := client.UploadFileFromPath(ctx, filePath, nil)
	if err != nil {
		return "", fmt.Errorf("failed to upload file to Gemini: %v", err)
	}
	fmt.Printf("File uploaded successfully, state: %v\n", file.State)

	// Wait for file processing with context-aware polling
	// Adjust wait time based on file size
	maxWait := 60 * time.Second // Base wait time

	// Get file size for timeout calculation
	fileInfo, err := os.Stat(filePath)
	if err == nil {
		fileSize := fileInfo.Size()
		if fileSize > 10485760 { // Files larger than 10MB
			maxWait = 300 * time.Second // 5 minutes for very large files
		} else if fileSize > 1048576 { // Files larger than 1MB
			maxWait = 120 * time.Second // 2 minutes for large files
		}
		fmt.Printf("Gemini processing timeout set to %v for file %s (size: %d bytes)\n", maxWait, filePath, fileSize)
	} else {
		fmt.Printf("Gemini processing timeout set to %v for file %s (size unknown)\n", maxWait, filePath)
	}

	deadline := time.Now().Add(maxWait)
	pollInterval := 2 * time.Second // Increased polling interval for large files
	pollCount := 0

	for file.State == genai.FileStateProcessing {
		pollCount++
		fmt.Printf("File still processing, poll #%d, state: %v\n", pollCount, file.State)

		// Check context cancellation first
		select {
		case <-ctx.Done():
			fmt.Printf("Context cancelled during file processing: %v\n", ctx.Err())
			return "", fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		if time.Now().After(deadline) {
			fmt.Printf("File processing deadline exceeded after %d polls\n", pollCount)
			return "", errors.New("file processing timeout")
		}

		time.Sleep(pollInterval)
		file, err = client.GetFile(ctx, file.Name)
		if err != nil {
			fmt.Printf("Failed to check file status: %v\n", err)
			return "", fmt.Errorf("failed to check file status: %v", err)
		}
	}

	fmt.Printf("File processing completed, final state: %v\n", file.State)
	if file.State != genai.FileStateActive {
		return "", fmt.Errorf("file processing failed with state: %v", file.State)
	}

	// Generate content using FREE tier model
	// 🆓 Ensure FREE tier model
	validModel := ensureFreeGeminiModel(modelName)
	fmt.Printf("🎯 Generating content with FREE tier model: %s...\n", validModel)
	model := client.GenerativeModel(validModel)
	prompt := genai.Text(`
Return ONLY plain text from this PDF.
- Keep reading order.
- Insert a separate line as [[PAGE N]] at each page start (N = 1,2,3...).
- No markdown, no bullets unless they exist in the text.
`)

	resp, err := model.GenerateContent(ctx,
		genai.FileData{URI: file.URI, MIMEType: file.MIMEType},
		prompt,
	)
	if err != nil {
		// Check if it's a quota error
		if isGeminiQuotaError(err) {
			return "", fmt.Errorf("gemini free tier quota exceeded: %w", err)
		}
		return "", fmt.Errorf("content generation failed: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0] == nil || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		fmt.Printf("No content generated from PDF\n")
		return "", errors.New("no content generated from PDF")
	}

	var out strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		switch v := p.(type) {
		case genai.Text:
			out.WriteString(string(v))
		default:
			// ignore other types
		}
	}

	fmt.Printf("Content generation completed, extracted %d characters\n", out.Len())
	return out.String(), nil
}

// ===================
// TEXT PROCESSING
// ===================

// cleanCorruptedText attempts to clean up corrupted text as a last resort
func cleanCorruptedText(text string) string {
	if text == "" {
		return text
	}

	// Remove common corruption patterns
	corrupted := []string{"◊", "�", "♦", "♠", "♣", "♥", "▲", "▼", "►", "◄", "→", "←", "↑", "↓"}
	cleaned := text

	for _, char := range corrupted {
		cleaned = strings.ReplaceAll(cleaned, char, " ")
	}

	// Remove excessive brackets and symbols
	cleaned = strings.ReplaceAll(cleaned, "]]", " ")
	cleaned = strings.ReplaceAll(cleaned, "[[", " ")
	cleaned = strings.ReplaceAll(cleaned, "}}}", " ")
	cleaned = strings.ReplaceAll(cleaned, "{{{", " ")
	cleaned = strings.ReplaceAll(cleaned, "###", " ")

	// Remove multiple spaces
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}

	// Don't replace with generic message - return the cleaned text as-is
	// The caller will decide if it's useful enough
	return strings.TrimSpace(cleaned)
}

// sanitize cleans up extracted text
func sanitize(s string) string {
	if s == "" {
		return s
	}

	var b strings.Builder
	for _, r := range s {
		// keep newlines/tabs/printables
		if r == '\n' || r == '\r' || r == '\t' || (r >= 32 && r < 0xD800) || (r >= 0xE000 && r <= 0xFFFD) {
			b.WriteRune(r)
		}
	}

	// Normalize whitespace
	clean := strings.ReplaceAll(b.String(), "\u0000", "")
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")

	return strings.TrimSpace(clean)
}

// qualityOK checks if extracted text quality is acceptable
func qualityOK(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 20 {
		return false
	}

	total := 0
	printable := 0
	questionMarks := 0
	alphanumeric := 0
	specialChars := 0

	for _, r := range trimmed {
		total++
		if r == '�' || r == '\uFFFD' {
			questionMarks++
		}

		// Count letters and numbers
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			alphanumeric++
			printable++
		} else if r == ' ' || r == '\n' || r == '\t' || r == '.' || r == ',' || r == '-' || r == ':' {
			printable++
		} else if (r >= 32 && r < 0xD800) || (r >= 0xE000 && r <= 0xFFFD) {
			printable++
			// Count unusual characters that might indicate corruption
			if r > 127 && r != '—' && r != '"' && r != '"' && r != '\'' && r != '\'' {
				specialChars++
			}
		}
	}

	if total == 0 {
		return false
	}

	printableRatio := float64(printable) / float64(total)
	alphanumericRatio := float64(alphanumeric) / float64(total)

	// ✅ RELAXED QUALITY CHECKS - Be more permissive
	// 1. Too many question marks/corrupted chars (relaxed threshold)
	if questionMarks > 20 || float64(questionMarks)/float64(total) > 0.15 {
		fmt.Printf("Quality check failed: too many corrupted characters (%d/%d)\n", questionMarks, total)
		return false
	}

	// 2. Too many special/unusual characters (relaxed threshold)
	if float64(specialChars)/float64(total) > 0.30 {
		fmt.Printf("Quality check failed: too many special characters (%d/%d)\n", specialChars, total)
		return false
	}

	// 3. Not enough alphanumeric content (relaxed threshold)
	if alphanumericRatio < 0.15 {
		fmt.Printf("Quality check failed: not enough alphanumeric content (%.2f)\n", alphanumericRatio)
		return false
	}

	// 4. Overall printable ratio too low (relaxed threshold)
	if printableRatio < 0.70 {
		fmt.Printf("Quality check failed: low printable ratio (%.2f)\n", printableRatio)
		return false
	}

	// ✅ CHECK FOR COMMON CORRUPTED PATTERNS
	corruptedPatterns := []string{"���", "◊◊", "]]", "}}}", "###", "���"}
	for _, pattern := range corruptedPatterns {
		if strings.Count(trimmed, pattern) > 10 {
			fmt.Printf("Quality check failed: found corrupted pattern '%s'\n", pattern)
			return false
		}
	}

	fmt.Printf("Quality check passed: printable=%.2f, alphanumeric=%.2f, special=%d, corrupted=%d\n",
		printableRatio, alphanumericRatio, specialChars, questionMarks)
	return true
}

// guessPagesFromMarkers estimates page count from page markers
func guessPagesFromMarkers(s string) int {
	count := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[[PAGE ") && strings.HasSuffix(line, "]]") {
			count++
		}
	}
	return count
}

// chunkTextSmart creates intelligent text chunks
func chunkTextSmart(text string, maxChunkWords, overlapWords int) []models.ContentChunk {
	if strings.TrimSpace(text) == "" {
		return []models.ContentChunk{}
	}

	// Split by page markers first, then paragraphs
	blocks := splitByPageThenPara(text)
	var chunks []models.ContentChunk
	order := 0

	for _, block := range blocks {
		words := strings.Fields(block)
		if len(words) == 0 {
			continue
		}

		for i := 0; i < len(words); {
			end := i + maxChunkWords
			if end > len(words) {
				end = len(words)
			}

			chunkText := strings.Join(words[i:end], " ")
			chunks = append(chunks, models.ContentChunk{
				ChunkID: uuid.New().String(),
				Text:    chunkText,
				Order:   order,
			})
			order++

			if end >= len(words) {
				break
			}

			nextStart := end - overlapWords
			if nextStart <= i {
				nextStart = i + 1
			}
			i = nextStart
		}
	}

	return chunks
}

// splitByPageThenPara splits text by pages and then paragraphs
func splitByPageThenPara(text string) []string {
	lines := strings.Split(text, "\n")
	var blocks []string
	var cur []string

	flush := func() {
		para := strings.TrimSpace(strings.Join(cur, "\n"))
		if para != "" {
			// Further split by blank lines to avoid massive blocks
			for _, p := range strings.Split(para, "\n\n") {
				pt := strings.TrimSpace(p)
				if pt != "" {
					blocks = append(blocks, pt)
				}
			}
		}
		cur = cur[:0]
	}

	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[[PAGE ") && strings.HasSuffix(t, "]]") {
			flush()
			// Skip marker line
			continue
		}
		cur = append(cur, line)
	}
	flush()

	return blocks
}

// ===================
// UTILITY FUNCTIONS
// ===================

// Helper functions for pointer conversion
func float32Ptr(f float32) *float32 {
	return &f
}

func int32Ptr(i int32) *int32 {
	return &i
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// calculateTimeout determines appropriate timeout based on file size
func calculateTimeout(fileSize int64) time.Duration {
	switch {
	case fileSize < 500*1024: // < 500KB
		return 30 * time.Second
	case fileSize < 2*1024*1024: // < 2MB
		return 60 * time.Second
	case fileSize < 5*1024*1024: // < 5MB
		return 180 * time.Second // 3 minutes
	default: // Up to 10MB
		return 300 * time.Second // 5 minutes
	}
}

// categorizeError determines the appropriate error response
func categorizeError(err error, filename string, fileSize int64) (statusCode int, errorCode string, message string) {
	errStr := err.Error()

	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "context") || strings.Contains(errStr, "deadline") {
		return http.StatusRequestTimeout, "pdf_processing_timeout",
			fmt.Sprintf("PDF processing timed out. File: %s (%s). Try a smaller file.", filename, formatBytes(fileSize))
	}

	if strings.Contains(errStr, "API key") || strings.Contains(errStr, "authentication") {
		return http.StatusInternalServerError, "api_key_error",
			"Gemini API authentication failed. Please contact support."
	}

	if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") {
		return http.StatusInternalServerError, "model_not_found",
			"PDF processing model not available. Please contact support."
	}

	if strings.Contains(errStr, "corrupted") || strings.Contains(errStr, "invalid") {
		return http.StatusBadRequest, "invalid_pdf",
			"PDF file appears to be corrupted or invalid."
	}

	if strings.Contains(errStr, "AI processing failed") {
		return http.StatusInternalServerError, "pdf_processing_error",
			"AI processing failed. Please try again later."
	}

	return http.StatusInternalServerError, "pdf_processing_error",
		fmt.Sprintf("Failed to process PDF file: %s", filename)
}

// formatBytes converts bytes to human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// qualityOKWithThreshold checks text quality with custom threshold
func qualityOKWithThreshold(s string, minRatio float64) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 50 {
		return false
	}

	total := 0
	printable := 0
	questionMarks := 0

	for _, r := range trimmed {
		total++
		if r == '\uFFFD' {
			questionMarks++
		}
		if (r >= 32 && r < 0xD800) || (r >= 0xE000 && r <= 0xFFFD) || r == '\n' || r == '\t' {
			printable++
		}
	}

	if total == 0 {
		return false
	}

	ratio := float64(printable) / float64(total)
	return ratio > minRatio && questionMarks < 30
}

// ensureFreeGeminiModel ensures only FREE TIER Gemini models with correct versions
func ensureFreeGeminiModel(requestedModel string) string {
	// 🆓 FREE TIER MODELS with version numbers (Required by Gemini API)
	freeModels := map[string]string{
		// Auto-correct unversioned names to versioned ones
		"gemini-1.5-flash":        "gemini-2.0-flash",
		"gemini-1.5-flash-001":    "gemini-2.0-flash",
		"gemini-1.5-flash-002":    "gemini-2.0-flash",
		"gemini-1.5-flash-latest": "gemini-2.0-flash",
		"gemini-2.0-flash":        "gemini-2.0-flash",
		"gemini-2.0-flash-exp":    "gemini-2.0-flash",
		// Paid models (will be converted to free)
		"gemini-1.5-pro":     "gemini-2.0-flash",
		"gemini-1.5-pro-001": "gemini-2.0-flash",
		"gemini-1.5-pro-002": "gemini-2.0-flash",
		"gemini-pro":         "gemini-2.0-flash",
	}

	// Get the corrected free model
	if validModel, ok := freeModels[requestedModel]; ok {
		if requestedModel != validModel {
			fmt.Printf("✅ Auto-correcting: %s → %s (FREE tier)\n", requestedModel, validModel)
		} else {
			fmt.Printf("✅ Using FREE tier model: %s\n", validModel)
		}
		return validModel
	}

	// Unknown model - default to free flash
	fmt.Printf("⚠️ Unknown model '%s', using default: gemini-2.0-flash\n", requestedModel)
	return "gemini-2.0-flash"
}

// isGeminiQuotaError checks if error is due to API quota/rate limits
func isGeminiQuotaError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "quota") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "resource exhausted")
}

// calculateProcessingTimeout returns appropriate timeout based on file size
func calculateProcessingTimeout(fileSize int64) time.Duration {
	switch {
	case fileSize < 500*1024: // < 500KB
		return 30 * time.Second
	case fileSize < 2*1024*1024: // < 2MB
		return 60 * time.Second
	case fileSize < 5*1024*1024: // < 5MB
		return 180 * time.Second
	default: // Up to 10MB
		return 300 * time.Second
	}
}

// categorizeProcessingError determines appropriate error response
func categorizeProcessingError(err error, filename string, fileSize int64) (statusCode int, errorCode string, message string) {
	errStr := err.Error()

	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
		return http.StatusRequestTimeout, "pdf_processing_timeout",
			fmt.Sprintf("Processing timed out for %s (%s). Try a smaller file.", filename, formatBytes(fileSize))
	}

	if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") {
		return http.StatusInternalServerError, "model_not_available",
			"PDF processing model temporarily unavailable. Try again later."
	}

	if strings.Contains(errStr, "authentication") || strings.Contains(errStr, "API key") {
		return http.StatusInternalServerError, "api_configuration_error",
			"API configuration error. Please contact support."
	}

	return http.StatusInternalServerError, "pdf_processing_error",
		"Failed to process PDF. Please try again or contact support."
}

// ========== CHAT EXPORT HANDLERS ==========

// handleExportChats handles chat export requests
func handleExportChats(messagesCollection, clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
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
	}
}

// handleDownloadExport handles direct download of exported chat data
func handleDownloadExport(messagesCollection, clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
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
		var err error

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

		// Build export request
		req := &services.ExportRequest{
			Format:         format,
			DateFrom:       dateFrom,
			DateTo:         dateTo,
			ClientID:       c.Query("client_id"),
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
		// This is a simplified version - in production, you might want to cache the results
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
	}
}

// ========== CRAWLER HANDLERS ==========

// handleStartCrawl starts a new crawl job
func handleStartCrawl(cfg *config.Config, crawlsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
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

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Create crawl job
		crawlJob := models.CrawlJob{
			ID:             primitive.NewObjectID(),
			ClientID:       clientObjID,
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

		// Start crawl in background goroutine
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
				Timeout:        60 * time.Second, // Increased timeout for production
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
							"status":          models.CrawlStatusCompleted, // Mark as completed even with partial data
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

			// Update crawl job with results
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

			fmt.Printf("✅ Crawl completed for %s: %d pages in %v\n", req.URL, result.PagesCrawled, processingTime)
		}()

		c.JSON(http.StatusOK, gin.H{
			"id":      crawlJob.ID.Hex(),
			"url":     crawlJob.URL,
			"status":  crawlJob.Status,
			"message": "Crawl job started successfully",
		})
	}
}

// handleBulkCrawl handles bulk URL crawling - creates multiple crawl jobs
func handleBulkCrawl(cfg *config.Config, crawlsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		var req struct {
			URLs           []string `json:"urls" binding:"required,min=1"`
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

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx := context.Background()
		var createdJobs []gin.H

		// Create a crawl job for each URL
		for _, urlStr := range validURLs {
			crawlJob := models.CrawlJob{
				ID:             primitive.NewObjectID(),
				ClientID:       clientObjID,
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

			// Start crawl in background goroutine with production error handling
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
			}(crawlJob.ID.Hex(), urlStr)
		}

		c.JSON(http.StatusOK, gin.H{
			"message":      fmt.Sprintf("Bulk crawl started: %d jobs created", len(createdJobs)),
			"jobs_created": len(createdJobs),
			"jobs":         createdJobs,
		})
	}
}

// handleListCrawls lists all crawl jobs for the client
func handleListCrawls(crawlsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
		skip := (page - 1) * limit

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := crawlsCollection.Find(ctx,
			bson.M{"client_id": clientObjID},
			&options.FindOptions{
				Skip:  &[]int64{int64(skip)}[0],
				Limit: &[]int64{int64(limit)}[0],
				Sort:  bson.M{"created_at": -1},
			},
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve crawls",
			})
			return
		}
		defer cursor.Close(ctx)

		var crawls []models.CrawlJob
		if err := cursor.All(ctx, &crawls); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to decode crawls",
			})
			return
		}

		total, _ := crawlsCollection.CountDocuments(ctx, bson.M{"client_id": clientObjID})

		c.JSON(http.StatusOK, gin.H{
			"crawls":      crawls,
			"total":       total,
			"page":        page,
			"limit":       limit,
			"total_pages": (total + int64(limit) - 1) / int64(limit),
		})
	}
}

// handleGetCrawl gets a specific crawl job
func handleGetCrawl(crawlsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		crawlID := c.Param("id")
		crawlObjID, err := primitive.ObjectIDFromHex(crawlID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_crawl_id",
				"message":    "Invalid crawl ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var crawlJob models.CrawlJob
		err = crawlsCollection.FindOne(ctx, bson.M{
			"_id":       crawlObjID,
			"client_id": clientObjID,
		}).Decode(&crawlJob)

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

		c.JSON(http.StatusOK, crawlJob)
	}
}

// handleCrawlStatus returns the status of a crawl job
func handleCrawlStatus(crawlsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		crawlID := c.Param("id")
		crawlObjID, err := primitive.ObjectIDFromHex(crawlID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_crawl_id",
				"message":    "Invalid crawl ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var crawlJob models.CrawlJob
		err = crawlsCollection.FindOne(ctx, bson.M{
			"_id":       crawlObjID,
			"client_id": clientObjID,
		}).Decode(&crawlJob)

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
				"message":    "Failed to retrieve crawl status",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"id":            crawlJob.ID.Hex(),
			"url":           crawlJob.URL,
			"status":        crawlJob.Status,
			"progress":      crawlJob.Progress,
			"pages_found":   crawlJob.PagesFound,
			"pages_crawled": crawlJob.PagesCrawled,
			"created_at":    crawlJob.CreatedAt,
			"updated_at":    crawlJob.UpdatedAt,
			"completed_at":  crawlJob.CompletedAt,
			"error":         crawlJob.Error,
		})
	}
}

// handleDeleteCrawl deletes a crawl job
func handleDeleteCrawl(crawlsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		crawlID := c.Param("id")
		crawlObjID, err := primitive.ObjectIDFromHex(crawlID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_crawl_id",
				"message":    "Invalid crawl ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx := context.Background()
		result, err := crawlsCollection.DeleteOne(ctx, bson.M{
			"_id":       crawlObjID,
			"client_id": clientObjID,
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
	}
}

// Helper functions for crawl operations
func updateCrawlStatus(crawlsCollection *mongo.Collection, crawlID string, status string, progress int) {
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

func updateCrawlError(crawlsCollection *mongo.Collection, crawlID string, errorMsg string) {
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

// ========== IMAGE HANDLERS ==========

// handleGetImages returns all images for the authenticated client
func handleGetImages(imagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := imagesCollection.Find(ctx, bson.M{"client_id": clientObjID})
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
	}
}

// handleAddImage adds a new image for the authenticated client
func handleAddImage(imagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		image := models.Image{
			ID:        primitive.NewObjectID(),
			ClientID:  clientObjID,
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
	}
}

// handleDeleteImage deletes an image for the authenticated client
func handleDeleteImage(imagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		imageID := c.Param("id")
		imageObjID, err := primitive.ObjectIDFromHex(imageID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_image_id",
				"message":    "Invalid image ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		result, err := imagesCollection.DeleteOne(ctx, bson.M{
			"_id":       imageObjID,
			"client_id": clientObjID,
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
	}
}

// handlePublicImages returns all images for a specific client (public endpoint for embed widget)
func handlePublicImages(imagesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := imagesCollection.Find(ctx, bson.M{"client_id": clientOID})
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
	}
}

// handlePublicCalendly returns Calendly configuration for a specific client (public endpoint for embed widget)
func handlePublicCalendly(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientOID}).Decode(&client)
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
	}
}

// handleGetCalendly returns Calendly configuration for the authenticated client
func handleGetCalendly(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&client)
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
	}
}

// handleUpdateCalendly updates Calendly configuration for the authenticated client
func handleUpdateCalendly(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
			// Basic URL validation
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

		result, err := clientsCollection.UpdateOne(
			ctx,
			bson.M{"_id": clientObjID},
			update,
		)

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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&updatedClient)
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
	}
}

// handlePublicQRCode returns QR code configuration for a specific client (public endpoint for embed widget)
func handlePublicQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientOID}).Decode(&client)
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
	}
}

// handleGetQRCode returns QR code configuration for the authenticated client
func handleGetQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&client)
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
	}
}

// handleUpdateQRCode updates QR code configuration for the authenticated client
func handleUpdateQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
			
			// Accept data URLs (base64 encoded images)
			if strings.HasPrefix(qrCodeURL, "data:image/") {
				// Data URL is valid, no further validation needed
			} else if strings.HasPrefix(qrCodeURL, "/") {
				// Relative URL is valid
			} else {
				// Validate absolute URL format
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

		result, err := clientsCollection.UpdateOne(
			ctx,
			bson.M{"_id": clientObjID},
			update,
		)

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

		// Fetch updated client to return
		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&updatedClient)
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
	}
}

// handlePublicWhatsAppQRCode returns WhatsApp QR code configuration for a specific client (public endpoint for embed widget)
func handlePublicWhatsAppQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientOID}).Decode(&client)
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
	}
}

// handleGetWhatsAppQRCode returns WhatsApp QR code configuration for the authenticated client
func handleGetWhatsAppQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&client)
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
	}
}

// handleUpdateWhatsAppQRCode updates WhatsApp QR code configuration for the authenticated client
func handleUpdateWhatsAppQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
			
			// Accept data URLs (base64 encoded images)
			if strings.HasPrefix(qrCodeURL, "data:image/") {
				// Data URL is valid, no further validation needed
			} else if strings.HasPrefix(qrCodeURL, "/") {
				// Relative URL is valid
			} else {
				// Validate absolute URL format
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

		result, err := clientsCollection.UpdateOne(
			ctx,
			bson.M{"_id": clientObjID},
			update,
		)

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

		// Fetch updated client to return
		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&updatedClient)
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
	}
}

// handlePublicTelegramQRCode returns Telegram QR code configuration for a specific client (public endpoint for embed widget)
func handlePublicTelegramQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientOID}).Decode(&client)
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
	}
}

// handleGetTelegramQRCode returns Telegram QR code configuration for the authenticated client
func handleGetTelegramQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&client)
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
	}
}

// handleUpdateTelegramQRCode updates Telegram QR code configuration for the authenticated client
func handleUpdateTelegramQRCode(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
			
			// Accept data URLs (base64 encoded images)
			if strings.HasPrefix(qrCodeURL, "data:image/") {
				// Data URL is valid, no further validation needed
			} else if strings.HasPrefix(qrCodeURL, "/") {
				// Relative URL is valid
			} else {
				// Validate absolute URL format
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

		result, err := clientsCollection.UpdateOne(
			ctx,
			bson.M{"_id": clientObjID},
			update,
		)

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

		// Fetch updated client to return
		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&updatedClient)
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
	}
}

// ========== FACEBOOK POST HANDLERS ==========

// handleGetFacebookPosts returns all Facebook posts for the authenticated client
func handleGetFacebookPosts(facebookPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := facebookPostsCollection.Find(ctx, bson.M{"client_id": clientObjID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Facebook posts",
			})
			return
		}
		defer cursor.Close(ctx)

		var posts []models.FacebookPost
		if err = cursor.All(ctx, &posts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode Facebook posts",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"posts": posts,
			"count": len(posts),
		})
	}
}

// handleAddFacebookPost adds a new Facebook post for the authenticated client
func handleAddFacebookPost(facebookPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		post := models.FacebookPost{
			ID:        primitive.NewObjectID(),
			ClientID:  clientObjID,
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
	}
}

// handleDeleteFacebookPost deletes a Facebook post for the authenticated client
func handleDeleteFacebookPost(facebookPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		postID := c.Param("id")
		postObjID, err := primitive.ObjectIDFromHex(postID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_post_id",
				"message":    "Invalid post ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		result, err := facebookPostsCollection.DeleteOne(ctx, bson.M{
			"_id":       postObjID,
			"client_id": clientObjID,
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
	}
}

// handleGetFacebookPostsConfig returns Facebook posts configuration for the authenticated client
func handleGetFacebookPostsConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&client)
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
	}
}

// handleUpdateFacebookPostsConfig updates Facebook posts configuration for the authenticated client
func handleUpdateFacebookPostsConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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
			bson.M{"_id": clientObjID},
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message":                 "Facebook posts configuration updated successfully",
				"facebook_posts_enabled":  request.FacebookPostsEnabled,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":                 "Facebook posts configuration updated successfully",
			"facebook_posts_enabled":  updatedClient.FacebookPostsEnabled,
		})
	}
}

// handlePublicFacebookPosts returns all Facebook posts for a specific client (public endpoint for embed widget)
func handlePublicFacebookPosts(facebookPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := facebookPostsCollection.Find(ctx, bson.M{"client_id": clientOID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Facebook posts",
			})
			return
		}
		defer cursor.Close(ctx)

		var posts []models.FacebookPost
		if err = cursor.All(ctx, &posts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode Facebook posts",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"posts": posts,
			"count": len(posts),
		})
	}
}

// handlePublicFacebookPostsConfig returns Facebook posts configuration for a specific client (public endpoint for embed widget)
func handlePublicFacebookPostsConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientOID}).Decode(&client)
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
	}
}

// ========== INSTAGRAM POST HANDLERS ==========

// handleGetInstagramPosts returns all Instagram posts for the authenticated client
func handleGetInstagramPosts(instagramPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := instagramPostsCollection.Find(ctx, bson.M{"client_id": clientObjID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Instagram posts",
			})
			return
		}
		defer cursor.Close(ctx)

		var posts []models.InstagramPost
		if err = cursor.All(ctx, &posts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode Instagram posts",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"posts": posts,
			"count": len(posts),
		})
	}
}

// handleAddInstagramPost adds a new Instagram post for the authenticated client
func handleAddInstagramPost(instagramPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		post := models.InstagramPost{
			ID:        primitive.NewObjectID(),
			ClientID:  clientObjID,
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
	}
}

// handleDeleteInstagramPost deletes an Instagram post for the authenticated client
func handleDeleteInstagramPost(instagramPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		postID := c.Param("id")
		postObjID, err := primitive.ObjectIDFromHex(postID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_post_id",
				"message":    "Invalid post ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		result, err := instagramPostsCollection.DeleteOne(ctx, bson.M{
			"_id":       postObjID,
			"client_id": clientObjID,
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
	}
}

// handleGetInstagramPostsConfig returns Instagram posts configuration for the authenticated client
func handleGetInstagramPostsConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&client)
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
	}
}

// handleUpdateInstagramPostsConfig updates Instagram posts configuration for the authenticated client
func handleUpdateInstagramPostsConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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
			bson.M{"_id": clientObjID},
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message":                  "Instagram posts configuration updated successfully",
				"instagram_posts_enabled":  request.InstagramPostsEnabled,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":                  "Instagram posts configuration updated successfully",
			"instagram_posts_enabled":  updatedClient.InstagramPostsEnabled,
		})
	}
}

// handlePublicInstagramPosts returns all Instagram posts for a specific client (public endpoint for embed widget)
func handlePublicInstagramPosts(instagramPostsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		cursor, err := instagramPostsCollection.Find(ctx, bson.M{"client_id": clientOID})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to fetch Instagram posts",
			})
			return
		}
		defer cursor.Close(ctx)

		var posts []models.InstagramPost
		if err = cursor.All(ctx, &posts); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "database_error",
				"message":    "Failed to decode Instagram posts",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"posts": posts,
			"count": len(posts),
		})
	}
}

// handlePublicInstagramPostsConfig returns Instagram posts configuration for a specific client (public endpoint for embed widget)
func handlePublicInstagramPostsConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientOID}).Decode(&client)
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
	}
}

// handleGetWebsiteEmbedConfig returns website embed configuration for the authenticated client
func handleGetWebsiteEmbedConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&client)
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
	}
}

// handleUpdateWebsiteEmbedConfig updates website embed configuration for the authenticated client
func handleUpdateWebsiteEmbedConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
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

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
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
			bson.M{"_id": clientObjID},
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

		// Fetch updated client to return
		var updatedClient models.Client
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientObjID}).Decode(&updatedClient)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"message":              "Website embed configuration updated successfully",
				"website_embed_enabled": request.WebsiteEmbedEnabled,
				"website_embed_url":     request.WebsiteEmbedURL,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":              "Website embed configuration updated successfully",
			"website_embed_enabled": updatedClient.WebsiteEmbedEnabled,
			"website_embed_url":     updatedClient.WebsiteEmbedURL,
		})
	}
}

// handlePublicWebsiteEmbedConfig returns website embed configuration for a specific client (public endpoint for embed widget)
func handlePublicWebsiteEmbedConfig(clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
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
		err = clientsCollection.FindOne(ctx, bson.M{"_id": clientOID}).Decode(&client)
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
	}
}

// ==========================
// EMAIL TEMPLATE HANDLERS
// ==========================

// handleGetEmailTemplates returns all email templates for the authenticated client
func handleGetEmailTemplates(emailTemplatesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		cursor, err := emailTemplatesCollection.Find(ctx, bson.M{"client_id": clientObjID})
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
	}
}

// handleGetEmailTemplateByType returns an email template by type for the authenticated client
func handleGetEmailTemplateByType(emailTemplatesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
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

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		var template models.EmailTemplate
		err = emailTemplatesCollection.FindOne(ctx, bson.M{
			"client_id": clientObjID,
			"type":      templateType,
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
	}
}

// handleCreateEmailTemplate creates a new email template
func handleCreateEmailTemplate(emailTemplatesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
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

		// Check if template with same type already exists
		var existingTemplate models.EmailTemplate
		err = emailTemplatesCollection.FindOne(ctx, bson.M{
			"client_id": clientObjID,
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
			ClientID:       clientObjID,
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
	}
}

// handleUpdateEmailTemplate updates an existing email template
func handleUpdateEmailTemplate(emailTemplatesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		templateIDHex := c.Param("id")
		templateObjID, err := primitive.ObjectIDFromHex(templateIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_id",
				"message":    "Invalid template ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
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

		// Verify template belongs to client
		var existingTemplate models.EmailTemplate
		err = emailTemplatesCollection.FindOne(ctx, bson.M{
			"_id":       templateObjID,
			"client_id": clientObjID,
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
			"client_id": clientObjID,
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

		if result.ModifiedCount == 0 {
			// No changes were made, but template exists - return existing template
			var existingTemplate models.EmailTemplate
			err = emailTemplatesCollection.FindOne(ctx, bson.M{"_id": templateObjID}).Decode(&existingTemplate)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{
					"error_code": "database_error",
					"message":    "Failed to fetch updated template",
					"details":    err.Error(),
				})
				return
			}
			c.JSON(http.StatusOK, existingTemplate)
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
	}
}

// handleDeleteEmailTemplate deletes an email template
func handleDeleteEmailTemplate(emailTemplatesCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		userClientID := middleware.GetClientID(c)
		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		templateIDHex := c.Param("id")
		templateObjID, err := primitive.ObjectIDFromHex(templateIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_id",
				"message":    "Invalid template ID format",
			})
			return
		}

		clientObjID, err := primitive.ObjectIDFromHex(userClientID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		result, err := emailTemplatesCollection.DeleteOne(ctx, bson.M{
			"_id":       templateObjID,
			"client_id": clientObjID,
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
	}
}

// handlePublicQuote handles quote/proposal requests from embedded widgets
func handlePublicQuote(cfg *config.Config, clientsCollection *mongo.Collection) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIDHex := c.Param("client_id")
		clientOID, err := primitive.ObjectIDFromHex(clientIDHex)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_client_id",
				"message":    "Invalid client ID format",
			})
			return
		}

		// Parse request body
		var req struct {
			CompanyName        string `json:"company_name"`
			CompanyDescription string `json:"company_description"`
			ClientEmail        string `json:"client_email"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_request",
				"message":    "Invalid request body",
				"details":    err.Error(),
			})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		// Get client configuration
		clientDoc, err := getClientConfig(ctx, clientsCollection, clientOID)
		if err != nil {
			handleClientError(c, err)
			return
		}

		// Validate embedding permissions
		if !clientDoc.Branding.AllowEmbedding {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "embedding_not_allowed",
				"message":    "Embedding not allowed for this client",
			})
			return
		}

		// Initialize email sender
		emailSender := services.NewSMTPEmailSender(*cfg)

		// Prepare email content
		companyName := req.CompanyName
		if companyName == "" {
			companyName = clientDoc.Name
		}
		companyDescription := req.CompanyDescription
		if companyDescription == "" {
			companyDescription = "No description provided"
		}

		// Email recipients
		recipients := []string{}
		if clientDoc.ContactEmail != "" {
			recipients = append(recipients, clientDoc.ContactEmail)
		}
		for _, adminEmail := range cfg.AdminEmails {
			if strings.TrimSpace(adminEmail) != "" {
				recipients = append(recipients, strings.TrimSpace(adminEmail))
			}
		}

		if len(recipients) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "no_recipients",
				"message":    "No email recipients configured",
			})
			return
		}

		// ========== STEP 1: Send Email to Visitor with Quote Information ==========
		if req.ClientEmail != "" {
			// Try to fetch email template from database
			db := clientsCollection.Database()
			emailTemplatesCollection := db.Collection("email_templates")

			var emailTemplate models.EmailTemplate
			err = emailTemplatesCollection.FindOne(ctx, bson.M{
				"client_id": clientOID,
				"type":      "quote_visitor",
				"is_active": true,
			}).Decode(&emailTemplate)

			// Only use dynamic template from database - no hardcoded fallback
			if err == nil {
				// Use template from database
				tf := emailTemplate.TemplateFields

				// Override with request values if provided
				if req.CompanyName != "" {
					tf.CompanyName = req.CompanyName
				}
				if req.CompanyDescription != "" {
					tf.CompanyDescription = req.CompanyDescription
				}

				// Generate subject from template
				visitorSubject := strings.ReplaceAll(emailTemplate.Subject, "{{companyName}}", tf.CompanyName)
				if tf.CompanyName == "" {
					visitorSubject = strings.ReplaceAll(visitorSubject, "{{companyName}}", companyName)
				}

				// Generate HTML body from template
				visitorHTMLBody := generateQuoteEmailHTML(tf)

				// Generate text body from template
				visitorTextBody := generateQuoteEmailText(tf)

				// Send email to visitor asynchronously
				go func() {
					if err := emailSender.SendEmail([]string{req.ClientEmail}, visitorSubject, visitorHTMLBody, visitorTextBody); err != nil {
						fmt.Printf("Failed to send visitor email: %v\n", err)
					} else {
						fmt.Printf("Quote visitor email sent successfully to %s\n", req.ClientEmail)
					}
				}()
			} else {
				// No template found - log error but don't fail the request
				fmt.Printf("Email template not found for client %s (type: quote_visitor). Please configure email template in dashboard.\n", clientOID.Hex())
				// Don't send email if template is not configured
			}
		}

		// ========== STEP 2: Send Notification Email to Company ==========
		companySubject := fmt.Sprintf("New Quote Request - %s", companyName)

		companyHTMLBody := fmt.Sprintf(`
		<html>
		<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333;">
			<div style="max-width: 600px; margin: 0 auto; padding: 20px;">
				<h2 style="color: #3B82F6;">New Quote Request</h2>
				<p><strong>Company Name:</strong> %s</p>
				<p><strong>Description:</strong></p>
				<p style="background: #f5f5f5; padding: 15px; border-radius: 5px;">%s</p>
				<p><strong>Requested By:</strong> %s</p>
				<p><strong>Timestamp:</strong> %s</p>
			</div>
		</body>
		</html>
		`, companyName, companyDescription, req.ClientEmail, time.Now().Format("2006-01-02 15:04:05"))

		companyTextBody := fmt.Sprintf(`
New Quote Request

Company Name: %s

Description:
%s

Requested By: %s
Timestamp: %s
		`, companyName, companyDescription, req.ClientEmail, time.Now().Format("2006-01-02 15:04:05"))

		// Send email to company asynchronously (don't block the request)
		go func() {
			if err := emailSender.SendEmail(recipients, companySubject, companyHTMLBody, companyTextBody); err != nil {
				fmt.Printf("Failed to send quote proposal email: %v\n", err)
				// Log error but don't fail the request
			} else {
				fmt.Printf("Quote proposal email sent successfully to %v\n", recipients)
			}
		}()

		// Return success response immediately (email is sent in background)
		c.JSON(http.StatusOK, gin.H{
			"success":   true,
			"message":   "Proposal request received. You will be contacted shortly.",
			"timestamp": time.Now().Unix(),
		})
	}
}

// ==========================
// EMAIL TEMPLATE HELPERS
// ==========================

// generateQuoteEmailHTML generates HTML email body from template fields
// Only includes sections with actual data - no hardcoded fallback values
func generateQuoteEmailHTML(tf models.EmailTemplateFields) string {
	var html strings.Builder
	
	html.WriteString(`<html>
<head>
	<meta charset="UTF-8">
</head>
<body style="font-family: Arial, sans-serif; line-height: 1.8; color: #333; margin: 0; padding: 0; background-color: #f5f5f5;">
	<div style="max-width: 600px; margin: 0 auto; padding: 20px; background-color: #ffffff;">`)
	
	// Company Name Header - only if provided
	if tf.CompanyName != "" {
		html.WriteString(fmt.Sprintf(`
		<div style="border-bottom: 3px solid #3B82F6; padding-bottom: 20px; margin-bottom: 30px;">
			<h1 style="color: #3B82F6; margin: 0; font-size: 24px;">%s</h1>
		</div>`, tf.CompanyName))
	}
	
	// Greeting Message - only if provided
	if tf.GreetingMessage != "" {
		html.WriteString(fmt.Sprintf(`<p style="font-size: 16px; margin-bottom: 20px;">%s</p>`, tf.GreetingMessage))
	}
	
	// Service Introduction - only if provided
	if tf.ServiceIntroduction != "" {
		html.WriteString(fmt.Sprintf(`<p style="font-size: 16px; margin-bottom: 20px;">%s</p>`, tf.ServiceIntroduction))
	}
	
	// Services Section - only if at least one field is provided
	hasServiceContent := tf.ServiceBenefits != "" || tf.FreePanelMessage != "" || tf.RetailRateMessage != ""
	if hasServiceContent && tf.CompanyName != "" {
		html.WriteString(fmt.Sprintf(`
		<div style="background-color: #f8f9fa; padding: 20px; border-radius: 8px; margin: 20px 0;">
			<h2 style="color: #1f2937; font-size: 20px; margin-top: 0;">Why %s's WhatsApp Services?</h2>`, tf.CompanyName))
		
		if tf.ServiceBenefits != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 15px; margin-bottom: 15px;">%s</p>`, tf.ServiceBenefits))
		}
		if tf.FreePanelMessage != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 15px; margin-bottom: 15px;">%s</p>`, tf.FreePanelMessage))
		}
		if tf.RetailRateMessage != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 15px;"><strong>%s</strong></p>`, tf.RetailRateMessage))
		}
		html.WriteString(`</div>`)
	}
	
	// Pricing Plans - only if there are plans with data
	var pricingPlansHTML strings.Builder
	for _, plan := range tf.PricingPlans {
		if plan.Title != "" && plan.Price != "" {
			pricingPlansHTML.WriteString(fmt.Sprintf(`
				<li style="padding: 12px; margin-bottom: 10px; background-color: #f8f9fa; border-left: 4px solid #3B82F6;">
					<strong>%s</strong><br>
					%s %s
				</li>`, plan.Title, plan.Price, plan.Rate))
		}
	}
	if pricingPlansHTML.Len() > 0 {
		html.WriteString(`
		<div style="margin: 25px 0;">
			<h3 style="color: #1f2937; font-size: 18px; margin-bottom: 15px;">WhatsApp Marketing Plans:-</h3>
			<ul style="list-style-type: none; padding: 0; margin: 0;">`)
		html.WriteString(pricingPlansHTML.String())
		html.WriteString(`</ul>`)
		
		// Special discount message - only if provided
		if tf.SpecialDiscountMessage != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin-top: 15px; color: #6b7280;">%s</p>`, tf.SpecialDiscountMessage))
		}
		html.WriteString(`</div>`)
	}
	
	// How It Works Section - only if title or features exist
	var featuresHTML strings.Builder
	for _, feature := range tf.HowItWorksFeatures {
		if strings.TrimSpace(feature) != "" {
			featuresHTML.WriteString(fmt.Sprintf(`<li style="margin-bottom: 10px; font-size: 15px;">%s</li>`, feature))
		}
	}
	if tf.HowItWorksTitle != "" || featuresHTML.Len() > 0 {
		html.WriteString(`
		<div style="background-color: #e0f2fe; padding: 20px; border-radius: 8px; margin: 25px 0;">`)
		if tf.HowItWorksTitle != "" {
			html.WriteString(fmt.Sprintf(`<h3 style="color: #1f2937; font-size: 18px; margin-top: 0;">%s</h3>`, tf.HowItWorksTitle))
		}
		if featuresHTML.Len() > 0 {
			html.WriteString(`<ul style="padding-left: 20px; margin: 0;">`)
			html.WriteString(featuresHTML.String())
			html.WriteString(`</ul>`)
		}
		html.WriteString(`</div>`)
	}
	
	// Demo Section - only if at least one demo field is provided
	hasDemoFields := tf.DemoTitle != "" || tf.DemoDescription != "" || tf.DemoURL != "" || tf.DemoUsername != "" || tf.DemoPassword != ""
	if hasDemoFields {
		html.WriteString(`
		<div style="background-color: #fff7ed; padding: 20px; border-radius: 8px; margin: 25px 0; border-left: 4px solid #f59e0b;">`)
		if tf.DemoTitle != "" {
			html.WriteString(fmt.Sprintf(`<h3 style="color: #1f2937; font-size: 18px; margin-top: 0;">%s</h3>`, tf.DemoTitle))
		}
		if tf.DemoDescription != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin-bottom: 10px;"><strong>%s</strong></p>`, tf.DemoDescription))
		}
		if tf.DemoURL != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 5px 0;"><strong>Login url:</strong> <a href="%s" style="color: #3B82F6; text-decoration: none;">%s</a></p>`, tf.DemoURL, tf.DemoURL))
		}
		if tf.DemoUsername != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 5px 0;"><strong>Username:</strong> %s</p>`, tf.DemoUsername))
		}
		if tf.DemoPassword != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 5px 0;"><strong>Password:</strong> %s</p>`, tf.DemoPassword))
		}
		html.WriteString(`</div>`)
	}
	
	// Links Section - only if at least one link is provided
	hasLinks := tf.CompanyProfileURL != "" || tf.ClientListURL != "" || tf.FAQsURL != ""
	if hasLinks {
		html.WriteString(`<div style="margin: 25px 0;">`)
		if tf.CompanyProfileURL != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 8px 0;"><strong>Company Profile & Presentation:</strong> <a href="%s" style="color: #3B82F6; text-decoration: none;">%s</a></p>`, tf.CompanyProfileURL, tf.CompanyProfileURL))
		}
		if tf.ClientListURL != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 8px 0;"><strong>Client List:</strong> <a href="%s" style="color: #3B82F6; text-decoration: none;">%s</a></p>`, tf.ClientListURL, tf.ClientListURL))
		}
		if tf.FAQsURL != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 8px 0;"><strong>Frequently Asked Questions:</strong> <a href="%s" style="color: #3B82F6; text-decoration: none;">%s</a></p>`, tf.FAQsURL, tf.FAQsURL))
		}
		html.WriteString(`</div>`)
	}
	
	// CTA Section - only if both title and message are provided
	if tf.CTATitle != "" && tf.CTAMessage != "" {
		html.WriteString(fmt.Sprintf(`
		<div style="background-color: #f0fdf4; padding: 20px; border-radius: 8px; margin: 25px 0; text-align: center; border: 2px solid #22c55e;">
			<p style="font-size: 16px; font-weight: bold; color: #166534; margin: 0;">%s</p>
			<p style="font-size: 18px; font-weight: bold; color: #166534; margin: 15px 0 0 0;">%s</p>
		</div>`, tf.CTATitle, tf.CTAMessage))
	}
	
	// Footer - only if at least one footer field is provided
	hasFooter := tf.FooterName != "" || tf.FooterPhone != "" || tf.FooterEmail != "" || tf.FooterWebsite != ""
	if hasFooter {
		html.WriteString(`
		<div style="margin-top: 30px; padding-top: 20px; border-top: 1px solid #e5e7eb;">
			<p style="font-size: 15px; margin: 5px 0;">Thanks & Regards,</p>`)
		if tf.FooterName != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 16px; font-weight: bold; color: #1f2937; margin: 10px 0 5px 0;">%s</p>`, tf.FooterName))
		}
		if tf.FooterPhone != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 5px 0; color: #6b7280;">%s</p>`, tf.FooterPhone))
		}
		if tf.FooterEmail != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 5px 0; color: #6b7280;">%s</p>`, tf.FooterEmail))
		}
		if tf.FooterWebsite != "" {
			html.WriteString(fmt.Sprintf(`<p style="font-size: 14px; margin: 5px 0; color: #6b7280;">💻: <a href="%s" style="color: #3B82F6; text-decoration: none;">%s</a></p>`, tf.FooterWebsite, tf.FooterWebsite))
		}
		html.WriteString(`</div>`)
	}
	
	html.WriteString(`
	</div>
</body>
</html>`)
	
	return html.String()
}

// generateQuoteEmailText generates plain text email body from template fields
// Only includes sections with actual data - no hardcoded fallback values
func generateQuoteEmailText(tf models.EmailTemplateFields) string {
	var text strings.Builder
	
	// Company Name - only if provided
	if tf.CompanyName != "" {
		text.WriteString(fmt.Sprintf("%s\n\n", tf.CompanyName))
	}
	
	// Greeting Message - only if provided
	if tf.GreetingMessage != "" {
		text.WriteString(fmt.Sprintf("%s\n\n", tf.GreetingMessage))
	}
	
	// Service Introduction - only if provided
	if tf.ServiceIntroduction != "" {
		text.WriteString(fmt.Sprintf("%s\n\n", tf.ServiceIntroduction))
	}
	
	// Services Section - only if at least one field is provided
	hasServiceContent := tf.ServiceBenefits != "" || tf.FreePanelMessage != "" || tf.RetailRateMessage != ""
	if hasServiceContent && tf.CompanyName != "" {
		text.WriteString(fmt.Sprintf("Why %s's WhatsApp Services?\n\n", tf.CompanyName))
		if tf.ServiceBenefits != "" {
			text.WriteString(fmt.Sprintf("%s\n\n", tf.ServiceBenefits))
		}
		if tf.FreePanelMessage != "" {
			text.WriteString(fmt.Sprintf("%s\n\n", tf.FreePanelMessage))
		}
		if tf.RetailRateMessage != "" {
			text.WriteString(fmt.Sprintf("%s\n\n", tf.RetailRateMessage))
		}
	}
	
	// Pricing Plans - only if there are plans with data
	var pricingPlansText strings.Builder
	for _, plan := range tf.PricingPlans {
		if plan.Title != "" && plan.Price != "" {
			pricingPlansText.WriteString(fmt.Sprintf("%s, %s %s\n", plan.Title, plan.Price, plan.Rate))
		}
	}
	if pricingPlansText.Len() > 0 {
		text.WriteString("WhatsApp Marketing Plans:-\n")
		text.WriteString(pricingPlansText.String())
		if tf.SpecialDiscountMessage != "" {
			text.WriteString(fmt.Sprintf("%s\n", tf.SpecialDiscountMessage))
		}
		text.WriteString("\n")
	}
	
	// How It Works Section - only if title or features exist
	var featuresText strings.Builder
	for _, feature := range tf.HowItWorksFeatures {
		if strings.TrimSpace(feature) != "" {
			featuresText.WriteString(fmt.Sprintf("%s\n", feature))
		}
	}
	if tf.HowItWorksTitle != "" || featuresText.Len() > 0 {
		if tf.HowItWorksTitle != "" {
			text.WriteString(fmt.Sprintf("%s\n\n", tf.HowItWorksTitle))
		}
		if featuresText.Len() > 0 {
			text.WriteString(featuresText.String())
			text.WriteString("\n")
		}
	}
	
	// Demo Section - only if at least one demo field is provided
	hasDemoFields := tf.DemoTitle != "" || tf.DemoDescription != "" || tf.DemoURL != "" || tf.DemoUsername != "" || tf.DemoPassword != ""
	if hasDemoFields {
		if tf.DemoTitle != "" {
			text.WriteString(fmt.Sprintf("%s\n\n", tf.DemoTitle))
		}
		if tf.DemoDescription != "" {
			text.WriteString(fmt.Sprintf("%s\n\n", tf.DemoDescription))
		}
		if tf.DemoURL != "" {
			text.WriteString(fmt.Sprintf("Login url: %s\n", tf.DemoURL))
		}
		if tf.DemoUsername != "" {
			text.WriteString(fmt.Sprintf("Username: %s\n", tf.DemoUsername))
		}
		if tf.DemoPassword != "" {
			text.WriteString(fmt.Sprintf("Password: %s\n", tf.DemoPassword))
		}
		text.WriteString("\n")
	}
	
	// Links Section - only if at least one link is provided
	hasLinks := tf.CompanyProfileURL != "" || tf.ClientListURL != "" || tf.FAQsURL != ""
	if hasLinks {
		if tf.CompanyProfileURL != "" {
			text.WriteString(fmt.Sprintf("Company Profile & Presentation: %s\n", tf.CompanyProfileURL))
		}
		if tf.ClientListURL != "" {
			text.WriteString(fmt.Sprintf("Client List: %s\n", tf.ClientListURL))
		}
		if tf.FAQsURL != "" {
			text.WriteString(fmt.Sprintf("Frequently Asked Questions: %s\n", tf.FAQsURL))
		}
		text.WriteString("\n")
	}
	
	// CTA Section - only if both title and message are provided
	if tf.CTATitle != "" && tf.CTAMessage != "" {
		text.WriteString(fmt.Sprintf("%s\n\n%s\n\n", tf.CTATitle, tf.CTAMessage))
	}
	
	// Footer - only if at least one footer field is provided
	hasFooter := tf.FooterName != "" || tf.FooterPhone != "" || tf.FooterEmail != "" || tf.FooterWebsite != ""
	if hasFooter {
		text.WriteString("Thanks & Regards,\n")
		if tf.FooterName != "" {
			text.WriteString(fmt.Sprintf("%s\n", tf.FooterName))
		}
		if tf.FooterPhone != "" {
			text.WriteString(fmt.Sprintf("%s\n", tf.FooterPhone))
		}
		if tf.FooterEmail != "" {
			text.WriteString(fmt.Sprintf("%s\n", tf.FooterEmail))
		}
		if tf.FooterWebsite != "" {
			text.WriteString(fmt.Sprintf("💻: %s\n", tf.FooterWebsite))
		}
	}
	
	return text.String()
}

// ✅ ADDED: Response length validation functions
// validateResponseLength checks if response meets depth requirements
func validateResponseLength(responseText string, depth int) (valid bool, validatedText string, action string) {
	wordCount := countWords(responseText)
	
	// Define word count requirements by depth
	minWords, maxWords := getWordRangeForDepth(depth)
	
	validatedText = responseText
	
	if wordCount < minWords {
		return false, validatedText, "expand"
	} else if wordCount > maxWords*2 {
		// Only flag as too long if it's significantly over (2x max)
		return false, validatedText, "condense"
	}
	
	return true, validatedText, "none"
}

// countWords counts words in a text
func countWords(text string) int {
	words := strings.Fields(text)
	return len(words)
}

// getWordRangeForDepth returns min and max word count for a given depth
func getWordRangeForDepth(depth int) (minWords, maxWords int) {
	switch depth {
	case 1:
		return 40, 80
	case 2:
		return 100, 180
	case 3:
		return 180, 300
	default:
		return 60, 120
	}
}

// getMaxWordsForDepth returns maximum word count for a given depth
func getMaxWordsForDepth(depth int) int {
	_, maxWords := getWordRangeForDepth(depth)
	return maxWords
}

// ✅ ADDED: Performance metrics storage
// storePerformanceMetrics stores performance metrics in database
func storePerformanceMetrics(db *mongo.Database, clientID primitive.ObjectID, sessionID string, 
	phases models.PhaseTimings, totalTimeMs int, tokenCount int, status string, errorMessage string, 
	messageLength int, responseLength int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	metricsCollection := db.Collection("performance_metrics")
	
	metric := models.PerformanceMetrics{
		ID:            primitive.NewObjectID(),
		Timestamp:     time.Now(),
		ClientID:      clientID,
		SessionID:     sessionID,
		TotalTimeMs:   totalTimeMs,
		Phases:        phases,
		TokenCount:    tokenCount,
		Status:        status,
		ErrorMessage:  errorMessage,
		MessageLength: messageLength,
		ResponseLength: responseLength,
	}
	
	_, err := metricsCollection.InsertOne(ctx, metric)
	if err != nil {
		fmt.Printf("Warning: Failed to store performance metrics: %v\n", err)
	}
}

// ✅ ADDED: User-friendly error mapping
// UserFriendlyError represents a user-friendly error message
type UserFriendlyError struct {
	UserMessage string
	Technical   string
	Action      string
}

// mapToUserFriendlyError maps technical errors to user-friendly messages
func mapToUserFriendlyError(err error, context string) UserFriendlyError {
	errorStr := err.Error()
	errorLower := strings.ToLower(errorStr)
	
	// Network/timeout errors
	if strings.Contains(errorLower, "context deadline exceeded") || 
		strings.Contains(errorLower, "timeout") ||
		strings.Contains(errorLower, "deadline") {
		return UserFriendlyError{
			UserMessage: "I'm taking a bit longer than usual. This might be because:\n• Your question requires more context\n• Our servers are processing many requests\n\n💡 What you can do:\n• Wait a moment and try again\n• Rephrase your question to be more specific\n• Break complex questions into smaller parts",
			Technical:   errorStr,
			Action:      "retry",
		}
	}
	
	// Rate limit errors
	if strings.Contains(errorLower, "rate limit") || 
		strings.Contains(errorLower, "too many requests") ||
		strings.Contains(errorLower, "quota exceeded") {
		return UserFriendlyError{
			UserMessage: "We're experiencing high traffic right now. Please wait a moment and try again.",
			Technical:   errorStr,
			Action:      "wait_retry",
		}
	}
	
	// Token limit errors
	if strings.Contains(errorLower, "token limit") || 
		strings.Contains(errorLower, "context length") ||
		strings.Contains(errorLower, "too long") {
		return UserFriendlyError{
			UserMessage: "Your question is too complex or too long. Please break it into smaller questions.",
			Technical:   errorStr,
			Action:      "simplify",
		}
	}
	
	// AI generation errors
	if strings.Contains(errorLower, "generation failed") || 
		strings.Contains(errorLower, "ai") ||
		strings.Contains(errorLower, "model") {
		return UserFriendlyError{
			UserMessage: "I'm having trouble processing that request. Please try rephrasing your question.",
			Technical:   errorStr,
			Action:      "rephrase",
		}
	}
	
	// Insufficient context errors
	if strings.Contains(errorLower, "insufficient context") || 
		strings.Contains(errorLower, "no context") ||
		strings.Contains(errorLower, "not enough") {
		return UserFriendlyError{
			UserMessage: "I don't have enough information to answer that question. Could you provide more details?",
			Technical:   errorStr,
			Action:      "provide_details",
		}
	}
	
	// Generic error fallback
	return UserFriendlyError{
		UserMessage: fmt.Sprintf("Something went wrong. %s Please try again.", context),
		Technical:   errorStr,
		Action:      "retry",
	}
}
