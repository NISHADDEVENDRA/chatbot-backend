package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"saas-chatbot-platform/internal/config"
	"saas-chatbot-platform/models"
)

// OCRClient handles communication with OCR services (Deprecated - DeepSeek-OCR removed)
type OCRClient struct {
	config     *config.Config
	httpClient *http.Client
	baseURL    string
}

// OCRRequest represents a request to the OCR service
type OCRRequest struct {
	File                io.Reader
	Filename            string
	ContentType         string
	ExtractTables       bool
	ExtractImages       bool
	ConfidenceThreshold float64
}

// OCRResponse represents the response from the OCR service
type OCRResponse struct {
	Success        bool       `json:"success"`
	Text           string     `json:"text"`
	Chunks         []OCRChunk `json:"chunks"`
	Pages          int        `json:"pages"`
	ProcessingTime float64    `json:"processing_time"`
	Method         string     `json:"method"`
	QualityScore   float64    `json:"quality_score"`
	WordCount      int        `json:"word_count"`
	CharacterCount int        `json:"character_count"`
	HasTables      bool       `json:"has_tables"`
	HasImages      bool       `json:"has_images"`
	Language       string     `json:"language"`
	Error          string     `json:"error,omitempty"`
}

// OCRChunk represents a text chunk from OCR processing
type OCRChunk struct {
	Text       string    `json:"text"`
	Confidence float64   `json:"confidence"`
	Page       int       `json:"page"`
	Bbox       []float64 `json:"bbox"`
	ChunkType  string    `json:"chunk_type"`
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status      string `json:"status"`
	ModelLoaded bool   `json:"model_loaded"`
	Device      string `json:"device"`
	Version     string `json:"version"`
}

// NewOCRClient creates a new OCR client
func NewOCRClient(cfg *config.Config) *OCRClient {
	baseURL := cfg.OCRServiceURL
	if baseURL == "" {
		baseURL = "http://localhost:8001"
	}

	return &OCRClient{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // OCR can take time
		},
		baseURL: baseURL,
	}
}

// IsHealthy checks if the OCR service is healthy
func (c *OCRClient) IsHealthy(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return false, fmt.Errorf("failed to create health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("OCR service unhealthy: status %d", resp.StatusCode)
	}

	var healthResp HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		return false, fmt.Errorf("failed to decode health response: %w", err)
	}

	return healthResp.Status == "healthy" && healthResp.ModelLoaded, nil
}

// ExtractText extracts text from a file using the OCR service
func (c *OCRClient) ExtractText(ctx context.Context, req *OCRRequest) (*OCRResponse, error) {
	// Check if service is healthy first
	healthy, err := c.IsHealthy(ctx)
	if err != nil {
		return nil, fmt.Errorf("OCR service health check failed: %w", err)
	}
	if !healthy {
		return nil, fmt.Errorf("OCR service is not healthy")
	}

	// Prepare multipart form data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	fileWriter, err := writer.CreateFormFile("file", req.Filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(fileWriter, req.File); err != nil {
		return nil, fmt.Errorf("failed to copy file data: %w", err)
	}

	// Add form fields
	writer.WriteField("extract_tables", fmt.Sprintf("%t", req.ExtractTables))
	writer.WriteField("extract_images", fmt.Sprintf("%t", req.ExtractImages))
	writer.WriteField("confidence_threshold", fmt.Sprintf("%.2f", req.ConfidenceThreshold))

	writer.Close()

	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/ocr/extract", &buf)
	if err != nil {
		return nil, fmt.Errorf("failed to create OCR request: %w", err)
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("OCR request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("❌ OCR request failed with status %d: %s\n", resp.StatusCode, string(body))
		return nil, fmt.Errorf("OCR request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Decode response
	var ocrResp OCRResponse
	if err := json.NewDecoder(resp.Body).Decode(&ocrResp); err != nil {
		fmt.Printf("❌ Failed to decode OCR response: %v\n", err)
		return nil, fmt.Errorf("failed to decode OCR response: %w", err)
	}

	fmt.Printf("📊 OCR Response: Success=%t, Method=%s, Quality=%.2f, TextLen=%d\n",
		ocrResp.Success, ocrResp.Method, ocrResp.QualityScore, len(ocrResp.Text))

	if !ocrResp.Success {
		fmt.Printf("❌ OCR processing failed: %s\n", ocrResp.Error)
		return nil, fmt.Errorf("OCR processing failed: %s", ocrResp.Error)
	}

	return &ocrResp, nil
}

// ConvertOCRResponseToExtractionResult converts OCR response to internal format
func (c *OCRClient) ConvertOCRResponseToExtractionResult(ocrResp *OCRResponse) *ExtractionResult {
	// Convert OCR chunks to internal chunks
	chunks := make([]models.ContentChunk, len(ocrResp.Chunks))
	for i, chunk := range ocrResp.Chunks {
		chunks[i] = models.ContentChunk{
			ChunkID:    fmt.Sprintf("ocr_%d", i),
			Text:       chunk.Text,
			Order:      i,
			Page:       chunk.Page,
			Confidence: chunk.Confidence,
			Method:     "ocr",
		}
	}

	return &ExtractionResult{
		Text:           ocrResp.Text,
		Pages:          ocrResp.Pages,
		Method:         "ocr",
		QualityScore:   ocrResp.QualityScore,
		ProcessingTime: time.Duration(ocrResp.ProcessingTime * float64(time.Second)),
		WordCount:      ocrResp.WordCount,
		CharacterCount: ocrResp.CharacterCount,
		Language:       ocrResp.Language,
		HasImages:      ocrResp.HasImages,
		HasTables:      ocrResp.HasTables,
		Confidence:     c.calculateAverageConfidence(ocrResp.Chunks),
	}
}

// calculateAverageConfidence calculates average confidence from chunks
func (c *OCRClient) calculateAverageConfidence(chunks []OCRChunk) float64 {
	if len(chunks) == 0 {
		return 0.0
	}

	total := 0.0
	for _, chunk := range chunks {
		total += chunk.Confidence
	}

	return total / float64(len(chunks))
}

// ExtractTextFromFile extracts text from a file path
func (c *OCRClient) ExtractTextFromFile(ctx context.Context, filePath, filename string) (*ExtractionResult, error) {
	// Read the entire file into memory first
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Create a reader from the file data
	fileReader := bytes.NewReader(fileData)

	// Determine content type
	contentType := "application/pdf"
	if filename != "" {
		ext := getFileExtension(filename)
		switch ext {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".gif":
			contentType = "image/gif"
		case ".bmp":
			contentType = "image/bmp"
		case ".tiff", ".tif":
			contentType = "image/tiff"
		}
	}

	req := &OCRRequest{
		File:                fileReader,
		Filename:            filename,
		ContentType:         contentType,
		ExtractTables:       true,
		ExtractImages:       true,
		ConfidenceThreshold: 0.7,
	}

	fmt.Printf("📁 File loaded: %s (%d bytes)\n", filename, len(fileData))

	ocrResp, err := c.ExtractText(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.ConvertOCRResponseToExtractionResult(ocrResp), nil
}

// getFileExtension extracts file extension
func getFileExtension(filename string) string {
	if len(filename) == 0 {
		return ""
	}

	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			return filename[i:]
		}
	}

	return ""
}
