package services

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"saas-chatbot-platform/models"
	"saas-chatbot-platform/utils"

	"github.com/google/uuid"
)

// SmartChunkingService provides intelligent text chunking
type SmartChunkingService struct {
	maxChunkSize   int
	overlap        int
	minChunkSize   int
	sentenceRegex  *regexp.Regexp
	paragraphRegex *regexp.Regexp
}

// NewSmartChunkingService creates a new smart chunking service
func NewSmartChunkingService(maxChunkSize, overlap, minChunkSize int) *SmartChunkingService {
	return &SmartChunkingService{
		maxChunkSize:   maxChunkSize,
		overlap:        overlap,
		minChunkSize:   minChunkSize,
		sentenceRegex:  regexp.MustCompile(`[.!?]+[\s]+`),
		paragraphRegex: regexp.MustCompile(`\n\n+`),
	}
}

// SmartChunk represents an enhanced chunk with metadata
type SmartChunk struct {
	ChunkID     string    `json:"chunk_id" bson:"chunk_id"`
	Text        string    `json:"text" bson:"text"`
	Compressed  bool      `json:"compressed" bson:"compressed"`
	Compression string    `json:"compression" bson:"compression"`
	Order       int       `json:"order" bson:"order"`
	StartIndex  int       `json:"start_index" bson:"start_index"`
	EndIndex    int       `json:"end_index" bson:"end_index"`
	CharCount   int       `json:"char_count" bson:"char_count"`
	WordCount   int       `json:"word_count" bson:"word_count"`
	Page        int       `json:"page" bson:"page,omitempty"`
	Summary     string    `json:"summary" bson:"summary,omitempty"`
	Keywords    []string  `json:"keywords" bson:"keywords,omitempty"`
	Embedding   []float64 `json:"embedding,omitempty" bson:"embedding,omitempty"`
	Language    string    `json:"language" bson:"language,omitempty"`
	Topic       string    `json:"topic" bson:"topic,omitempty"`
}

// ChunkText intelligently chunks text with sentence boundary awareness
func (scs *SmartChunkingService) ChunkText(text string) []SmartChunk {
	// First, split by paragraphs for better context
	paragraphs := scs.paragraphRegex.Split(text, -1)
	paragraphs = filterEmpty(paragraphs)

	if len(paragraphs) == 0 {
		return []SmartChunk{}
	}

	var chunks []SmartChunk
	currentChunk := new(strings.Builder)
	currentSize := 0
	chunkIndex := 0
	startIndex := 0

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if len(paragraph) == 0 {
			continue
		}

		paraSize := len(paragraph)

		// If adding this paragraph would exceed max size
		if currentSize+paraSize > scs.maxChunkSize && currentSize >= scs.minChunkSize {
			// Finalize current chunk
			if currentChunk.Len() > 0 {
				chunk := scs.createChunk(currentChunk.String(), chunkIndex, startIndex, startIndex+currentSize)
				chunks = append(chunks, chunk)
				chunkIndex++
			}

			// Start new chunk with overlap
			currentChunk = new(strings.Builder)
			currentSize = 0
			startIndex = 0

			// Add overlap from previous chunk
			if len(chunks) > 0 && scs.overlap > 0 {
				lastChunk := chunks[len(chunks)-1]
				overlapText := scs.getOverlapText(lastChunk.Text, scs.overlap)
				if len(overlapText) > 0 {
					currentChunk.WriteString(overlapText)
					currentSize += len(overlapText)
				}
			}
		}

		// Add paragraph to current chunk
		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(paragraph)
		currentSize += paraSize
	}

	// Add final chunk if there's remaining content
	if currentChunk.Len() > 0 {
		chunk := scs.createChunk(currentChunk.String(), chunkIndex, startIndex, startIndex+currentSize)
		chunks = append(chunks, chunk)
	}

	return chunks
}

// createChunk creates a SmartChunk with metadata
func (scs *SmartChunkingService) createChunk(text string, order, startIndex, endIndex int) SmartChunk {
	words := strings.Fields(text)

	// Extract keywords (simple implementation - get frequent words)
	keywords := scs.extractKeywords(text, 5)

	// Create chunk with metadata
	chunk := SmartChunk{
		ChunkID:     uuid.NewString(),
		Text:        text,
		Compressed:  false, // Keep original text, compression handled separately
		Compression: "",
		Order:       order,
		StartIndex:  startIndex,
		EndIndex:    endIndex,
		CharCount:   len(text),
		WordCount:   len(words),
		Keywords:    keywords,
		Language:    "en", // Default, can be detected
	}

	return chunk
}

