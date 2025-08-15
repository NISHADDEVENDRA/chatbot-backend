package routes

import (
	"context"
	"net/http"
	"strconv"
	"time"
	"fmt"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/middleware"
	"saas-chatbot-platform/models"
	"saas-chatbot-platform/utils"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
        "message": "Client tokens successfully reset",
        "client_id": clientID.Hex(),
        "old_limit": client.TokenLimit,
        "new_limit": req.NewTokenLimit,
        "reason":    req.Reason,
        "reset_at":  time.Now(),
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

			user := models.User{
				Username:     req.InitialUser.Username,
				Name:         req.InitialUser.Name,
				Email:        req.InitialUser.Email,
				Phone:        req.InitialUser.Phone,
				PasswordHash: hashed,
				Role:         "client",
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
	admin.PUT("/client/:id", func(c *gin.Context) {
		clientID, err := primitive.ObjectIDFromHex(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error_code": "invalid_client_id", "message": "Invalid client ID format"})
			return
		}

		var req models.UpdateClientRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error_code": "invalid_input", "message": "Invalid request data", "details": gin.H{"error": err.Error()}})
			return
		}

		set := bson.M{"updated_at": time.Now()}
		if req.Name != nil {
			set["name"] = *req.Name
		}
		if req.TokenLimit != nil {
			set["token_limit"] = *req.TokenLimit
		}
		if req.Branding != nil {
			set["branding"] = *req.Branding
		}
		if req.Status != nil {
			set["status"] = *req.Status
		}
		if req.ContactEmail != nil {
			set["contact_email"] = *req.ContactEmail
		}
		if req.ContactPhone != nil {
			set["contact_phone"] = *req.ContactPhone
		}

		result, err := clientsCollection.UpdateOne(context.Background(), bson.M{"_id": clientID}, bson.M{"$set": set})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to update client"})
			return
		}
		if result.MatchedCount == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error_code": "client_not_found", "message": "Client not found"})
			return
		}

		var updated models.Client
		if err := clientsCollection.FindOne(context.Background(), bson.M{"_id": clientID}).Decode(&updated); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error_code": "internal_error", "message": "Failed to retrieve updated client"})
			return
		}
		c.JSON(http.StatusOK, updated)
	})

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
			{{"$group", bson.D{
				{"_id", nil},
				{"total_tokens", bson.D{{"$sum", "$token_used"}}},
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

		c.JSON(http.StatusOK, models.UsageAnalytics{
			TotalClients:    int(totalClients),
			TotalTokensUsed: totalTokensUsed,
			TotalMessages:   int(totalMessages),
			ActiveClients:   len(activeClients),
			ClientStats:     clientStats,
			PeriodStart:     periodStart,
			PeriodEnd:       periodEnd,
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
			{{"$group", bson.D{
				{"_id", nil},
				{"total_tokens", bson.D{{"$sum", "$token_used"}}},
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
				"clients":            totalClients,
				"users":              totalUsers,
				"pdfs":               totalPDFs,
				"messages":           totalMessages,
				"messages_last_24h":  msgLast24h,
				"total_tokens_used":  totalTokens,
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
}
