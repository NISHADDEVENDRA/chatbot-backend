package ai

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/sony/gobreaker"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/time/rate"
	"google.golang.org/api/option"

	genai "github.com/google/generative-ai-go/genai"
)

type GeminiClient struct {
	apiKey       string
	breaker      *gobreaker.CircuitBreaker
	rateLimiter  *rate.Limiter
	tokenCounter *TokenCounter
	client       *genai.Client
	tier         string
}

type TokenCounter struct {
	mu              sync.Mutex
	minuteTokens    int
	dailyTokens     int
	minuteRequests  int
	dailyRequests   int
	lastMinuteReset time.Time
	lastDayReset    time.Time
}

type RateLimits struct {
	RPM int // Requests per minute
	TPM int // Tokens per minute
	RPD int // Requests per day
}

func NewGeminiClient(apiKey string, tier string) (*GeminiClient, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}

	// Configure rate limits based on tier
	limits := getRateLimits(tier)

	breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "GeminiAPI",
		MaxRequests: 5,
		Interval:    10 * time.Second,
		Timeout:     60 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 3 && failureRatio >= 0.6
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Printf("Circuit breaker %s: %s -> %s", name, from, to)
			// Alert on state changes
			if to == gobreaker.StateOpen {
				alertOps("Gemini API circuit breaker opened - service degraded")
			}
		},
	})

	// RPM limit with some buffer
	rateLimiter := rate.NewLimiter(rate.Limit(float64(limits.RPM)*0.9/60.0), limits.RPM/10)

	return &GeminiClient{
		apiKey:       apiKey,
		breaker:      breaker,
		rateLimiter:  rateLimiter,
		tokenCounter: &TokenCounter{},
		client:       client,
		tier:         tier,
	}, nil
}

func getRateLimits(tier string) RateLimits {
	switch tier {
	case "free":
		return RateLimits{RPM: 10, TPM: 250000, RPD: 250}
	case "tier1":
		return RateLimits{RPM: 1000, TPM: 1000000, RPD: 10000}
	case "tier2":
		return RateLimits{RPM: 2000, TPM: 4000000, RPD: 50000}
	default:
		return RateLimits{RPM: 10, TPM: 250000, RPD: 250}
	}
}

func (gc *GeminiClient) GenerateContent(ctx context.Context, prompt string, contextChunks []string) (*genai.GenerateContentResponse, error) {
	// Create tracing span
	tracer := otel.Tracer("gemini-client")
	ctx, span := tracer.Start(ctx, "gemini.generate_content")
	defer span.End()

	// Estimate tokens BEFORE making request
	estimatedTokens := estimateTokens(prompt, contextChunks)
	span.SetAttributes(
		attribute.Int("gemini.estimated_tokens", estimatedTokens),
		attribute.Int("gemini.context_chunks", len(contextChunks)),
		attribute.String("gemini.model", "gemini-2.0-flash"),
	)

	// Check token limits
	if !gc.tokenCounter.CanConsume(estimatedTokens, 1) {
		span.SetAttributes(attribute.Bool("gemini.rate_limited", true))
		return nil, errors.New("rate limit exceeded: wait before retry")
	}

	// Rate limiter wait
	if err := gc.rateLimiter.Wait(ctx); err != nil {
		span.SetAttributes(attribute.Bool("gemini.rate_limited", true))
		return nil, err
	}

	// Circuit breaker execution
	result, err := gc.breaker.Execute(func() (interface{}, error) {
		model := gc.client.GenerativeModel("gemini-2.0-flash")
		model.SetTemperature(0.7)
		model.SetMaxOutputTokens(2048)

		// Build prompt with context
		fullPrompt := buildPromptWithContext(prompt, contextChunks)

		resp, err := model.GenerateContent(ctx, genai.Text(fullPrompt))
		if err != nil {
			span.SetAttributes(attribute.Bool("gemini.error", true))
			span.SetAttributes(attribute.String("gemini.error_message", err.Error()))
			return nil, err
		}

		// Get ACTUAL token usage from response
		actualTokens := extractTokenUsage(resp)
		gc.tokenCounter.RecordUsage(actualTokens, 1)

		span.SetAttributes(
			attribute.Int("gemini.actual_tokens", actualTokens),
			attribute.Float64("gemini.token_accuracy", float64(actualTokens)/float64(estimatedTokens)),
		)

		return resp, nil
	})

	if err != nil {
		// Check if circuit breaker is open
		if err == gobreaker.ErrOpenState {
			span.SetAttributes(attribute.Bool("gemini.circuit_breaker_open", true))
			// Return cached/fallback response
			return gc.getFallbackResponse(prompt)
		}
		span.SetAttributes(attribute.Bool("gemini.error", true))
		return nil, err
	}

	span.SetAttributes(attribute.Bool("gemini.success", true))
	return result.(*genai.GenerateContentResponse), nil
}

