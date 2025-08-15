package routes

import (
	"context"
	"net/http"
	"strings"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func SetupChatRoutes(router *gin.Engine, cfg *config.Config, mongoClient *mongo.Client, authMiddleware *middleware.AuthMiddleware) {
	chat := router.Group("/chat")
	chat.Use(authMiddleware.RequireAuth())

	db := mongoClient.Database(cfg.DBName)
	clientsCollection := db.Collection("clients")
	messagesCollection := db.Collection("messages")
	pdfsCollection := db.Collection("pdfs")

	geminiClient := utils.NewGeminiClient(cfg.GeminiAPIKey, cfg.GeminiAPIURL)

	// Process chat messages with AI integration
	chat.POST("/send", func(c *gin.Context) {
		var req models.ChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_input",
				"message":    "Invalid request data",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		// Get user info from context
		userID := middleware.GetUserID(c)
		userClientID := middleware.GetClientID(c)

		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required for chat",
			})
			return
		}

		userObjID, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
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

		// Get client to check token limits
		var clientDoc models.Client
		err = clientsCollection.FindOne(context.Background(), bson.M{"_id": clientObjID}).Decode(&clientDoc)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "client_not_found",
				"message":    "Client not found",
			})
			return
		}

		// Check token limits
		estimatedTokens := geminiClient.CalculateTokens(req.Message)
		if clientDoc.TokenUsed+estimatedTokens > clientDoc.TokenLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error_code": "token_limit_exceeded",
				"message":    "Token limit exceeded",
				"details": gin.H{
					"used":     clientDoc.TokenUsed,
					"limit":    clientDoc.TokenLimit,
					"required": estimatedTokens,
				},
			})
			return
		}

		// Generate conversation ID if not provided
		conversationID := req.ConversationID
		if conversationID == "" {
			conversationID = uuid.New().String()
		}

		// Get relevant context from PDFs
		relevantContext, err := getRelevantContext(pdfsCollection, clientObjID, req.Message, 3)
		if err != nil {
			// Log error but continue without context
			relevantContext = []string{}
		}

		// Call Gemini API
		response, err := geminiClient.AskGemini(req.Message, relevantContext, 2)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "ai_service_error",
				"message":    "Failed to get AI response",
				"details":    gin.H{"error": err.Error()},
			})
			return
		}

		reply := geminiClient.ExtractResponseText(response)
		actualTokens := geminiClient.CalculateTokens(req.Message + reply)

		// Update client token usage atomically
		filter := bson.M{
			"_id":        clientObjID,
			"token_used": bson.M{"$lte": clientDoc.TokenLimit - actualTokens},
		}
		update := bson.M{
			"$inc": bson.M{"token_used": actualTokens},
			"$set": bson.M{"updated_at": time.Now()},
		}

		result, err := clientsCollection.UpdateOne(context.Background(), filter, update)
		if err != nil || result.MatchedCount == 0 {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error_code": "token_limit_exceeded",
				"message":    "Token limit exceeded during processing",
			})
			return
		}

		// Store message
		message := models.Message{
			FromUserID:     userObjID,
			FromName:       "", // You might want to get this from user doc
			Message:        req.Message,
			Reply:          reply,
			Timestamp:      time.Now(),
			ClientID:       clientObjID,
			ConversationID: conversationID,
			TokenCost:      actualTokens,
		}

		_, err = messagesCollection.InsertOne(context.Background(), message)
		if err != nil {
			// Log error but don't fail the request since AI response was successful
		}

		// Calculate remaining tokens
		remainingTokens := clientDoc.TokenLimit - (clientDoc.TokenUsed + actualTokens)
		if remainingTokens < 0 {
			remainingTokens = 0
		}

		chatResponse := models.ChatResponse{
			Reply:           reply,
			TokensUsed:      actualTokens,
			RemainingTokens: remainingTokens,
			ConversationID:  conversationID,
			Timestamp:       time.Now(),
		}

		c.JSON(http.StatusOK, chatResponse)
	})

	// Get conversation history
	chat.GET("/conversations/:conversation_id", func(c *gin.Context) {
		conversationID := c.Param("conversation_id")
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

		// Get messages for the conversation
		cursor, err := messagesCollection.Find(
			context.Background(),
			bson.M{
				"conversation_id": conversationID,
				"client_id":       clientObjID,
			},
			options.Find().SetSort(bson.M{"timestamp": 1}),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve conversation",
			})
			return
		}
		defer cursor.Close(context.Background())

		messages := make([]models.Message, 0)
		if err := cursor.All(context.Background(), &messages); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
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

		conversation := models.ConversationHistory{
			ConversationID: conversationID,
			Messages:       messages,
			TotalTokens:    totalTokens,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		}

		c.JSON(http.StatusOK, conversation)
	})

	// List user conversations
	chat.GET("/conversations", func(c *gin.Context) {
		userID := middleware.GetUserID(c)
		userClientID := middleware.GetClientID(c)

		if userClientID == "" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "forbidden",
				"message":    "Client ID required",
			})
			return
		}

		userObjID, err := primitive.ObjectIDFromHex(userID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error_code": "invalid_user_id",
				"message":    "Invalid user ID format",
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

		// Get distinct conversation IDs for the user
		pipeline := mongo.Pipeline{
			{{"$match", bson.D{
				{"from_user_id", userObjID},
				{"client_id", clientObjID},
			}}},
			{{"$group", bson.D{
				{"_id", "$conversation_id"},
				{"last_message", bson.D{{"$last", "$$ROOT"}}},
				{"message_count", bson.D{{"$sum", 1}}},
				{"total_tokens", bson.D{{"$sum", "$token_cost"}}},
			}}},
			{{"$sort", bson.D{{"last_message.timestamp", -1}}}},
		}

		cursor, err := messagesCollection.Aggregate(context.Background(), pipeline)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "internal_error",
				"message":    "Failed to retrieve conversations",
			})
			return
		}
		defer cursor.Close(context.Background())

		conversations := make([]gin.H, 0)
		for cursor.Next(context.Background()) {
			var result struct {
				ID           string         `bson:"_id"`
				LastMessage  models.Message `bson:"last_message"`
				MessageCount int            `bson:"message_count"`
				TotalTokens  int            `bson:"total_tokens"`
			}
			if err := cursor.Decode(&result); err != nil {
				continue
			}

			conversation := gin.H{
				"conversation_id": result.ID,
				"last_message":    result.LastMessage,
				"message_count":   result.MessageCount,
				"total_tokens":    result.TotalTokens,
				"updated_at":      result.LastMessage.Timestamp,
			}

			conversations = append(conversations, conversation)
		}

		c.JSON(http.StatusOK, gin.H{
			"conversations": conversations,
			"total":         len(conversations),
		})
	})
}

