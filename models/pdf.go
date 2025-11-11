package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PDF represents a unified PDF document model for both sync and async processing
type PDF struct {
	ID                 primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ClientID           primitive.ObjectID `bson:"client_id" json:"client_id"`
	Filename           string             `bson:"filename" json:"filename"`
	OriginalName       string             `bson:"original_name" json:"original_name"`
	FilePath           string             `bson:"file_path" json:"file_path"` // Storage path
	FileHash           string             `bson:"file_hash" json:"file_hash"` // For deduplication
	ContentChunks      []ContentChunk     `bson:"content_chunks,omitempty" json:"content_chunks,omitempty"`
	CompressedChunks   []byte             `bson:"compressed_chunks,omitempty" json:"-"` // Compressed chunks for storage
	CompressionEnabled bool               `bson:"compression_enabled" json:"compression_enabled"`
	Summary            string             `bson:"summary,omitempty" json:"summary,omitempty"`
	TotalTokens        int                `bson:"total_tokens" json:"total_tokens"`
	OriginalTokenCount int                `bson:"original_token_count" json:"original_token_count"`
	Status             string             `bson:"status" json:"status"` // pending, processing, completed, failed
	Progress           int                `bson:"progress" json:"progress"`
	ErrorMessage       string             `bson:"error_message,omitempty" json:"error_message,omitempty"`
	UploadedAt         time.Time          `bson:"uploaded_at" json:"uploaded_at"`
	ProcessedAt        *time.Time         `bson:"processed_at,omitempty" json:"processed_at,omitempty"`
	Metadata           PDFMetadata        `bson:"metadata" json:"metadata"`
	Cached             bool               `bson:"cached,omitempty" json:"cached,omitempty"`
	CachedAt           *time.Time         `bson:"cached_at,omitempty" json:"cached_at,omitempty"`
}

// ContentChunk represents a text chunk from the PDF
type ContentChunk struct {
	ChunkID     string    `bson:"chunk_id" json:"chunk_id"`
	Text        string    `bson:"text" json:"text"`
	Compressed  bool      `bson:"compressed,omitempty" json:"compressed,omitempty"`
	Compression string    `bson:"compression,omitempty" json:"compression,omitempty"`
	Order       int       `bson:"order" json:"order"`
	StartIndex  int       `bson:"start_index,omitempty" json:"start_index,omitempty"`
	EndIndex    int       `bson:"end_index,omitempty" json:"end_index,omitempty"`
	CharCount   int       `bson:"char_count,omitempty" json:"char_count,omitempty"`
	WordCount   int       `bson:"word_count,omitempty" json:"word_count,omitempty"`
	Page        int       `bson:"page,omitempty" json:"page,omitempty"`
	Confidence  float64   `bson:"confidence,omitempty" json:"confidence,omitempty"`   // Extraction confidence
	Method      string    `bson:"method,omitempty" json:"method,omitempty"`           // Extraction method used
	Keywords    []string  `bson:"keywords,omitempty" json:"keywords,omitempty"`       // Extracted keywords
	Summary     string    `bson:"summary,omitempty" json:"summary,omitempty"`         // Optional chunk summary
	TokenCount  int       `bson:"token_count,omitempty" json:"token_count,omitempty"` // Estimated tokens
	Language    string    `bson:"language,omitempty" json:"language,omitempty"`       // Language of chunk
	Topic       string    `bson:"topic,omitempty" json:"topic,omitempty"`             // Detected topic
	Vector      []float32 `bson:"vector,omitempty" json:"-"`                          // Optional: Atlas Vector Search
}

// PDFMetadata contains processing metadata
type PDFMetadata struct {
	Size             int64         `bson:"size" json:"size"`
	Pages            int           `bson:"pages" json:"pages"`
	ProcessingTime   time.Duration `bson:"processing_time" json:"processing_time"`
	ExtractionMethod string        `bson:"extraction_method" json:"extraction_method"`
	QualityScore     float64       `bson:"quality_score" json:"quality_score"`
	Language         string        `bson:"language,omitempty" json:"language,omitempty"`
	HasImages        bool          `bson:"has_images" json:"has_images"`
	HasTables        bool          `bson:"has_tables" json:"has_tables"`
	WordCount        int           `bson:"word_count" json:"word_count"`
	CharacterCount   int           `bson:"character_count" json:"character_count"`
}

// UploadResponse represents the response after successful upload
type UploadResponse struct {
	ID         string      `json:"id"`
	Filename   string      `json:"filename"`
	Status     string      `json:"status"`
	ChunkCount int         `json:"chunk_count,omitempty"`
	Metadata   PDFMetadata `json:"metadata"`
	Message    string      `json:"message"`
	TaskID     string      `json:"task_id,omitempty"` // For async processing
}

// ChunkingConfig defines how text should be chunked
type ChunkingConfig struct {
	MaxChunkSize int `json:"max_chunk_size"`
	Overlap      int `json:"overlap"`
	MinChunkSize int `json:"min_chunk_size"`
}

// PDFProcessingStatus represents processing status constants
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
)

// ExtractionMethod represents different extraction methods
const (
	ExtractionMethodGemini  = "gemini"
	ExtractionMethodPoppler = "poppler"
	ExtractionMethodGoPDF   = "go-pdf"
	ExtractionMethodHybrid  = "hybrid"
	ExtractionMethodOCR     = "ocr" // Generic OCR method
)
