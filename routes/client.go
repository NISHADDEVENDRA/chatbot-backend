package routes

import (
    "bytes"
    "context"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
    "strconv"
    "strings"
    "time"

    "saas-chatbot-platform/internal/config"
    "saas-chatbot-platform/middleware"
    "saas-chatbot-platform/models"

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

    // Public routes (no authentication required)
    setupPublicRoutes(router, cfg, clientsCollection, pdfsCollection, messagesCollection)

    // Authenticated client routes
    setupAuthenticatedRoutes(client, cfg, clientsCollection, pdfsCollection, messagesCollection)
}

// setupPublicRoutes configures public endpoints for embedded widgets
func setupPublicRoutes(router *gin.Engine, cfg *config.Config, clientsCollection, pdfsCollection, messagesCollection *mongo.Collection) {
    // Public: branding for embed widget (no auth)
    router.GET("/public/branding/:client_id", handlePublicBranding(clientsCollection))

    // Public: chat endpoint for embed widget (no auth)
    router.POST("/public/chat", handlePublicChat(cfg, clientsCollection, pdfsCollection, messagesCollection))
}

// setupAuthenticatedRoutes configures routes that require authentication
func setupAuthenticatedRoutes(client *gin.RouterGroup, cfg *config.Config, clientsCollection, pdfsCollection, messagesCollection *mongo.Collection) {
    // Branding management
    client.GET("/branding", handleGetBranding(clientsCollection))
    client.POST("/branding", handleUpdateBranding(clientsCollection))

    // PDF management
    client.POST("/upload", handlePDFUpload(cfg, pdfsCollection))
    client.GET("/pdfs", handleListPDFs(pdfsCollection))

    // Token usage
    client.GET("/tokens", handleGetTokens(clientsCollection))

        // ========== ADD THESE DELETE ROUTES ==========
    client.DELETE("/pdfs/:id", handleDeletePDF(pdfsCollection))           // Single PDF delete
    client.DELETE("/pdfs/bulk", handleBulkDeletePDFs(pdfsCollection))
    // PATCH /client/pdfs/:id/status - Update PDF status
client.PATCH("/pdfs/:id/status", handleUpdatePDFStatus(pdfsCollection))
     // Bulk PDF delete

    // Analytics
    client.GET("/analytics", handleAnalytics(messagesCollection))

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
        })
    }
}

// handlePublicChat processes chat requests from embedded widgets with conversation memory
func handlePublicChat(cfg *config.Config, clientsCollection, pdfsCollection, messagesCollection *mongo.Collection) gin.HandlerFunc {
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
                "error_code": "token_limit_exceeded",
                "message":    "Token limit exceeded. Please upgrade your plan.",
                "tokens_used": clientDoc.TokenUsed,
                "token_limit": clientDoc.TokenLimit,
            })
            return
        }

        // Generate AI response with conversation memory
        response, tokenCost, latency, err := generateAIResponseWithMemory(ctx, cfg, pdfsCollection, messagesCollection, clientDoc, req.Message, req.SessionID)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{
                "error_code": "ai_generation_error",
                "message":    "Failed to generate response",
                "details":    err.Error(),
            })
            return
        }

        // Validate token budget again with actual cost
        if clientDoc.TokenUsed+tokenCost > clientDoc.TokenLimit {
            c.JSON(http.StatusPaymentRequired, gin.H{
                "error_code": "insufficient_tokens",
                "message":    "Insufficient tokens to complete this request",
                "required_tokens": tokenCost,
                "available_tokens": clientDoc.TokenLimit - clientDoc.TokenUsed,
            })
            return
        }

        // Persist conversation
        if err := persistMessage(ctx, messagesCollection, clientDoc.ID, req, response, tokenCost); err != nil {
            // Log error but don't fail the request
            fmt.Printf("Failed to persist message: %v\n", err)
        }

        // Update token usage atomically
        if err := updateTokenUsage(ctx, clientsCollection, clientDoc.ID, clientDoc.TokenLimit, tokenCost); err != nil {
            c.JSON(http.StatusPaymentRequired, gin.H{
                "error_code": "token_update_failed",
                "message":    "Failed to update token usage or insufficient tokens",
            })
            return
        }

        // Return successful response
        c.JSON(http.StatusOK, gin.H{
            "reply":           response,
            "token_cost":      tokenCost,
            "conversation_id": req.SessionID,
            "latency_ms":      int(latency.Milliseconds()),
            "timestamp":       time.Now().Unix(),
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

        c.JSON(http.StatusOK, gin.H{
            "message":  "Branding updated successfully",
            "branding": branding,
        })
    }
}

