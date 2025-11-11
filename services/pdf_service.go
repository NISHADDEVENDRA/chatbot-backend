package services

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"

	"saas-chatbot-platform/internal/ai"
	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/models"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PDFService provides secure, production-ready PDF processing
type PDFService struct {
	config         *config.Config
	pdfsCollection *mongo.Collection
	extractor      *PDFExtractor
	storage        *FileStorageManager
}

// NewPDFService creates a new PDF service instance
func NewPDFService(cfg *config.Config, pdfsCollection *mongo.Collection) *PDFService {
	storage := NewFileStorageManager(cfg)
	extractor := NewPDFExtractor(cfg)

	return &PDFService{
		config:         cfg,
		pdfsCollection: pdfsCollection,
		extractor:      extractor,
		storage:        storage,
	}
}

// FileStorageManager handles secure file storage operations
type FileStorageManager struct {
	config    *config.Config
	uploadDir string
	tempDir   string
}

// NewFileStorageManager creates a new file storage manager
func NewFileStorageManager(cfg *config.Config) *FileStorageManager {
	baseDir := cfg.FileStorageDir
	if baseDir == "" {
		baseDir = "./storage"
	}

	uploadDir := filepath.Join(baseDir, "pdfs")
	tempDir := filepath.Join(baseDir, "temp")

	// Create directories
	os.MkdirAll(uploadDir, 0755)
	os.MkdirAll(tempDir, 0755)

	return &FileStorageManager{
		config:    cfg,
		uploadDir: uploadDir,
		tempDir:   tempDir,
	}
}

// SecureUploadRequest represents a validated upload request
type SecureUploadRequest struct {
	File     multipart.File
	Header   *multipart.FileHeader
	ClientID primitive.ObjectID
	UserID   primitive.ObjectID
	IsAsync  bool
}

// UploadResult represents the result of an upload operation
type UploadResult struct {
	PDF    *models.PDF
	TaskID string // For async processing
}

// ValidateAndProcessUpload validates and processes a PDF upload
func (s *PDFService) ValidateAndProcessUpload(ctx context.Context, req *SecureUploadRequest) (*UploadResult, error) {
	// Step 1: Validate file
	if err := s.validateFile(req); err != nil {
		return nil, fmt.Errorf("file validation failed: %w", err)
	}

	// Step 2: Create secure file storage
	fileInfo, err := s.storage.SecureStore(req.File, req.Header, req.ClientID.Hex())
	if err != nil {
		return nil, fmt.Errorf("file storage failed: %w", err)
	}

	// Step 3: Check for duplicates
	existingPDF, err := s.checkDuplicate(ctx, req.ClientID, fileInfo.Hash)
	if err != nil {
		s.storage.Cleanup(fileInfo.Path) // Clean up on error
		return nil, fmt.Errorf("duplicate check failed: %w", err)
	}
	if existingPDF != nil {
		s.storage.Cleanup(fileInfo.Path) // Clean up duplicate
		return &UploadResult{PDF: existingPDF}, nil
	}

	// Step 4: Create PDF document record
	pdfDoc := &models.PDF{
		ID:           primitive.NewObjectID(),
		ClientID:     req.ClientID,
		Filename:     fileInfo.SecureName,
		OriginalName: req.Header.Filename,
		FilePath:     fileInfo.Path,
		FileHash:     fileInfo.Hash,
		Status:       models.StatusPending,
		Progress:     0,
		UploadedAt:   time.Now(),
		Metadata: models.PDFMetadata{
			Size: fileInfo.Size,
		},
	}

	// Step 5: Save to database
	if _, err := s.pdfsCollection.InsertOne(ctx, pdfDoc); err != nil {
		s.storage.Cleanup(fileInfo.Path) // Clean up on error
		return nil, fmt.Errorf("database save failed: %w", err)
	}

	// Step 6: Process based on size and async flag
	result := &UploadResult{PDF: pdfDoc}

	if req.IsAsync || fileInfo.Size > s.config.SyncProcessingLimit {
		// Async processing for large files
		taskID, err := s.enqueueProcessing(ctx, pdfDoc)
		if err != nil {
			// Don't fail the upload, just log the error
			fmt.Printf("Failed to enqueue async processing for %s: %v\n", pdfDoc.ID.Hex(), err)
		} else {
			result.TaskID = taskID
		}
	} else {
		// Sync processing for small files
		go func() {
			processingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			if err := s.ProcessPDFSync(processingCtx, pdfDoc); err != nil {
				fmt.Printf("Sync processing failed for %s: %v\n", pdfDoc.ID.Hex(), err)
				s.updateStatus(context.Background(), pdfDoc.ID, models.StatusFailed, err.Error())
			}
		}()
	}

	return result, nil
}

