package vector

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAIEmbeddingClientRequestsFloatEncodingAndChecksDimension(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			t.Fatalf("path = %s, want embeddings endpoint", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			t.Fatalf("authorization header = %q, want bearer token", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "bge-m3",
			"data": []map[string]any{{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.1, 0.2, 0.3},
			}},
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer server.Close()

	client, err := NewOpenAIEmbeddingClient(ClientConfig{
		BaseURL:             server.URL,
		APIKey:              "sk-test",
		EmbeddingModel:      "bge-m3",
		EmbeddingDimensions: 3,
		HTTPClient:          server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	vector, err := client.Embed(context.Background(), "起草通知")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vector.Values) != 3 {
		t.Fatalf("embedding dimension = %d, want 3", len(vector.Values))
	}
	if got["encoding_format"] != "float" {
		t.Fatalf("encoding_format = %#v, want float", got["encoding_format"])
	}
	if got["model"] != "bge-m3" || got["input"] != "起草通知" {
		t.Fatalf("request body = %#v", got)
	}
}

func TestOpenAIEmbeddingClientRejectsUnexpectedDimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"model":  "bge-m3",
			"data": []map[string]any{{
				"object":    "embedding",
				"index":     0,
				"embedding": []float64{0.1, 0.2},
			}},
			"usage": map[string]any{"prompt_tokens": 1, "total_tokens": 1},
		})
	}))
	defer server.Close()

	client, err := NewOpenAIEmbeddingClient(ClientConfig{
		BaseURL:             server.URL,
		APIKey:              "sk-test",
		EmbeddingModel:      "bge-m3",
		EmbeddingDimensions: 3,
		HTTPClient:          server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.Embed(context.Background(), "起草通知")
	if !errors.Is(err, ErrEmbeddingDimensionMismatch) {
		t.Fatalf("error = %v, want ErrEmbeddingDimensionMismatch", err)
	}
}

func TestRerankClientReturnsScoresAndTreatsUnavailableAsFailure(t *testing.T) {
	t.Run("scores", func(t *testing.T) {
		var got map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/rerank" {
				t.Fatalf("path = %s, want /v1/rerank", r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"index": 1, "relevance_score": 0.91},
					{"index": 0, "relevance_score": 0.42},
				},
			})
		}))
		defer server.Close()

		client, err := NewHTTPRerankClient(ClientConfig{
			BaseURL:     server.URL,
			APIKey:      "sk-rerank",
			RerankModel: "bge-reranker",
			HTTPClient:  server.Client(),
			Timeout:     time.Second,
		})
		if err != nil {
			t.Fatalf("new client: %v", err)
		}
		result, err := client.Rerank(context.Background(), RerankRequest{
			Query:     "起草通知",
			Documents: []string{"候选 A", "候选 B"},
		})
		if err != nil {
			t.Fatalf("rerank: %v", err)
		}
		if got["model"] != "bge-reranker" || got["query"] != "起草通知" {
			t.Fatalf("request body = %#v", got)
		}
		if len(result.Results) != 2 || result.Results[0].Index != 1 || result.Results[0].Score != 0.91 {
			t.Fatalf("rerank results = %#v", result.Results)
		}
	})

	t.Run("5xx unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "down", http.StatusBadGateway)
		}))
		defer server.Close()

		client, err := NewHTTPRerankClient(ClientConfig{
			BaseURL:     server.URL,
			RerankModel: "bge-reranker",
			HTTPClient:  server.Client(),
			Timeout:     time.Second,
		})
		if err != nil {
			t.Fatalf("new client: %v", err)
		}
		_, err = client.Rerank(context.Background(), RerankRequest{Query: "q", Documents: []string{"d"}})
		if !errors.Is(err, ErrRerankUnavailable) {
			t.Fatalf("error = %v, want ErrRerankUnavailable", err)
		}
	})
}

func TestEmbeddingProfilesMustMatchForIndexingAndQuery(t *testing.T) {
	indexing := EmbeddingProfile{Model: "bge-m3", Dimensions: 1024}
	query := EmbeddingProfile{Model: "bge-m3", Dimensions: 1024}
	if err := ValidateEmbeddingProfileCompatibility(indexing, query); err != nil {
		t.Fatalf("same profile rejected: %v", err)
	}

	query.Model = "other"
	if err := ValidateEmbeddingProfileCompatibility(indexing, query); !errors.Is(err, ErrEmbeddingProfileMismatch) {
		t.Fatalf("model mismatch error = %v, want ErrEmbeddingProfileMismatch", err)
	}

	query.Model = indexing.Model
	query.Dimensions = 768
	if err := ValidateEmbeddingProfileCompatibility(indexing, query); !errors.Is(err, ErrEmbeddingProfileMismatch) {
		t.Fatalf("dimension mismatch error = %v, want ErrEmbeddingProfileMismatch", err)
	}
}

func TestClientsUseInjectedConfigForPrivateServiceSwitch(t *testing.T) {
	publicCfg := ClientConfig{
		BaseURL:             "https://public-embedding.example/v1",
		APIKey:              "sk-public",
		EmbeddingModel:      "bge-public",
		EmbeddingDimensions: 1024,
	}
	privateCfg := publicCfg
	privateCfg.BaseURL = "http://mis-tei.internal/v1"
	privateCfg.EmbeddingModel = "bge-private"

	publicClient, err := NewOpenAIEmbeddingClient(publicCfg)
	if err != nil {
		t.Fatalf("public client: %v", err)
	}
	privateClient, err := NewOpenAIEmbeddingClient(privateCfg)
	if err != nil {
		t.Fatalf("private client: %v", err)
	}
	var _ Embedder = publicClient
	var _ Embedder = privateClient
	if publicClient.Profile() == privateClient.Profile() {
		t.Fatal("client profile did not change after config-only switch")
	}
}
