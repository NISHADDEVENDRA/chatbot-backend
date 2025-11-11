package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"saas-chatbot-platform/internal/ai"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/internal/database"
)

const (
	TaskProcessPDF     = "pdf:process"
	TaskGenerateAIResp = "ai:generate"
)

type PDFProcessPayload struct {
	ClientID string `json:"client_id"`
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
}

type AIGeneratePayload struct {
	ClientID       string `json:"client_id"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Prompt         string `json:"prompt"`
}

// Task creators
func NewPDFProcessTask(clientID, fileID, filePath string) (*asynq.Task, error) {
	payload, err := json.Marshal(PDFProcessPayload{
		ClientID: clientID,
		FileID:   fileID,
		FilePath: filePath,
	})
	if err != nil {
		return nil, err
	}

	return asynq.NewTask(
		TaskProcessPDF,
		payload,
		asynq.MaxRetry(3),
		asynq.Timeout(10*time.Minute),
		asynq.Queue("critical"),
	), nil
}

func NewAIGenerateTask(clientID, conversationID, messageID, prompt string) (*asynq.Task, error) {
	payload, err := json.Marshal(AIGeneratePayload{
		ClientID:       clientID,
		ConversationID: conversationID,
		MessageID:      messageID,
		Prompt:         prompt,
	})
	if err != nil {
		return nil, err
	}

	return asynq.NewTask(
		TaskGenerateAIResp,
		payload,
		asynq.MaxRetry(5),
		asynq.Timeout(2*time.Minute),
		asynq.Queue("default"),
	), nil
}

// Task handlers
type TaskProcessor struct {
	dbManager    *database.TenantDBManager
	geminiClient *ai.GeminiClient
	rdb          *mongo.Client
}

func NewTaskProcessor(dbManager *database.TenantDBManager, geminiClient *ai.GeminiClient, rdb *mongo.Client) *TaskProcessor {
	return &TaskProcessor{
		dbManager:    dbManager,
		geminiClient: geminiClient,
		rdb:          rdb,
	}
}

func (p *TaskProcessor) ProcessPDF(ctx context.Context, t *asynq.Task) error {
	var payload PDFProcessPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal failed: %w", asynq.SkipRetry)
	}

	log.Printf("Processing PDF: client=%s file=%s", payload.ClientID, payload.FileID)

	// Get tenant database
	tenantDB, err := p.dbManager.GetTenantDB(payload.ClientID)
	if err != nil {
		return err
	}

	// Update status to processing
	updatePDFStatus(tenantDB, payload.FileID, "processing")

	// Extract text from PDF
	pdfText, err := extractPDFText(payload.FilePath)
	if err != nil {
		updatePDFStatus(tenantDB, payload.FileID, "failed")
		return err
	}

	// Chunk the text
	chunks := chunkText(pdfText, 1000, 200)

	// Store chunks in database (legacy/simple schema)
	storePDFChunks(tenantDB, payload.FileID, chunks)

	// Additionally, upsert embeddings into pdf_chunks for vector search when enabled
	if cfg, err := config.LoadConfig(); err == nil && cfg.VectorSearchEnabled {
		pdfChunksCol := tenantDB.Collection("pdf_chunks")
		batch := make([]mongo.WriteModel, 0, len(chunks))
		for i, ch := range chunks {
			vec, embErr := ai.GenerateEmbedding(ctx, cfg, ch)
			if embErr != nil {
				continue
			}
			chunkID := fmt.Sprintf("%s_%d", payload.FileID, i)
			doc := bson.M{
				"pdf_id":   payload.FileID,
				"chunk_id": chunkID,
				"order":    i,
				"text":     ch,
				"vector":   vec,
			}
			batch = append(batch, mongo.NewUpdateOneModel().
				SetFilter(bson.M{"pdf_id": payload.FileID, "chunk_id": chunkID}).
				SetUpdate(bson.M{"$set": doc}).
				SetUpsert(true))
		}
		if len(batch) > 0 {
			_, _ = pdfChunksCol.BulkWrite(ctx, batch, options.BulkWrite().SetOrdered(false))
		}
	}

	// Update status to completed
	updatePDFStatus(tenantDB, payload.FileID, "completed")

	log.Printf("PDF processed successfully: %s", payload.FileID)
	return nil
}

func (p *TaskProcessor) GenerateAIResponse(ctx context.Context, t *asynq.Task) error {
	var payload AIGeneratePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal failed: %w", asynq.SkipRetry)
	}

	tenantDB, err := p.dbManager.GetTenantDB(payload.ClientID)
	if err != nil {
		return err
	}

	// Check tenant quota
	if err := ai.CheckTenantQuota(payload.ClientID, 1500, tenantDB); err != nil {
		storeErrorMessage(tenantDB, payload.MessageID, "Daily quota exceeded")
		return asynq.SkipRetry // Don't retry quota errors
	}

	// Retrieve relevant PDF chunks
	chunks := retrieveRelevantChunks(tenantDB, payload.Prompt, 3)

	// Generate response
	resp, err := p.geminiClient.GenerateContent(ctx, payload.Prompt, chunks)
	if err != nil {
		return err // Will retry
	}

	// Store AI response
	storeAIMessage(tenantDB, payload.ConversationID, resp)

	return nil
}

// Helper functions for PDF processing
func updatePDFStatus(db *mongo.Database, fileID, status string) error {
	ctx := context.Background()
	col := db.Collection("pdfs")

	_, err := col.UpdateOne(
		ctx,
		bson.M{"_id": fileID},
		bson.M{
			"$set": bson.M{
				"status":     status,
				"updated_at": time.Now(),
			},
		},
	)
	return err
}

func extractPDFText(filePath string) (string, error) {
	// Placeholder for PDF text extraction
	// In production, use a proper PDF library
	return "Sample PDF text content", nil
}

func chunkText(text string, chunkSize, overlap int) []string {
	// Simple text chunking
	// In production, use proper text chunking with overlap
	chunks := []string{}
	for i := 0; i < len(text); i += chunkSize - overlap {
		end := i + chunkSize
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[i:end])
	}
	return chunks
}

func storePDFChunks(db *mongo.Database, fileID string, chunks []string) error {
	ctx := context.Background()
	col := db.Collection("pdf_chunks")

	docs := make([]interface{}, len(chunks))
	for i, chunk := range chunks {
		docs[i] = bson.M{
			"file_id":     fileID,
			"chunk_text":  chunk,
			"chunk_index": i,
			"created_at":  time.Now(),
		}
	}

	_, err := col.InsertMany(ctx, docs)
	return err
}

func retrieveRelevantChunks(db *mongo.Database, prompt string, limit int) []string {
	// Placeholder for chunk retrieval
	// In production, use vector similarity search
	return []string{"Sample context chunk"}
}

func storeErrorMessage(db *mongo.Database, messageID, errorMsg string) error {
	ctx := context.Background()
	col := db.Collection("messages")

	_, err := col.UpdateOne(
		ctx,
		bson.M{"_id": messageID},
		bson.M{
			"$set": bson.M{
				"reply":      errorMsg,
				"error":      true,
				"updated_at": time.Now(),
			},
		},
	)
	return err
}

func storeAIMessage(db *mongo.Database, conversationID string, resp interface{}) error {
	// Placeholder for storing AI response
	// In production, parse the response and store properly
	return nil
}