// Helper function to get relevant context from PDFs
func getRelevantContext(pdfsCollection *mongo.Collection, clientID primitive.ObjectID, query string, maxChunks int) ([]string, error) {
	// Get all PDFs for the client
	cursor, err := pdfsCollection.Find(
		context.Background(),
		bson.M{"client_id": clientID},
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(context.Background())

	var allChunks []models.ContentChunk
	for cursor.Next(context.Background()) {
		var pdf models.PDF
		if err := cursor.Decode(&pdf); err != nil {
			continue
		}
		allChunks = append(allChunks, pdf.ContentChunks...)
	}

	if len(allChunks) == 0 {
		return []string{}, nil
	}

	// Simple relevance scoring based on keyword matching
	queryWords := strings.Fields(strings.ToLower(query))
	type scoredChunk struct {
		chunk models.ContentChunk
		score int
	}

	var scoredChunks []scoredChunk
	for _, chunk := range allChunks {
		score := 0
		chunkText := strings.ToLower(chunk.Text)

		for _, word := range queryWords {
			if strings.Contains(chunkText, word) {
				score += strings.Count(chunkText, word)
			}
		}

		if score > 0 {
			scoredChunks = append(scoredChunks, scoredChunk{
				chunk: chunk,
				score: score,
			})
		}
	}

	// Sort by score (descending)
	for i := 0; i < len(scoredChunks); i++ {
		for j := i + 1; j < len(scoredChunks); j++ {
			if scoredChunks[j].score > scoredChunks[i].score {
				scoredChunks[i], scoredChunks[j] = scoredChunks[j], scoredChunks[i]
			}
		}
	}

	// Return top chunks
	var context []string
	limit := maxChunks
	if len(scoredChunks) < limit {
		limit = len(scoredChunks)
	}

	for i := 0; i < limit; i++ {
		context = append(context, scoredChunks[i].chunk.Text)
	}

	return context, nil
}
