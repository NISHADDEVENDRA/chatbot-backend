package routes

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"

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
	crawlsCollection := db.Collection("crawls")
	usersCollection := db.Collection("users")

	// ✅ MAIN CHAT ENDPOINT - Integrating with Client.go AI system
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

		// Get user info from JWT token
		userID := middleware.GetUserID(c)
		userClientID := middleware.GetClientID(c)

		if userID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "User ID required",
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

		// Get user details from database
		var user models.User
		err = usersCollection.FindOne(context.Background(),
			bson.M{"_id": userObjID}).Decode(&user)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error_code": "user_not_found",
				"message":    "User not found",
			})
			return
		}

		// ✅ DETERMINE CLIENT ID based on user role
		var targetClientID primitive.ObjectID

		if user.Role == "client" {
			// For client users, use their own client_id (must exist)
			if userClientID == "" {
				c.JSON(http.StatusForbidden, gin.H{
					"error_code": "missing_client_id",
					"message":    "Client users must have client_id",
				})
				return
			}
			targetClientID, err = primitive.ObjectIDFromHex(userClientID)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{
					"error_code": "invalid_client_id",
					"message":    "Invalid client ID format",
				})
				return
			}
		} else {
			// For admin/visitor users, allow them to chat using any client's system
			if userClientID != "" {
				// If client_id provided, use that
				targetClientID, err = primitive.ObjectIDFromHex(userClientID)
				if err != nil {
					targetClientID = getDefaultClientID(clientsCollection)
				}
			} else {
				// Use first available client as default
				targetClientID = getDefaultClientID(clientsCollection)
			}
		}

		// Get client configuration using Client.go function
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		clientDoc, err := getClientConfig(ctx, clientsCollection, targetClientID)

		if err != nil {
			handleClientError(c, err)
			return
		}

		// ✅ CHECK CLIENT STATUS - If inactive or suspended, block chat
		if clientDoc.Status == "inactive" || clientDoc.Status == "suspended" {
			c.JSON(http.StatusForbidden, gin.H{
				"error_code": "client_inactive",
				"message":    fmt.Sprintf("Client account '%s' is not active. Status: %s", clientDoc.Name, clientDoc.Status),
			})
			return
		}

		// ✅ CHECK TOKEN BUDGET
		if clientDoc.TokenUsed >= clientDoc.TokenLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error_code": "token_limit_exceeded",
				"message": fmt.Sprintf("Token limit exceeded for %s. Used: %d, Limit: %d",
					clientDoc.Name, clientDoc.TokenUsed, clientDoc.TokenLimit),
				"details": gin.H{
					"client_name": clientDoc.Name,
					"used":        clientDoc.TokenUsed,
					"limit":       clientDoc.TokenLimit,
					"user_role":   user.Role,
				},
			})
			return
		}

		// ✅ GENERATE CONVERSATION ID with role info
		conversationID := req.ConversationID
		if conversationID == "" {
			conversationID = fmt.Sprintf("%s_%s_%s",
				targetClientID.Hex(), user.Role, uuid.New().String())
		}

		// ✅ USE AI SYSTEM from Client.go - generateAIResponseWithMemory
		aiResponse, tokenCost, latency, err := generateAIResponseWithMemory(
			ctx, cfg, db, pdfsCollection, messagesCollection, crawlsCollection, clientDoc, req.Message, conversationID)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error_code": "ai_generation_error",
				"message":    "Failed to generate AI response",
				"details":    err.Error(),
			})
			return
		}

		// ✅ VALIDATE TOKEN BUDGET with actual cost
		if clientDoc.TokenUsed+tokenCost > clientDoc.TokenLimit {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error_code":       "insufficient_tokens",
				"message":          fmt.Sprintf("Insufficient tokens for %s", clientDoc.Name),
				"required_tokens":  tokenCost,
				"available_tokens": clientDoc.TokenLimit - clientDoc.TokenUsed,
			})
			return
		}

		// ✅ SAVE MESSAGE with full user details
		message := models.Message{
			ID:             primitive.NewObjectID(),
			FromUserID:     userObjID,
			FromName:       user.Username,
			Message:        req.Message,
			Reply:          aiResponse,
			Timestamp:      time.Now(),
			ClientID:       targetClientID,
			ConversationID: conversationID,
			TokenCost:      tokenCost,
			UserName:       user.Username, // ✅ Store username
			UserEmail:      user.Email,    // ✅ Store email
		}

		_, err = messagesCollection.InsertOne(context.Background(), message)
		if err != nil {
			// Log error but continue - AI response was successful
			fmt.Printf("Failed to save message: %v\n", err)
		}

		if err := updateTokenUsage(ctx, clientsCollection, targetClientID, clientDoc.TokenLimit, tokenCost); err != nil {
			c.JSON(http.StatusPaymentRequired, gin.H{
				"error": map[string]interface{}{
					"code":    "token_update_failed",
					"message": "Failed to update token usage",
				},
			})
			return
		}

		// TRIGGER REAL-TIME ALERT EVALUATION
		// go func() {
		//     alertCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		//     defer cancel()

		//	    // Get alert evaluator from global service (implement service registry)
		//	    if alertEvaluator := getAlertEvaluator(); alertEvaluator != nil {
		//	        if err := alertEvaluator.EvaluateAndNotify(alertCtx, targetClientID); err != nil {
		//	            log.Printf("Failed to evaluate alerts for client %s: %v", targetClientID.Hex(), err)
		//	        }
		//	    }
		//	}()

		// Calculate remaining tokens AFTER database update
		remainingTokens := clientDoc.TokenLimit - (clientDoc.TokenUsed + tokenCost)
		if remainingTokens < 0 {
			remainingTokens = 0
		}

		// ✅ RETURN COMPREHENSIVE RESPONSE
		chatResponse := models.ChatResponse{
			Reply:           aiResponse,
			TokensUsed:      tokenCost,
			RemainingTokens: remainingTokens,
			ConversationID:  conversationID,
			Timestamp:       time.Now(),
		}

		c.JSON(http.StatusOK, gin.H{
			"reply":            chatResponse.Reply,
			"tokens_used":      chatResponse.TokensUsed,
			"remaining_tokens": chatResponse.RemainingTokens,
			"conversation_id":  chatResponse.ConversationID,
			"timestamp":        chatResponse.Timestamp,
			"latency_ms":       int(latency.Milliseconds()), // ✅ Using latency
			"client_info": gin.H{
				"client_id":   targetClientID.Hex(),
				"client_name": clientDoc.Name,
				"user_role":   user.Role,
				"user_name":   user.Username,
			},
		})
	})

	// ✅ GET CONVERSATION HISTORY
	chat.GET("/conversations/:conversation_id", func(c *gin.Context) {
		conversationID := c.Param("conversation_id")
		userID := middleware.GetUserID(c)

		if userID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "User ID required",
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

		// Get messages for the conversation (user's own messages)
		cursor, err := messagesCollection.Find(
			context.Background(),
			bson.M{
				"conversation_id": conversationID,
				"from_user_id":    userObjID,
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

	// ✅ LIST USER CONVERSATIONS
	chat.GET("/conversations", func(c *gin.Context) {
		userID := middleware.GetUserID(c)

		if userID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error_code": "unauthorized",
				"message":    "User ID required",
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

		// Get distinct conversation IDs for the user with aggregation
		pipeline := mongo.Pipeline{
			{{"$match", bson.D{
				{"from_user_id", userObjID},
			}}},
			{{"$group", bson.D{
				{"_id", "$conversation_id"},
				{"last_message", bson.D{{"$last", "$$ROOT"}}},
				{"message_count", bson.D{{"$sum", 1}}},
				{"total_tokens", bson.D{{"$sum", "$token_cost"}}},
				{"client_info", bson.D{{"$first", bson.D{
					{"client_id", "$client_id"},
				}}}},
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
				ClientInfo   struct {
					ClientID primitive.ObjectID `bson:"client_id"`
				} `bson:"client_info"`
			}
			if err := cursor.Decode(&result); err != nil {
				continue
			}

			// Get client name using existing function
			var client models.Client
			clientsCollection.FindOne(context.Background(),
				bson.M{"_id": result.ClientInfo.ClientID}).Decode(&client)

			conversation := gin.H{
				"conversation_id": result.ID,
				"last_message":    result.LastMessage,
				"message_count":   result.MessageCount,
				"total_tokens":    result.TotalTokens,
				"updated_at":      result.LastMessage.Timestamp,
				"client_name":     client.Name,
				"client_id":       result.ClientInfo.ClientID.Hex(),
			}

			conversations = append(conversations, conversation)
		}

		c.JSON(http.StatusOK, gin.H{
			"conversations": conversations,
			"total":         len(conversations),
		})
	})
}

// ✅ HELPER FUNCTIONS (minimal, non-duplicate)

// getDefaultClientID gets the first available client for admin/visitor users
func getDefaultClientID(collection *mongo.Collection) primitive.ObjectID {
	var client models.Client
	err := collection.FindOne(context.Background(), bson.M{}).Decode(&client)
	if err != nil {
		return primitive.NewObjectID() // Return new ID if no client found
	}
	return client.ID
}

// Helper function to get relevant context from PDFs (already exists in your current chat.go)
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
