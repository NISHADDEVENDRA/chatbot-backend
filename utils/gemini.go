package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type GeminiRequest struct {
	Contents []Content `json:"contents"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
	Error      *APIError   `json:"error,omitempty"`
}

type Candidate struct {
	Content Content `json:"content"`
}

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

type GeminiClient struct {
	APIKey    string
	APIURL    string
	HTTPClient *http.Client
}

func NewGeminiClient(apiKey, apiURL string) *GeminiClient {
	return &GeminiClient{
		APIKey: apiKey,
		APIURL: apiURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (g *GeminiClient) AskGemini(query string, context []string, maxRetries int) (*GeminiResponse, error) {
	// Prepare the prompt with context
	prompt := g.buildPromptWithContext(query, context)
	
	request := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{Text: prompt},
				},
			},
		},
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		response, err := g.makeRequest(request)
		if err == nil {
			return response, nil
		}
		
		lastErr = err
		if attempt < maxRetries {
			// Exponential backoff
			time.Sleep(time.Duration(1<<attempt) * time.Second)
		}
	}

	return nil, fmt.Errorf("failed after %d attempts: %v", maxRetries+1, lastErr)
}

func (g *GeminiClient) makeRequest(request GeminiRequest) (*GeminiResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("POST", g.APIURL+"?key="+g.APIKey, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %v", err)
	}

	if geminiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s (code: %d)", geminiResp.Error.Message, geminiResp.Error.Code)
	}

	if len(geminiResp.Candidates) == 0 {
		return nil, fmt.Errorf("no candidates in response")
	}

	return &geminiResp, nil
}

func (g *GeminiClient) buildPromptWithContext(query string, context []string) string {
	var prompt strings.Builder
	
	if len(context) > 0 {
		prompt.WriteString("Context from uploaded documents:\n\n")
		for i, ctx := range context {
			prompt.WriteString(fmt.Sprintf("Document %d:\n%s\n\n", i+1, ctx))
		}
		prompt.WriteString("Based on the above context, please answer the following question:\n\n")
	}
	
	prompt.WriteString(query)
	
	if len(context) > 0 {
		prompt.WriteString("\n\nPlease provide a helpful and accurate response based on the provided context. If the context doesn't contain relevant information, please say so.")
	}

	return prompt.String()
}

func (g *GeminiClient) CalculateTokens(text string) int {
	// Simple token estimation: ~4 characters per token
	// This is a rough approximation and should be replaced with actual tokenization
	return len(text) / 4
}

func (g *GeminiClient) ExtractResponseText(response *GeminiResponse) string {
	if len(response.Candidates) == 0 || len(response.Candidates[0].Content.Parts) == 0 {
		return ""
	}
	
	return response.Candidates[0].Content.Parts[0].Text
}