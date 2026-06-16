//go:build integration

package vector

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestHTTPRerankClientSmoke(t *testing.T) {
	baseURL := os.Getenv("RERANK_BASE_URL")
	apiKey := os.Getenv("RERANK_API_KEY")
	model := os.Getenv("RERANK_MODEL")
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("RERANK_BASE_URL, RERANK_API_KEY, and RERANK_MODEL are required")
	}
	client, err := NewHTTPRerankClient(ClientConfig{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		RerankModel: model,
		Timeout:     30 * time.Second,
	})
	if err != nil {
		t.Fatalf("new rerank client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := client.Rerank(ctx, RerankRequest{
		Query: "起草一份会议通知",
		Documents: []string{
			"关于召开项目推进会的通知，请各部门按时参会。",
			"采购合同付款条款与验收安排。",
		},
	})
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(result.Results) == 0 {
		t.Fatal("rerank returned no results")
	}
	t.Logf("top result index=%d score=%0.6f", result.Results[0].Index, result.Results[0].Score)
}