// handlePDFUpload processes PDF file uploads
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

        if err := c.Request.ParseMultipartForm(cfg.MaxFileSize); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{
                "error_code": "file_too_large",
                "message":    "File size exceeds maximum limit",
            })
            return
        }

        file, header, err := c.Request.FormFile("pdf")
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{
                "error_code": "no_file",
                "message":    "No PDF file provided",
            })
            return
        }
        defer file.Close()

        // Validate file type
        ct := header.Header.Get("Content-Type")
        if !strings.Contains(ct, "pdf") && !strings.HasSuffix(strings.ToLower(header.Filename), ".pdf") {
            c.JSON(http.StatusBadRequest, gin.H{
                "error_code": "invalid_file_type",
                "message":    "Only PDF files are allowed",
            })
            return
        }

        if header.Size > cfg.MaxFileSize {
            c.JSON(http.StatusBadRequest, gin.H{
                "error_code": "file_too_large",
                "message":    "File size exceeds maximum limit",
            })
            return
        }

        start := time.Now()
        fileContent, err := io.ReadAll(file)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{
                "error_code": "file_read_error",
                "message":    "Failed to read file",
            })
            return
        }

        // Extract text using smart extraction
        modelName := os.Getenv("GEMINI_FILE_MODEL")
        if modelName == "" {
            modelName = "gemini-2.0-flash"
        }

        text, pages, err := extractTextSmart(fileContent, header.Filename, cfg.GeminiAPIKey, modelName)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{
                "error_code": "pdf_processing_error",
                "message":    "Failed to process PDF file",
                "details":    gin.H{"error": err.Error()},
            })
            return
        }

        // Chunk text intelligently
        chunks := chunkTextSmart(text, cfg.MaxChunkSize, cfg.ChunkOverlap)

        clientObjID, err := primitive.ObjectIDFromHex(userClientID)
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{
                "error_code": "invalid_client_id",
                "message":    "Invalid client ID format",
            })
            return
        }

        pdfDoc := models.PDF{
            ClientID:      clientObjID,
            Filename:      header.Filename,
            ContentChunks: chunks,
            UploadedAt:    time.Now(),
            Metadata: models.PDFMetadata{
                Size:           header.Size,
                Pages:          pages,
                ProcessingTime: time.Since(start),
            },
        }

        ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
        defer cancel()

        result, err := pdfsCollection.InsertOne(ctx, pdfDoc)
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{
                "error_code": "internal_error",
                "message":    "Failed to save PDF",
            })
            return
        }

        c.JSON(http.StatusOK, models.UploadResponse{
            ID:         result.InsertedID.(primitive.ObjectID).Hex(),
            Filename:   header.Filename,
            ChunkCount: len(chunks),
            Metadata:   pdfDoc.Metadata,
        })
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

