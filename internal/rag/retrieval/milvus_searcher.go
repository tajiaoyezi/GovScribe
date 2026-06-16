package retrieval

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ragmilvus "github.com/tajiaoyezi/GovScribe/internal/rag/milvus"
	"github.com/tajiaoyezi/GovScribe/internal/rag/vector"

	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

var ErrHybridSearchUnavailable = errors.New("hybrid search unavailable")

type MilvusHybridSearcherConfig struct {
	CollectionName string
}

type MilvusHybridExecutor interface {
	ExecuteHybridSearch(context.Context, milvusclient.HybridSearchOption) ([]Hit, error)
}

type MilvusHybridSearcher struct {
	embedder vector.Embedder
	executor MilvusHybridExecutor
	cfg      MilvusHybridSearcherConfig
}

func NewMilvusHybridSearcher(embedder vector.Embedder, executor MilvusHybridExecutor, cfg MilvusHybridSearcherConfig) *MilvusHybridSearcher {
	return &MilvusHybridSearcher{embedder: embedder, executor: executor, cfg: cfg}
}

func (s *MilvusHybridSearcher) SearchHybrid(ctx context.Context, query HybridQuery) ([]Hit, error) {
	if s == nil || s.embedder == nil || s.executor == nil {
		return nil, ErrHybridSearchUnavailable
	}
	embedding, err := s.embedder.Embed(ctx, query.Intent)
	if err != nil {
		return nil, err
	}
	option := ragmilvus.BuildHybridSearchOption(ragmilvus.HybridSearchRequest{
		CollectionName: s.cfg.CollectionName,
		QueryText:      query.Intent,
		DenseVector:    embeddingFloat32(embedding.Values),
		Limit:          query.TopK,
		BackendFilter:  joinFilters(query.BackendFilter, documentTypeFilter(query.DocumentType)),
		OutputFields: []string{
			ragmilvus.FieldChunkID,
			ragmilvus.FieldDocumentID,
			ragmilvus.FieldBM25Text,
			ragmilvus.FieldDocumentType,
			ragmilvus.FieldDocumentNumber,
			ragmilvus.FieldOrganizationName,
		},
	})
	return s.executor.ExecuteHybridSearch(ctx, option)
}

func embeddingFloat32(values []float64) []float32 {
	out := make([]float32, len(values))
	for i, value := range values {
		out[i] = float32(value)
	}
	return out
}

func documentTypeFilter(documentType string) string {
	documentType = strings.TrimSpace(documentType)
	if documentType == "" {
		return ""
	}
	return fmt.Sprintf(`%s == "%s"`, ragmilvus.FieldDocumentType, strings.ReplaceAll(documentType, `"`, `\"`))
}
