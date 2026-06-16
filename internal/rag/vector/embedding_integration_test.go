//go:build integration

package vector

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOpenAICompatibleEmbeddingSmoke(t *testing.T) {
	baseURL := os.Getenv("EMBEDDING_BASE_URL")
	model := os.Getenv("EMBEDDING_MODEL")
	if baseURL == "" || model == "" {
		t.Skip("EMBEDDING_BASE_URL and EMBEDDING_MODEL are required")
	}
	client, err := NewOpenAIEmbeddingClient(ClientConfig{
		BaseURL:             baseURL,
		APIKey:              "ollama",
		EmbeddingModel:      model,
		EmbeddingDimensions: 1024,
		Timeout:             30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new embedding client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	embedding, err := client.Embed(ctx, "起草一份会议通知")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if embedding.Model != model || len(embedding.Values) != 1024 {
		t.Fatalf("embedding = %#v, dimensions=%d", embedding, len(embedding.Values))
	}
	t.Logf("embedding model=%s dimensions=%d first3=%0.6f,%0.6f,%0.6f", embedding.Model, len(embedding.Values), embedding.Values[0], embedding.Values[1], embedding.Values[2])
}