// SecureFileInfo contains information about securely stored file
type SecureFileInfo struct {
	Path       string
	SecureName string
	Hash       string
	Size       int64
}

// SecureStore stores a file securely with proper naming and validation
// OPTIMIZED: Uses streaming to avoid loading entire file into memory
func (sm *FileStorageManager) SecureStore(file multipart.File, header *multipart.FileHeader, clientID string) (*SecureFileInfo, error) {
	// Reset file reader position
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("failed to reset file position: %w", err)
	}

	// Generate secure filename
	secureName := sm.generateSecureFilename(header.Filename)

	// Create client-specific directory
	clientDir := filepath.Join(sm.uploadDir, clientID)
	if err := os.MkdirAll(clientDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create client directory: %w", err)
	}

	// Final file path
	filePath := filepath.Join(clientDir, secureName)

	// Create temp file for atomic write
	tempPath := filepath.Join(sm.tempDir, uuid.NewString()+".tmp")
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	// Create hash writer for deduplication (multiwriter writes to both temp file and hash)
	hasher := md5.New()
	multiWriter := io.MultiWriter(tempFile, hasher)

	// Stream file to temp location with hash calculation
	bytesWritten, err := io.Copy(multiWriter, file)
	if err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Close temp file before validation
	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	// Validate file size
	if bytesWritten == 0 {
		os.Remove(tempPath)
		return nil, fmt.Errorf("file is empty")
	}

	// Read first 4 bytes to validate PDF header without loading entire file
	tempCheckFile, err := os.Open(tempPath)
	if err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to open temp file for validation: %w", err)
	}

	headerBytes := make([]byte, 4)
	if _, err := tempCheckFile.ReadAt(headerBytes, 0); err != nil {
		tempCheckFile.Close()
		os.Remove(tempPath)
		return nil, fmt.Errorf("failed to read PDF header: %w", err)
	}
	tempCheckFile.Close()

	// Validate PDF magic bytes
	pdfHeaderBytes := []byte{0x25, 0x50, 0x44, 0x46}
	if string(headerBytes) != string(pdfHeaderBytes) {
		os.Remove(tempPath)
		return nil, fmt.Errorf("invalid PDF file: file is not a valid PDF document (missing PDF header)")
	}

	// Enhanced PDF validation using comprehensive checks
	if err := sm.validateFileContent(tempPath); err != nil {
		os.Remove(tempPath)
		return nil, fmt.Errorf("PDF validation failed: %w", err)
	}

	// Additional corruption check: verify PDF structure integrity
	if bytesWritten > 1024 {
		structureCheckFile, err := os.Open(tempPath)
		if err == nil {
			checkBytes := make([]byte, min(32768, bytesWritten))
			n, _ := structureCheckFile.ReadAt(checkBytes, 0)
			structureCheckFile.Close()

			// Check for basic PDF structure
			content := string(checkBytes[:n])
			if !strings.Contains(content, "obj") && !strings.Contains(content, "xref") {
				os.Remove(tempPath)
				return nil, fmt.Errorf("invalid PDF structure: file appears to be corrupted or incomplete")
			}
		}
	}

	// Move to final location
	if err := os.Rename(tempPath, filePath); err != nil {
		os.Remove(tempPath) // Clean up temp file
		return nil, fmt.Errorf("failed to move file to final location: %w", err)
	}

	fileHash := hex.EncodeToString(hasher.Sum(nil))

	return &SecureFileInfo{
		Path:       filePath,
		SecureName: secureName,
		Hash:       fileHash,
		Size:       bytesWritten,
	}, nil
}