// extractKeywords extracts top keywords from text
func (scs *SmartChunkingService) extractKeywords(text string, limit int) []string {
	words := strings.Fields(strings.ToLower(text))

	// Filter common stop words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
		"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
		"with": true, "is": true, "are": true, "was": true, "were": true,
	}

	wordFreq := make(map[string]int)
	for _, word := range words {
		word = strings.Trim(word, ".,;:!?()[]{}\"")
		if len(word) > 2 && !stopWords[word] {
			wordFreq[word]++
		}
	}

	// Get top keywords
	keywords := make([]string, 0, limit)
	for word, freq := range wordFreq {
		if freq >= 2 && len(keywords) < limit {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// getOverlapText extracts overlap text from end of previous chunk
func (scs *SmartChunkingService) getOverlapText(text string, overlapSize int) string {
	if len(text) <= overlapSize {
		return text
	}

	// Try to start from sentence boundary
	sentences := scs.sentenceRegex.Split(text, -1)
	sentences = filterEmpty(sentences)

	if len(sentences) == 0 {
		return text[len(text)-overlapSize:]
	}

	// Take last sentences that fit in overlap
	result := new(strings.Builder)
	result.WriteString(strings.Join(sentences[1:], ". "))

	return result.String()
}

// CompressChunk compresses a chunk for storage
func CompressChunk(chunk SmartChunk) (SmartChunk, error) {
	compressed, compression, err := utils.CompressText(chunk.Text)
	if err != nil {
		return chunk, err
	}

	chunk.Compressed = true
	chunk.Compression = string(compression)
	chunk.Text = base64.StdEncoding.EncodeToString(compressed)

	return chunk, nil
}

// DecompressChunk decompresses a chunk for retrieval
func DecompressChunk(chunk SmartChunk) (SmartChunk, error) {
	if !chunk.Compressed {
		return chunk, nil
	}

	compressed, err := base64.StdEncoding.DecodeString(chunk.Text)
	if err != nil {
		return chunk, fmt.Errorf("failed to decode chunk: %w", err)
	}

	decompressed, err := utils.DecompressText(compressed, utils.CompressionAlgorithm(chunk.Compression))
	if err != nil {
		return chunk, fmt.Errorf("failed to decompress chunk: %w", err)
	}

	chunk.Text = decompressed
	chunk.Compressed = false
	chunk.Compression = ""

	return chunk, nil
}

// filterEmpty removes empty strings from slice
func filterEmpty(slice []string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if len(strings.TrimSpace(s)) > 0 {
			result = append(result, s)
		}
	}
	return result
}

// ConvertSmartChunksToContentChunks converts SmartChunks to models.ContentChunk
func ConvertSmartChunksToContentChunks(smartChunks []SmartChunk) []models.ContentChunk {
	result := make([]models.ContentChunk, len(smartChunks))
	for i, chunk := range smartChunks {
		result[i] = models.ContentChunk{
			ChunkID:    chunk.ChunkID,
			Text:       chunk.Text,
			Order:      chunk.Order,
			Page:       chunk.Page,
			Confidence: 1.0,
			Method:     "smart-chunking",
		}
	}
	return result
}

// CompressChunksForStorage compresses all chunks for database storage
func CompressChunksForStorage(chunks []SmartChunk) ([]SmartChunk, error) {
	compressedChunks := make([]SmartChunk, len(chunks))

	for i, chunk := range chunks {
		compressed, err := CompressChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to compress chunk %d: %w", i, err)
		}
		compressedChunks[i] = compressed
	}

	return compressedChunks, nil
}

// DecompressChunksForRetrieval decompresses all chunks for retrieval
func DecompressChunksForRetrieval(chunks []SmartChunk) ([]SmartChunk, error) {
	decompressedChunks := make([]SmartChunk, len(chunks))

	for i, chunk := range chunks {
		decompressed, err := DecompressChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress chunk %d: %w", i, err)
		}
		decompressedChunks[i] = decompressed
	}

	return decompressedChunks, nil
}