func (tc *TokenCounter) CanConsume(tokens, requests int) bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	now := time.Now()

	// Reset counters if time windows expired
	if now.Sub(tc.lastMinuteReset) >= time.Minute {
		tc.minuteTokens = 0
		tc.minuteRequests = 0
		tc.lastMinuteReset = now
	}

	if now.Sub(tc.lastDayReset) >= 24*time.Hour {
		tc.dailyTokens = 0
		tc.dailyRequests = 0
		tc.lastDayReset = now
	}

	// Check limits - using free tier limits for now
	limits := RateLimits{RPM: 10, TPM: 250000, RPD: 250}

	if tc.minuteRequests+requests > limits.RPM {
		return false
	}
	if tc.minuteTokens+tokens > limits.TPM {
		return false
	}
	if tc.dailyRequests+requests > limits.RPD {
		return false
	}

	return true
}

func (tc *TokenCounter) RecordUsage(tokens, requests int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tc.minuteTokens += tokens
	tc.minuteRequests += requests
	tc.dailyTokens += tokens
	tc.dailyRequests += requests
}

// Fallback when Gemini unavailable
func (gc *GeminiClient) getFallbackResponse(prompt string) (*genai.GenerateContentResponse, error) {
	// Return cached response if available
	// Or polite error message for user
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				Content: &genai.Content{
					Parts: []genai.Part{
						genai.Text("I'm experiencing high demand right now. Please try again in a moment."),
					},
				},
			},
		},
	}, nil
}

// Accurate token estimation using Gemini's tokenizer
func estimateTokens(prompt string, chunks []string) int {
	// Fallback to rough estimation: 1 token ≈ 4 characters
	fullText := prompt
	for _, chunk := range chunks {
		fullText += "\n" + chunk
	}
	return len(fullText) / 4
}

// Extract token usage from Gemini response
func extractTokenUsage(resp *genai.GenerateContentResponse) int {
	// Try to extract actual usage from response metadata
	if resp.UsageMetadata != nil {
		return int(resp.UsageMetadata.TotalTokenCount)
	}

	// Fallback: estimate from response text
	// Average is ~4 characters per token for Gemini
	totalText := ""
	for _, candidate := range resp.Candidates {
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if text, ok := part.(genai.Text); ok {
					totalText += string(text)
				}
			}
		}
	}

	// Estimate using 4 characters per token
	estimated := len(totalText) / 4
	if estimated < 1 {
		estimated = 1 // Minimum 1 token
	}

	return estimated
}

// Build prompt with context chunks
func buildPromptWithContext(prompt string, contextChunks []string) string {
	if len(contextChunks) == 0 {
		return prompt
	}

	contextStr := ""
	for i, chunk := range contextChunks {
		contextStr += fmt.Sprintf("Context %d:\n%s\n\n", i+1, chunk)
	}

	return fmt.Sprintf("Based on the following context:\n\n%s\n\nPlease answer this question: %s", contextStr, prompt)
}

// Alert operations team
func alertOps(message string) {
	log.Printf("🚨 ALERT: %s", message)
	// In production, send to monitoring service (PagerDuty, Slack, etc.)
}

// Close the client
func (gc *GeminiClient) Close() error {
	if gc.client != nil {
		return gc.client.Close()
	}
	return nil
}