// validateFile performs comprehensive file validation
func (s *PDFService) validateFile(req *SecureUploadRequest) error {
	header := req.Header

	// File size validation
	if header.Size > s.config.MaxFileSize {
		return fmt.Errorf("file size %d exceeds maximum allowed size %d", header.Size, s.config.MaxFileSize)
	}

	if header.Size == 0 {
		return fmt.Errorf("file is empty")
	}

	// Filename validation
	if err := s.validateFilename(header.Filename); err != nil {
		return err
	}

	// Content-Type validation
	contentType := header.Header.Get("Content-Type")
	if !strings.Contains(contentType, "pdf") && !strings.Contains(contentType, "application/pdf") {
		return fmt.Errorf("invalid content type: %s", contentType)
	}

	return nil
}

// validateFilename ensures filename is safe
func (s *PDFService) validateFilename(filename string) error {
	if filename == "" {
		return fmt.Errorf("filename is required")
	}

	if len(filename) > 255 {
		return fmt.Errorf("filename too long (max 255 characters)")
	}

	// Check for dangerous characters
	dangerous := []string{"../", "..\\", "<", ">", ":", "\"", "|", "?", "*", "\x00"}
	for _, char := range dangerous {
		if strings.Contains(filename, char) {
			return fmt.Errorf("filename contains invalid or dangerous characters")
		}
	}

	// Must end with .pdf
	if !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		return fmt.Errorf("only PDF files (.pdf extension) are allowed")
	}

	return nil
}

// min helper function for comparisons
func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// validateFileContent validates the actual file content
// Enhanced with comprehensive PDF corruption detection
func (sm *FileStorageManager) validateFileContent(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for validation: %w", err)
	}
	defer file.Close()

	// Read first 1024 bytes for header validation
	header := make([]byte, 1024)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read file header: %w", err)
	}
	if n < 4 {
		return fmt.Errorf("file is too small or empty")
	}

	// Check PDF magic bytes (0x25=P, 0x50=P, 0x44=D, 0x46=F)
	expectedPDFHeader := []byte{0x25, 0x50, 0x44, 0x46}
	if string(header[:4]) != string(expectedPDFHeader) {
		return fmt.Errorf("invalid PDF file: missing PDF magic bytes")
	}

	// Read PDF version - PDF format is: %PDF-X.Y where X is major version
	// Examples: %PDF-1.0, %PDF-1.4, %PDF-1.7, %PDF-2.0
	// Note: We validate the PDF structure below, so we're lenient with versions
	// Some PDFs may have unusual version numbers but still be valid

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	// Check for PDF EOF markers at the end (read last 2KB)
	if fileInfo.Size() > 2048 {
		trailer := make([]byte, 2048)
		file.Seek(fileInfo.Size()-2048, io.SeekStart)
		if _, err := file.Read(trailer); err != nil {
			return fmt.Errorf("failed to read PDF trailer: %w", err)
		}

		trailerStr := string(trailer)
		if !strings.Contains(trailerStr, "%%EOF") && !strings.Contains(trailerStr, "startxref") {
			return fmt.Errorf("invalid or corrupted PDF: missing EOF markers")
		}
	}

	// Check for basic PDF structure indicators
	headerStr := string(header)
	if !strings.Contains(headerStr, "obj") && !strings.Contains(headerStr, "xref") && !strings.Contains(headerStr, "trailer") {
		return fmt.Errorf("invalid PDF structure: file may be corrupted or not a valid PDF")
	}

	// Check for suspicious embedded content (security check)
	suspiciousPatterns := []string{
		"/JavaScript",
		"/JS",
		"/EmbeddedFile",
		"/Launch",
		"/URI",
		"javascript:",
	}

	lowerHeader := strings.ToLower(headerStr)
	for _, pattern := range suspiciousPatterns {
		if strings.Contains(lowerHeader, strings.ToLower(pattern)) {
			fmt.Printf("⚠️ Security warning: Potentially suspicious PDF content detected: %s\n", pattern)
		}
	}

	return nil
}