// generateAIResponseWithMemory generates AI response with conversation history
func generateAIResponseWithMemory(ctx context.Context, cfg *config.Config, pdfsCollection, messagesCollection *mongo.Collection, client *models.Client, message, sessionID string) (string, int, time.Duration, error) {
    // Retrieve conversation history (last 10 messages)
    conversationHistory, err := getConversationHistory(ctx, messagesCollection, client.ID, sessionID, 10)
    if err != nil {
        fmt.Printf("Warning: Failed to retrieve conversation history: %v\n", err)
    }

    // Retrieve PDF context
    contextChunks, err := retrievePDFContext(ctx, pdfsCollection, client.ID, message, 3)
    if err != nil {
        fmt.Printf("Warning: Failed to retrieve PDF context: %v\n", err)
    }

    // Build enhanced context with conversation history
    contextStr := buildContextWithHistory(contextChunks, conversationHistory)

    // Initialize Gemini client
    geminiClient, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
    if err != nil {
        return "", 0, 0, fmt.Errorf("failed to initialize Gemini client: %w", err)
    }
    defer geminiClient.Close()

    // Configure model
    model := configureGeminiModel(geminiClient)

    // Generate enhanced prompt with conversation context
    prompt := buildPromptWithHistory(client.Name, contextStr, conversationHistory, message)

    // Generate response with timing
    start := time.Now()
    resp, err := model.GenerateContent(ctx, genai.Text(prompt))
    latency := time.Since(start)

    if err != nil {
        return "", 0, latency, fmt.Errorf("generation failed: %w", err)
    }

    // Extract response text
    replyText, err := extractResponseText(resp)
    if err != nil {
        return "", 0, latency, err
    }

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
        tokenCost = estimateTokenCostWithHistory(message, replyText, len(contextChunks), len(conversationHistory))
    }

    return replyText, tokenCost, latency, nil
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

// buildContextWithHistory creates context string including conversation history
func buildContextWithHistory(chunks []models.ContentChunk, history []models.Message) string {
    var contextStr strings.Builder

    // Add conversation history if available
    if len(history) > 0 {
        contextStr.WriteString("Previous conversation context:\n")
        for _, msg := range history {
            contextStr.WriteString(fmt.Sprintf("User: %s\n", msg.Message))
            contextStr.WriteString(fmt.Sprintf("Assistant: %s\n\n", msg.Reply))
        }
        contextStr.WriteString("---\n\n")
    }

    // Add PDF context
    if len(chunks) > 0 {
        contextStr.WriteString("Relevant information from uploaded documents:\n\n")
        for i, chunk := range chunks {
            contextStr.WriteString(fmt.Sprintf("%d. %s\n\n", i+1, chunk.Text))
        }
        contextStr.WriteString("---\n\n")
    }

    return contextStr.String()
}

// buildPromptWithHistory creates AI prompt with conversation context and enhanced user guidance
// func buildPromptWithHistory(clientName, contextStr string, history []models.Message, currentMessage string) string {
//     const (
//         recapLimit = 8
//         trimLen    = 350
//     )

//     trim := func(s string) string {
//         s = strings.TrimSpace(s)
//         if len(s) <= trimLen {
//             return s
//         }
//         return s[:trimLen] + "…"
//     }

//     hasHistory := len(history) > 0
//     var prompt strings.Builder

//     // Core identity with stronger directive
//     prompt.WriteString(fmt.Sprintf("You are a knowledgeable customer service assistant for %s. ", clientName))
//     prompt.WriteString("CRITICAL: Never say 'I cannot answer' or 'I don't have information'. Always provide helpful guidance or alternatives in 1-2 lines maximum.\n\n")

//     // Enhanced core principles
//     prompt.WriteString("MANDATORY RESPONSE RULES:\n")
//     prompt.WriteString("• ALWAYS give a direct, actionable answer - never deflect\n")
//     prompt.WriteString("• For service inquiries: Describe what you likely offer based on context\n")
//     prompt.WriteString("• For contact requests: Provide specific contact methods or suggest checking website\n")
//     prompt.WriteString("• For complaints: Acknowledge and offer concrete next steps\n")
//     prompt.WriteString("• When unsure: Give your best helpful response plus 'Contact us at [method] for specifics'\n")
//     prompt.WriteString("• FORBIDDEN phrases: 'unable to answer', 'don't have information', 'cannot help'\n\n")

