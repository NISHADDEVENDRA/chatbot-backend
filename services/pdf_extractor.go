package services

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/models"

	"github.com/google/generative-ai-go/genai"
	"github.com/google/uuid"
	"github.com/ledongthuc/pdf"
	"google.golang.org/api/option"
)

// PDFExtractor handles robust PDF text extraction
type PDFExtractor struct {
	config       *config.Config
	geminiClient *genai.Client
}

// NewPDFExtractor creates a new PDF extractor
func NewPDFExtractor(cfg *config.Config) *PDFExtractor {
	return &PDFExtractor{
		config: cfg,
	}
}

// ExtractionResult contains the result of PDF text extraction
type ExtractionResult struct {
	Text           string
	Pages          int
	Method         string
	QualityScore   float64
	ProcessingTime time.Duration
	WordCount      int
	CharacterCount int
	Language       string
	HasImages      bool
	HasTables      bool
	Confidence     float64
}

// ExtractText extracts text from PDF using multiple methods with fallbacks
func (e *PDFExtractor) ExtractText(ctx context.Context, filePath string) (*ExtractionResult, error) {
	start := time.Now()

	// Enforce context deadline before heavy operations
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= 0 {
			return nil, fmt.Errorf("context deadline exceeded before extraction")
		}
	}

	// Cap extremely large files to avoid OOM
	stat, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat PDF file: %w", err)
	}
	if stat.Size() > 200<<20 { // 200MB safety cap
		return nil, fmt.Errorf("pdf too large for in-memory extraction")
	}

	// Read file content (bounded by size check above)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read PDF file: %w", err)
	}

	// Try extraction methods in order of preference
	methods := []struct {
		name    string
		extract func(context.Context, []byte) (*ExtractionResult, error)
	}{
		{"gemini", e.extractWithGemini},
		{"poppler", e.extractWithPoppler},
		{"go-pdf", e.extractWithGoPDF},
	}

	var lastErr error
	var bestResult *ExtractionResult

	for _, method := range methods {
		fmt.Printf("Trying %s extraction method...\n", method.name)

		result, err := method.extract(ctx, content)
		if err != nil {
			fmt.Printf("%s extraction failed: %v\n", method.name, err)
			lastErr = err
			continue
		}

		result.Method = method.name
		result.ProcessingTime = time.Since(start)

		// Evaluate quality
		quality := e.evaluateTextQuality(result.Text)
		result.QualityScore = quality

		fmt.Printf("%s extraction: %d chars, quality: %.2f\n", method.name, len(result.Text), quality)

		// If quality is good enough, use this result
		if quality >= 0.7 {
			return result, nil
		}

		// Keep track of best result so far
		if bestResult == nil || quality > bestResult.QualityScore {
			bestResult = result
		}
	}

	// If no method succeeded with good quality, return best attempt or error
	if bestResult != nil && bestResult.QualityScore >= 0.3 {
		fmt.Printf("Using best available result with quality %.2f\n", bestResult.QualityScore)
		return bestResult, nil
	}

	return nil, fmt.Errorf("all extraction methods failed: %v", lastErr)
}

// extractWithDeepSeekOCR removed - DeepSeek-OCR dependency eliminated

// extractWithGemini uses Google Gemini API for text extraction
func (e *PDFExtractor) extractWithGemini(ctx context.Context, content []byte) (*ExtractionResult, error) {
	if e.config.GeminiAPIKey == "" {
		return nil, fmt.Errorf("gemini API key not configured")
	}

	// Initialize Gemini client if not already done
	if e.geminiClient == nil {
		client, err := genai.NewClient(ctx, option.WithAPIKey(e.config.GeminiAPIKey))
		if err != nil {
			return nil, fmt.Errorf("failed to create gemini client: %w", err)
		}
		e.geminiClient = client
	}

	// Upload file to Gemini
	file, err := e.geminiClient.UploadFile(ctx, "", bytes.NewReader(content), &genai.UploadFileOptions{
		MIMEType: "application/pdf",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to upload file to gemini: %w", err)
	}
	defer e.geminiClient.DeleteFile(ctx, file.Name)

	// Configure model
	model := e.geminiClient.GenerativeModel("gemini-2.0-flash")
	model.SetTemperature(0.1)
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(`You are a precise document text extractor. Extract ALL text content from this PDF exactly as it appears, maintaining original formatting, line breaks, and structure. Do not summarize, interpret, or modify the content. Include headers, footers, captions, and all readable text elements. Preserve the document's natural flow and organization.`)},
	}

	// Extract text
	resp, err := model.GenerateContent(ctx,
		genai.FileData{URI: file.URI},
		genai.Text("Extract all text content from this PDF document. Maintain original formatting and structure."),
	)
	if err != nil {
		return nil, fmt.Errorf("gemini text extraction failed: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("no text extracted by gemini")
	}

	extractedText := ""
	for _, part := range resp.Candidates[0].Content.Parts {
		if textPart, ok := part.(genai.Text); ok {
			extractedText += string(textPart)
		}
	}

	result := &ExtractionResult{
		Text:       extractedText,
		Pages:      e.guessPageCount(extractedText),
		Confidence: 0.9, // Gemini typically has high confidence
	}

	e.analyzeText(result)
	return result, nil
}