// generateSecureFilename creates a secure filename
func (sm *FileStorageManager) generateSecureFilename(originalName string) string {
	// Generate random prefix
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	randomPrefix := hex.EncodeToString(randomBytes)

	// Create timestamp
	timestamp := time.Now().Format("20060102_150405")

	// Clean original name
	ext := filepath.Ext(originalName)
	baseName := strings.TrimSuffix(originalName, ext)

	// Remove dangerous characters and limit length
	safeName := strings.ReplaceAll(baseName, " ", "_")
	safeName = strings.ReplaceAll(safeName, "..", "")
	if len(safeName) > 50 {
		safeName = safeName[:50]
	}

	return fmt.Sprintf("%s_%s_%s%s", timestamp, randomPrefix, safeName, ext)
}

// checkDuplicate checks if a file with the same hash already exists
func (s *PDFService) checkDuplicate(ctx context.Context, clientID primitive.ObjectID, fileHash string) (*models.PDF, error) {
	var existingPDF models.PDF
	err := s.pdfsCollection.FindOne(ctx, bson.M{
		"client_id": clientID,
		"file_hash": fileHash,
		"status":    bson.M{"$in": []string{models.StatusCompleted, models.StatusProcessing}},
	}).Decode(&existingPDF)

	if err == mongo.ErrNoDocuments {
		return nil, nil // No duplicate found
	}
	if err != nil {
		return nil, err
	}

	return &existingPDF, nil
}

// Cleanup removes a file from storage
func (sm *FileStorageManager) Cleanup(filePath string) {
	if filePath != "" {
		if err := os.Remove(filePath); err != nil {
			fmt.Printf("Failed to cleanup file %s: %v\n", filePath, err)
		}
	}
}

// updateStatus updates the processing status of a PDF
func (s *PDFService) updateStatus(ctx context.Context, pdfID primitive.ObjectID, status, errorMessage string) error {
	update := bson.M{
		"$set": bson.M{
			"status":     status,
			"updated_at": time.Now(),
		},
	}

	// Update progress based on status
	if status == models.StatusPending {
		update["$set"].(bson.M)["progress"] = 0
	} else if status == models.StatusProcessing {
		update["$set"].(bson.M)["progress"] = 50 // Processing is halfway
	} else if status == models.StatusCompleted {
		update["$set"].(bson.M)["progress"] = 100 // Completed = 100%
	} else if status == models.StatusFailed {
		update["$set"].(bson.M)["progress"] = 0 // Failed = 0%
	}

	if errorMessage != "" {
		update["$set"].(bson.M)["error_message"] = errorMessage
	}

	if status == models.StatusCompleted || status == models.StatusFailed {
		update["$set"].(bson.M)["processed_at"] = time.Now()
	}

	_, err := s.pdfsCollection.UpdateOne(ctx, bson.M{"_id": pdfID}, update)
	return err
}

// enqueueProcessing queues a PDF for async processing
func (s *PDFService) enqueueProcessing(ctx context.Context, pdf *models.PDF) (string, error) {
	// This would integrate with your queue system (Redis, etc.)
	// For now, return a mock task ID
	taskID := uuid.NewString()

	// In production, you would:
	// 1. Enqueue to Redis/RabbitMQ/etc.
	// 2. Return actual task ID
	// 3. Have background workers process the queue

	fmt.Printf("Enqueued PDF %s for async processing with task ID %s\n", pdf.ID.Hex(), taskID)
	return taskID, nil
}