//     // History and context (keep your existing logic)
//     if hasHistory {
//         start := 0
//         if len(history) > recapLimit {
//             start = len(history) - recapLimit
//         }
//         prompt.WriteString("Conversation context:\n")
//         for _, msg := range history[start:] {
//             if strings.TrimSpace(msg.Message) != "" {
//                 prompt.WriteString(fmt.Sprintf("User: %s\n", trim(msg.Message)))
//             }
//             if strings.TrimSpace(msg.Reply) != "" {
//                 prompt.WriteString(fmt.Sprintf("Assistant: %s\n", trim(msg.Reply)))
//             }
//         }
//         prompt.WriteString("\n")
//     }

//     if cs := strings.TrimSpace(contextStr); cs != "" {
//         prompt.WriteString("Business information:\n")
//         prompt.WriteString(trim(cs) + "\n\n")
//     }

//     prompt.WriteString(fmt.Sprintf("User: %s\n", strings.TrimSpace(currentMessage)))
//     prompt.WriteString("Assistant: [Provide a helpful, specific answer in 1-2 lines. Be resourceful and solution-focused.]")

//     return prompt.String()
// }

func buildPromptWithHistory(clientName, contextStr string, history []models.Message, currentMessage string) string {
    hasHistory := len(history) > 0
    var prompt strings.Builder
    
    // Core identity and role - Clear and professional
    prompt.WriteString(fmt.Sprintf("You are a helpful assistant for %s. ", clientName))
    prompt.WriteString("Your primary goal is to understand user needs and provide accurate, relevant information promptly.\n\n")
    
    // Response quality standards - Focused on effectiveness
    prompt.WriteString("RESPONSE QUALITY STANDARDS:\n")
    prompt.WriteString("- UNDERSTAND the user's actual intent before responding\n")
    prompt.WriteString("- ANSWER direct questions with direct, helpful information\n")
    prompt.WriteString("- KEEP responses CONCISE but COMPLETE (1-2 lines maximum unless details needed)\n")
    prompt.WriteString("- BE DIRECT, HELPFUL, and ACTIONABLE in every response\n")
    prompt.WriteString("- PROVIDE concrete information when available instead of just asking questions\n")
    prompt.WriteString("- USE bullet points for lists or multiple items\n")
    prompt.WriteString("- ASK clarifying questions ONLY when genuinely needed to help\n")
    prompt.WriteString("- ACKNOWLEDGE user's specific situation or location when relevant\n\n")
    
    // Intent recognition and priority handling
    prompt.WriteString("INTENT RECOGNITION AND PRIORITY:\n")
    prompt.WriteString("- RECOGNIZE common requests like 'contact support,' 'help,' 'assistance' and respond appropriately\n")
    prompt.WriteString("- PRIORITIZE direct service requests over general information\n")
    prompt.WriteString("- HANDLE emergency or urgent requests with immediate attention\n")
    prompt.WriteString("- RESPOND to 'how to contact' questions with actual contact methods\n")
    prompt.WriteString("- ADDRESS complaint or frustration with empathy and solutions\n")
    prompt.WriteString("- IDENTIFY sales inquiries and provide relevant offerings\n")
    prompt.WriteString("- DISTINGUISH between information-seeking and transactional requests\n\n")
    
    // Conversation context handling - Natural and effective
    if hasHistory {
        prompt.WriteString("CONVERSATION CONTEXT MANAGEMENT:\n")
        prompt.WriteString("- MAINTAIN continuity with previous discussions naturally\n")
        prompt.WriteString("- REFERENCE earlier topics ONLY when directly relevant\n")
        prompt.WriteString("- AVOID forcing previous topics if user has moved on\n")
        prompt.WriteString("- RESPECT user's current focus and questions\n")
        prompt.WriteString("- BUILD upon established context without ignoring new requests\n")
        prompt.WriteString("- TRANSITION between topics smoothly when user changes subject\n\n")
    }
    
    // User engagement strategies - Value-focused
    prompt.WriteString("USER ENGAGEMENT PRINCIPLES:\n")
    prompt.WriteString("- START by addressing the user's immediate question or need\n")
    prompt.WriteString("- PROVIDE the information they actually asked for\n")
    prompt.WriteString("- OFFER additional help only after addressing their primary request\n")
    prompt.WriteString("- SUGGEST relevant next steps based on their actual inquiry\n")
    prompt.WriteString("- ANTICIPATE logical follow-up questions and prepare answers\n")
    prompt.WriteString("- VALIDATE user concerns and show understanding\n")
    prompt.WriteString("- MAKE responses actionable - tell users what they can do next\n\n")
    
    // Context information - Practical use
    if contextStr != "" {
        prompt.WriteString("AVAILABLE BUSINESS INFORMATION:\n")
        prompt.WriteString(contextStr)
        prompt.WriteString("\n\n")
    }
    
    // Current query with proper emphasis
    prompt.WriteString(fmt.Sprintf("USER'S CURRENT QUESTION: %s\n\n", currentMessage))
    
    // Enhanced task instructions - Clear and actionable
    prompt.WriteString("YOUR PRIMARY TASK:\n")
    prompt.WriteString("1. UNDERSTAND what the user is actually asking for\n")
    prompt.WriteString("2. PROVIDE the specific information or assistance they need\n")
    prompt.WriteString("3. BE HELPFUL, accurate, and responsive to their actual intent\n")
    prompt.WriteString("4. KEEP responses concise but complete\n\n")
    
    if hasHistory {
        prompt.WriteString("CONTEXTUAL TASK GUIDANCE:\n")
        prompt.WriteString("- Reference previous conversation ONLY when it helps answer the current question\n")
        prompt.WriteString("- Don't ignore the user's current request to talk about previous topics\n")
        prompt.WriteString("- If the user changes topics, follow their lead naturally\n")
        prompt.WriteString("- Maintain helpfulness over maintaining conversation flow\n\n")
    }
    
    // Special handling instructions - Critical improvements
    prompt.WriteString("SPECIAL HANDLING INSTRUCTIONS:\n")
    prompt.WriteString("- For 'contact support' or 'how to contact' questions: PROVIDE actual contact methods\n")
    prompt.WriteString("- For 'help' requests: IDENTIFY what specific help is needed\n")
    prompt.WriteString("- For direct questions: ANSWER directly before asking follow-ups\n")
    prompt.WriteString("- For complaints: ACKNOWLEDGE frustration and offer solutions\n")
    prompt.WriteString("- For location-specific questions: CONSIDER user's location in responses\n")
    prompt.WriteString("- For offer/package inquiries: PROVIDE specific available options\n")
    prompt.WriteString("- When uncertain: ASK one clear, specific question to clarify\n\n")
    
    // Quality assurance - Effectiveness focused
    prompt.WriteString("QUALITY CHECK BEFORE RESPONDING:\n")
    prompt.WriteString("- Does my response actually answer what the user asked?\n")
    prompt.WriteString("- Am I providing helpful information rather than just asking questions?\n")
    prompt.WriteString("- Would a user find this response useful and complete?\n")
    prompt.WriteString("- Am I addressing their current question, not redirecting to something else?\n")
    prompt.WriteString("- Have I prioritized their immediate needs over other considerations?\n\n")
    
    // Edge case handling - Robust protocols
    prompt.WriteString("EDGE CASE PROTOCOLS:\n")
    prompt.WriteString("- If you don't have specific information: ADMIT this and suggest alternatives\n")
    prompt.WriteString("- For repetitive questions: PROVIDE the information they're asking for\n")
    prompt.WriteString("- For contradictory information: CLARIFY current preferences politely\n")
    prompt.WriteString("- For frustrated users: ACKNOWLEDGE concerns and focus on solutions\n")
    prompt.WriteString("- For off-topic questions: POLITELY redirect while staying helpful\n")
    prompt.WriteString("- For unclear requests: ASK one specific, helpful clarifying question\n")
    prompt.WriteString("- When in doubt: PRIORITIZE being helpful over being perfect\n\n")
    
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

// persistMessage saves the conversation to database
func persistMessage(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, req ChatRequest, response string, tokenCost int) error {
    message := models.Message{
        FromUserID:     primitive.NilObjectID, // public user
        FromName:       "User",
        Message:        req.Message,
        Reply:          response,
        Timestamp:      time.Now(),
        ClientID:       clientID,
        ConversationID: req.SessionID,
        TokenCost:      tokenCost,
    }

    _, err := collection.InsertOne(ctx, message)
    return err
}

// updateTokenUsage atomically updates client token usage
func updateTokenUsage(ctx context.Context, collection *mongo.Collection, clientID primitive.ObjectID, tokenLimit, tokenCost int) error {
    updateResult, err := collection.UpdateOne(ctx,
        bson.M{
            "_id": clientID,
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

// configureGeminiModel sets up the Gemini model with proper configuration
func configureGeminiModel(client *genai.Client) *genai.GenerativeModel {
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
        var r []struct{ Tokens int64 `bson:"tokens"` }
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
        var r []struct{ Tokens int64 `bson:"tokens"` }
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
func retrievePDFContext(ctx context.Context, pdfsCollection *mongo.Collection, clientID primitive.ObjectID, query string, maxChunks int) ([]models.ContentChunk, error) {
    queryWords := strings.Fields(strings.ToLower(query))
    if len(queryWords) == 0 {
        return nil, nil
    }

    cursor, err := pdfsCollection.Find(ctx, bson.M{"client_id": clientID})
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var relevantChunks []models.ContentChunk
    var pdfs []models.PDF

    if err := cursor.All(ctx, &pdfs); err != nil {
        return nil, err
    }

    type scoredChunk struct {
        chunk models.ContentChunk
        score int
    }

    var scored []scoredChunk

    for _, pdf := range pdfs {
        for _, chunk := range pdf.ContentChunks {
            chunkLower := strings.ToLower(chunk.Text)
            score := 0

            for _, word := range queryWords {
                if len(word) > 2 {
                    score += strings.Count(chunkLower, word)
                }
            }

            if score > 0 {
                scored = append(scored, scoredChunk{chunk: chunk, score: score})
            }
        }
    }

    sort.Slice(scored, func(i, j int) bool {
        return scored[i].score > scored[j].score
    })

    limit := maxChunks
    if len(scored) < limit {
        limit = len(scored)
    }

    for i := 0; i < limit; i++ {
        relevantChunks = append(relevantChunks, scored[i].chunk)
    }

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

// processPDFWithGemini processes PDF file using Gemini File API
func processPDFWithGemini(filePath, apiKey, modelName string) (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
    defer cancel()

    client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
    if err != nil {
        return "", fmt.Errorf("failed to create Gemini client: %v", err)
    }
    defer client.Close()

    file, err := client.UploadFileFromPath(ctx, filePath, nil)
    if err != nil {
        return "", fmt.Errorf("failed to upload file to Gemini: %v", err)
    }

    // Wait for file processing
    maxWait := 45 * time.Second
    deadline := time.Now().Add(maxWait)

    for file.State == genai.FileStateProcessing {
        if time.Now().After(deadline) {
            return "", errors.New("file processing timeout")
        }
        time.Sleep(2 * time.Second)
        file, err = client.GetFile(ctx, file.Name)
        if err != nil {
            return "", fmt.Errorf("failed to check file status: %v", err)
        }
    }

    if file.State != genai.FileStateActive {
        return "", fmt.Errorf("file processing failed with state: %v", file.State)
    }

    // Generate content
    model := client.GenerativeModel(modelName)
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
        return "", fmt.Errorf("failed to generate content: %v", err)
    }

    if len(resp.Candidates) == 0 || resp.Candidates[0] == nil || resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
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

    return out.String(), nil
}

// ===================
// TEXT PROCESSING
// ===================

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
    if len(strings.TrimSpace(s)) < 20 {
        return false
    }

    total := 0
    printable := 0
    questionMarks := 0

    for _, r := range s {
        total++
        if r == '�' {
            questionMarks++
        }
        if (r >= 32 && r < 0xD800) || (r >= 0xE000 && r <= 0xFFFD) {
            printable++
        }
    }

    if total == 0 {
        return false
    }

    ratio := float64(printable) / float64(total)
    return ratio > 0.85 && questionMarks < 10
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