// extractWithPoppler uses poppler-utils (pdftotext) for extraction
func (e *PDFExtractor) extractWithPoppler(ctx context.Context, content []byte) (*ExtractionResult, error) {
	// Check if pdftotext is available
	if !e.hasBinary("pdftotext") {
		return nil, fmt.Errorf("pdftotext not available")
	}

	// Create context with timeout
	extractCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Run pdftotext
	cmd := exec.CommandContext(extractCtx, "pdftotext", "-layout", "-", "-")
	cmd.Stdin = bytes.NewReader(content)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdftotext failed: %v, stderr: %s", err, stderr.String())
	}

	extractedText := stdout.String()
	if len(extractedText) == 0 {
		return nil, fmt.Errorf("no text extracted by pdftotext")
	}

	result := &ExtractionResult{
		Text:       extractedText,
		Pages:      e.guessPageCount(extractedText),
		Confidence: 0.8, // Poppler is generally reliable
	}

	e.analyzeText(result)
	return result, nil
}

// extractWithGoPDF uses the Go PDF library for extraction
func (e *PDFExtractor) extractWithGoPDF(ctx context.Context, content []byte) (*ExtractionResult, error) {
	reader, err := pdf.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("failed to create PDF reader: %w", err)
	}

	var textBuilder strings.Builder
	pages := reader.NumPage()

	for i := 1; i <= pages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}

		fonts := make(map[string]*pdf.Font)
		text, err := page.GetPlainText(fonts)
		if err != nil {
			fmt.Printf("Warning: failed to extract text from page %d: %v\n", i, err)
			continue
		}

		textBuilder.WriteString(fmt.Sprintf("\n\n--- PAGE %d ---\n", i))
		textBuilder.WriteString(text)
	}

	extractedText := textBuilder.String()
	if len(extractedText) == 0 {
		return nil, fmt.Errorf("no text extracted by go-pdf")
	}

	result := &ExtractionResult{
		Text:       extractedText,
		Pages:      pages,
		Confidence: 0.6, // Go PDF can be unreliable for complex PDFs
	}

	e.analyzeText(result)
	return result, nil
}

// evaluateTextQuality assesses the quality of extracted text
func (e *PDFExtractor) evaluateTextQuality(text string) float64 {
	if len(text) == 0 {
		return 0.0
	}

	text = strings.TrimSpace(text)
	if len(text) < 10 {
		return 0.1
	}

	// Count different character types
	var alphanumeric, printable, corrupted, whitespace int

	for _, r := range text {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			alphanumeric++
			printable++
		case r == ' ' || r == '\n' || r == '\t':
			whitespace++
			printable++
		case r == '.' || r == ',' || r == ';' || r == ':' || r == '!' || r == '?' || r == '-' || r == '_':
			printable++
		case r == '\uFFFD':
			corrupted++
		case r >= 32 && r <= 126:
			printable++
		default:
			// Check for unusual characters that might indicate corruption
			if r > 127 && !e.isCommonUnicodeChar(r) {
				corrupted++
			} else {
				printable++
			}
		}
	}

	total := len([]rune(text))
	if total == 0 {
		return 0.0
	}

	// Calculate ratios
	alphanumericRatio := float64(alphanumeric) / float64(total)
	printableRatio := float64(printable) / float64(total)
	corruptedRatio := float64(corrupted) / float64(total)

	// Quality scoring
	score := 0.0

	// Base score from printable ratio
	score += printableRatio * 0.4

	// Bonus for alphanumeric content
	if alphanumericRatio >= 0.3 {
		score += 0.3
	} else {
		score += alphanumericRatio
	}

	// Penalty for corruption
	score -= corruptedRatio * 2.0

	// Bonus for reasonable length
	if len(text) > 100 {
		score += 0.1
	}

	// Check for common patterns that indicate good extraction
	if e.hasGoodPatterns(text) {
		score += 0.2
	}

	// Ensure score is between 0 and 1
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	return score
}

