//go:build integration

package milvus

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

func TestRunAnalyzerSplitsDocumentNumberAndOrganization(t *testing.T) {
	addr := os.Getenv("MILVUS_ADDR")
	if addr == "" {
		t.Skip("MILVUS_ADDR is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := milvusclient.New(ctx, &milvusclient.ClientConfig{Address: addr})
	if err != nil {
		t.Fatalf("connect milvus: %v", err)
	}
	defer client.Close(context.Background())

	results, err := client.RunAnalyzer(ctx, milvusclient.NewRunAnalyzerOption("XX市人民政府办公厅印发〔2024〕12号通知").WithAnalyzerParams(map[string]any{
		"type": "chinese",
	}).WithDetail())
	if err != nil {
		t.Fatalf("run analyzer: %v", err)
	}
	if len(results) != 1 || len(results[0].Tokens) == 0 {
		t.Fatalf("tokens = %#v", results)
	}
	tokens := tokenTexts(results[0].Tokens)
	t.Logf("tokens: %v", tokens)
	if containsToken(tokens, "〔2024〕12号") {
		t.Fatalf("document number remained whole token: %v", tokens)
	}
	if containsToken(tokens, "XX市人民政府办公厅") {
		t.Fatalf("organization remained whole token: %v", tokens)
	}
}

func tokenTexts(tokens []*entity.Token) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		out = append(out, token.Text)
	}
	return out
}

func containsToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}