// ProcessPDFSync processes a PDF synchronously
func (s *PDFService) ProcessPDFSync(ctx context.Context, pdf *models.PDF) error {
	// Update status to processing
	if err := s.updateStatus(ctx, pdf.ID, models.StatusProcessing, ""); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Extract text
	result, err := s.extractor.ExtractText(ctx, pdf.FilePath)
	if err != nil {
		return fmt.Errorf("text extraction failed: %w", err)
	}

	// Create chunks
	chunks := s.createChunks(result.Text, pdf.ID)

	// Update PDF with extracted content
	update := bson.M{
		"$set": bson.M{
			"content_chunks": chunks,
			"status":         models.StatusCompleted,
			"progress":       100, // Completed = 100%
			"processed_at":   time.Now(),
			"metadata": models.PDFMetadata{
				Size:             pdf.Metadata.Size,
				Pages:            result.Pages,
				ProcessingTime:   result.ProcessingTime,
				ExtractionMethod: result.Method,
				QualityScore:     result.QualityScore,
				Language:         result.Language,
				HasImages:        result.HasImages,
				HasTables:        result.HasTables,
				WordCount:        result.WordCount,
				CharacterCount:   result.CharacterCount,
			},
		},
	}

	_, err = s.pdfsCollection.UpdateOne(ctx, bson.M{"_id": pdf.ID}, update)
	if err != nil {
		return fmt.Errorf("failed to update PDF with extracted content: %w", err)
	}

	// If vector search is enabled, generate embeddings and upsert into pdf_chunks
	if s.config.VectorSearchEnabled {
		pdfChunksCol := s.pdfsCollection.Database().Collection("pdf_chunks")
		batch := make([]mongo.WriteModel, 0, len(chunks))
		for _, ch := range chunks {
			vec, embErr := ai.GenerateEmbedding(ctx, s.config, ch.Text)
			if embErr != nil {
				continue
			}
			doc := bson.M{
				"client_id": pdf.ClientID,
				"pdf_id":    pdf.ID,
				"chunk_id":  ch.ChunkID,
				"order":     ch.Order,
				"text":      ch.Text,
				"vector":    vec,
			}
			batch = append(batch, mongo.NewUpdateOneModel().
				SetFilter(bson.M{"pdf_id": pdf.ID, "chunk_id": ch.ChunkID}).
				SetUpdate(bson.M{"$set": doc}).
				SetUpsert(true))
		}
		if len(batch) > 0 {
			_, _ = pdfChunksCol.BulkWrite(ctx, batch, options.BulkWrite().SetOrdered(false))
		}
	}

	fmt.Printf("Successfully processed PDF %s: %d chunks, quality %.2f\n",
		pdf.ID.Hex(), len(chunks), result.QualityScore)

	return nil
}

// createChunks creates text chunks from extracted text
func (s *PDFService) createChunks(text string, pdfID primitive.ObjectID) []models.ContentChunk {
	maxChunkSize := s.config.MaxChunkSize
	if maxChunkSize == 0 {
		maxChunkSize = 1000
	}

	overlap := s.config.ChunkOverlap
	if overlap == 0 {
		overlap = 200
	}

	var chunks []models.ContentChunk
	words := strings.Fields(text)

	for i := 0; i < len(words); {
		end := i + maxChunkSize
		if end > len(words) {
			end = len(words)
		}

		chunkText := strings.Join(words[i:end], " ")

		chunk := models.ContentChunk{
			ChunkID: uuid.NewString(),
			Text:    chunkText,
			Order:   len(chunks),
		}

		chunks = append(chunks, chunk)

		if end >= len(words) {
			break
		}

		// Move forward with overlap
		nextStart := end - overlap
		if nextStart <= i {
			nextStart = i + 1
		}
		i = nextStart
	}

	return chunks
}
