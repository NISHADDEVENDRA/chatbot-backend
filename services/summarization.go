package services

import (
	"context"
	"fmt"
	"strings"

	"saas-chatbot-platform/internal/ai"

	genai "github.com/google/generative-ai-go/genai"
)

// SummarizationService handles text summarization for token optimization
type SummarizationService struct {
	geminiClient *ai.GeminiClient
}

// NewSummarizationService creates a new summarization service
func NewSummarizationService(geminiClient *ai.GeminiClient) *SummarizationService {
	return &SummarizationService{
		geminiClient: geminiClient,
	}
}

// SummarizationResult represents the result of summarization
type SummarizationResult struct {
	Summary     string
	TokenCount  int
	Compression float64 // Original token count / summary token count
	KeyPoints   []string
	Topics      []string
}

// SummarizeText creates a concise summary of the text for token optimization
func (ss *SummarizationService) SummarizeText(ctx context.Context, text string) (*SummarizationResult, error) {
	// Estimate original tokens (1 token ≈ 4 characters for Gemini)
	originalTokens := len(text) / 4

	// For short texts, no summarization needed
	if originalTokens < 500 {
		previewLen := 100
		if len(text) < 100 {
			previewLen = len(text)
		}
		return &SummarizationResult{
			Summary:     text,
			TokenCount:  originalTokens,
			Compression: 1.0,
			KeyPoints:   []string{text[:previewLen]},
			Topics:      []string{"general"},
		}, nil
	}

	// Use Gemini to create summary
	prompt := buildSummarizationPrompt(text)

	contextChunks := []string{} // No context needed for summarization
	resp, err := ss.geminiClient.GenerateContent(ctx, prompt, contextChunks)
	if err != nil {
		return nil, fmt.Errorf("summarization failed: %w", err)
	}

	// Extract summary from response
	summary := extractTextFromResponse(resp)

	// Extract summary from response
	summaryTokens := len(summary) / 4
	compression := float64(originalTokens) / float64(max(summaryTokens, 1))

	result := &SummarizationResult{
		Summary:     summary,
		TokenCount:  summaryTokens,
		Compression: compression,
		KeyPoints:   extractKeyPoints(summary),
		Topics:      extractTopics(summary),
	}

	return result, nil
}

// SummarizeChunks summarizes multiple chunks efficiently
func (ss *SummarizationService) SummarizeChunks(ctx context.Context, chunks []string) ([]string, error) {
	// Group chunks if there are many to avoid rate limiting
	if len(chunks) <= 3 {
		return ss.summarizeBatch(ctx, chunks)
	}

	// Process in batches
	batchedSummaries := make([]string, 0, len(chunks))
	batchSize := 3

	for i := 0; i < len(chunks); i += batchSize {
		end := i + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		batch := chunks[i:end]
		summaries, err := ss.summarizeBatch(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("failed to summarize batch %d: %w", i/batchSize, err)
		}

		batchedSummaries = append(batchedSummaries, summaries...)
	}

	return batchedSummaries, nil
}

// summarizeBatch summarizes a batch of chunks
func (ss *SummarizationService) summarizeBatch(ctx context.Context, chunks []string) ([]string, error) {
	summaries := make([]string, len(chunks))

	for i, chunk := range chunks {
		result, err := ss.SummarizeText(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("failed to summarize chunk %d: %w", i, err)
		}
		summaries[i] = result.Summary
	}

	return summaries, nil
}

// buildSummarizationPrompt creates the prompt for summarization
func buildSummarizationPrompt(text string) string {
	return fmt.Sprintf(`Summarize the following text concisely, preserving:
1. Key information and facts
2. Important concepts
3. Names, numbers, and technical terms
4. Main topics and themes

Text to summarize:
%s

Provide a comprehensive yet concise summary:`, truncateText(text, 8000))
}

// extractTextFromResponse extracts text from Gemini response
func extractTextFromResponse(resp *genai.GenerateContentResponse) string {
	var result strings.Builder

	for _, candidate := range resp.Candidates {
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if text, ok := part.(genai.Text); ok {
					result.WriteString(string(text))
				}
			}
		}
	}

	return result.String()
}

// extractKeyPoints extracts key points from summary
func extractKeyPoints(summary string) []string {
	// Split by sentence and take important ones
	sentences := strings.Split(summary, ". ")
	keyPointsCount := 5
	if len(sentences) < 5 {
		keyPointsCount = len(sentences)
	}
	keyPoints := make([]string, 0, keyPointsCount)

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if len(sentence) > 20 && (strings.Contains(sentence, "key") || strings.Contains(sentence, "important") ||
			strings.Contains(sentence, "main") || strings.Contains(sentence, "critical")) {
			keyPoints = append(keyPoints, sentence)
		}
	}

	if len(keyPoints) == 0 && len(sentences) > 0 {
		keyPoints = append(keyPoints, sentences[0])
	}

	return keyPoints
}

// extractTopics extracts topics from summary
func extractTopics(summary string) []string {
	// Simple keyword-based topic extraction
	lowerSummary := strings.ToLower(summary)
	topics := []string{}

	// Common topic keywords
	topicKeywords := map[string][]string{
		"technical": {"api", "code", "technical", "implementation", "architecture"},
		"business":  {"revenue", "profit", "business", "market", "sales"},
		"research":  {"study", "research", "analysis", "findings", "data"},
	}

	for topic, keywords := range topicKeywords {
		for _, keyword := range keywords {
			if strings.Contains(lowerSummary, keyword) {
				topics = append(topics, topic)
				break
			}
		}
	}

	if len(topics) == 0 {
		topics = []string{"general"}
	}

	return topics
}

// truncateText truncates text to specified length
func truncateText(text string, maxLength int) string {
	if len(text) <= maxLength {
		return text
	}
	return text[:maxLength] + "..."
}

// ShouldSummarize determines if text should be summarized based on token count
func ShouldSummarize(text string, threshold int) bool {
	estimatedTokens := len(text) / 4
	return estimatedTokens > threshold
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}