// isCommonUnicodeChar checks if a character is a common Unicode character
func (e *PDFExtractor) isCommonUnicodeChar(r rune) bool {
	// Common punctuation and symbols
	common := []rune{'—', '"', '"', '‘', '’', '…', '€', '£', '¥', '©', '®', '™'}
	for _, c := range common {
		if r == c {
			return true
		}
	}
	return false
}

// hasGoodPatterns checks for patterns that indicate good text extraction
func (e *PDFExtractor) hasGoodPatterns(text string) bool {
	// Look for common patterns in well-extracted text
	patterns := []string{
		`\b[A-Z][a-z]+\b`,       // Capitalized words
		`\b\d{1,3}[,.]?\d{3}\b`, // Numbers with separators
		`[.!?]\s+[A-Z]`,         // Sentence boundaries
		`\b(the|and|or|of|to|in|for|with|on|at|by|from)\b`, // Common words
	}

	goodPatterns := 0
	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, text); matched {
			goodPatterns++
		}
	}

	return goodPatterns >= 3
}

// analyzeText performs additional analysis on extracted text
func (e *PDFExtractor) analyzeText(result *ExtractionResult) {
	text := result.Text

	// Count words and characters
	words := strings.Fields(text)
	result.WordCount = len(words)
	result.CharacterCount = len(text)

	// Detect language (simple heuristic)
	result.Language = e.detectLanguage(text)

	// Check for images and tables (based on common indicators)
	lowerText := strings.ToLower(text)
	result.HasImages = strings.Contains(lowerText, "image") || strings.Contains(lowerText, "figure") || strings.Contains(lowerText, "photo")
	result.HasTables = strings.Contains(lowerText, "table") || e.hasTableStructure(text)
}

// detectLanguage performs simple language detection
func (e *PDFExtractor) detectLanguage(text string) string {
	// Simple heuristic based on common words
	lowerText := strings.ToLower(text)

	englishWords := []string{"the", "and", "or", "of", "to", "in", "for", "with", "on", "at"}
	englishCount := 0
	for _, word := range englishWords {
		englishCount += strings.Count(lowerText, " "+word+" ")
	}

	if englishCount > 10 {
		return "en"
	}

	return "unknown"
}

// hasTableStructure checks if text contains table-like structure
func (e *PDFExtractor) hasTableStructure(text string) bool {
	lines := strings.Split(text, "\n")
	tabularLines := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for lines with multiple columns (indicated by multiple spaces or tabs)
		if len(line) > 10 && (strings.Count(line, "  ") > 2 || strings.Count(line, "\t") > 1) {
			tabularLines++
		}
	}

	return tabularLines > 3
}

// guessPageCount estimates page count from text markers or content
func (e *PDFExtractor) guessPageCount(text string) int {
	// Look for page markers
	pageMarkers := []string{"--- PAGE", "[[PAGE", "Page ", "\f"}
	maxPages := 0

	for _, marker := range pageMarkers {
		count := strings.Count(text, marker)
		if count > maxPages {
			maxPages = count
		}
	}

	if maxPages > 0 {
		return maxPages
	}

	// Estimate based on text length (rough heuristic)
	charCount := len(text)
	if charCount < 1000 {
		return 1
	} else if charCount < 5000 {
		return charCount / 2000
	} else {
		return charCount / 3000
	}
}

// hasBinary checks if a binary executable exists in PATH
func (e *PDFExtractor) hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// CreateChunks creates text chunks from extracted text
func (e *PDFExtractor) CreateChunks(text string, maxChunkSize, overlap int) []models.ContentChunk {
	if maxChunkSize == 0 {
		maxChunkSize = 1000
	}
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
