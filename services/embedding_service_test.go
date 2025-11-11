package services

import (
	"context"
	"os"
	"testing"

	"saas-chatbot-platform/internal/ai"
	"saas-chatbot-platform/internal/config"
)

func TestGenerateEmbedding(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Skipf("config load failed: %v", err)
	}
	vec, err := ai.GenerateEmbedding(context.Background(), cfg, "hello world")
	if err != nil {
		t.Fatalf("embedding error: %v", err)
	}
	if len(vec) == 0 {
		t.Fatalf("empty embedding")
	}
}
