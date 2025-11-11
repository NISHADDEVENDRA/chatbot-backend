package ai

import (
	"context"
	"fmt"

	"saas-chatbot-platform/internal/config"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GenerateEmbedding returns an embedding vector for the given text.
// Default provider is Google Generative AI (text-embedding-004).
func GenerateEmbedding(ctx context.Context, cfg *config.Config, text string) ([]float32, error) {
	switch cfg.EmbeddingsProvider {
	case "google", "":
		if cfg.GeminiAPIKey == "" {
			return nil, fmt.Errorf("missing GEMINI_API_KEY for embeddings")
		}
		client, err := genai.NewClient(ctx, option.WithAPIKey(cfg.GeminiAPIKey))
		if err != nil {
			return nil, err
		}
		defer client.Close()

		model := client.EmbeddingModel(cfg.GoogleEmbeddingsModel)
		resp, err := model.EmbedContent(ctx, genai.Text(text))
		if err != nil {
			return nil, err
		}
		if resp.Embedding == nil {
			return nil, fmt.Errorf("no embedding returned")
		}

		// genai SDK returns []float32 for Embedding.Values
		return resp.Embedding.Values, nil

	case "openai":
		// Optional: implement OpenAI embeddings if needed
		return nil, fmt.Errorf("openai embeddings not implemented")

	default:
		return nil, fmt.Errorf("unknown embeddings provider: %s", cfg.EmbeddingsProvider)
	}
}
