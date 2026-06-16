package milvus

import (
	"errors"
	"strings"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

const (
	DefaultCollectionName = "govscribe_corpus_chunks"

	FieldChunkID          = "chunk_id"
	FieldDocumentID       = "document_id"
	FieldBM25Text         = "content_text"
	FieldBM25Sparse       = "content_sparse"
	FieldDenseVector      = "dense_vector"
	FieldClassification   = "classification"
	FieldDocumentType     = "document_type"
	FieldDocumentNumber   = "document_number"
	FieldOrganizationName = "organization_name"
	FieldIsDeleted        = "is_deleted"
)

var ErrInvalidCollectionConfig = errors.New("invalid milvus collection config")

type CollectionConfig struct {
	CollectionName      string
	EmbeddingDimensions int
}

type HybridSearchRequest struct {
	CollectionName string
	QueryText      string
	DenseVector    []float32
	Limit          int
	BackendFilter  string
	OutputFields   []string
}

type ExactRecallRequest struct {
	CollectionName   string
	DocumentNumber   string
	OrganizationName string
	Limit            int
	BackendFilter    string
	OutputFields     []string
}

func BuildCollectionSchema(cfg CollectionConfig) (*entity.Schema, error) {
	if cfg.EmbeddingDimensions <= 0 {
		return nil, ErrInvalidCollectionConfig
	}
	name := cfg.CollectionName
	if name == "" {
		name = DefaultCollectionName
	}
	return entity.NewSchema().
		WithName(name).
		WithDescription("GovScribe corpus chunks derived index").
		WithAutoID(false).
		WithField(entity.NewField().WithName(FieldChunkID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(64).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(FieldDocumentID).WithDataType(entity.FieldTypeVarChar).WithMaxLength(64)).
		WithField(entity.NewField().WithName(FieldBM25Text).WithDataType(entity.FieldTypeVarChar).WithMaxLength(16384).WithEnableAnalyzer(true).WithEnableMatch(true).WithAnalyzerParams(map[string]any{"type": "chinese"})).
		WithField(entity.NewField().WithName(FieldBM25Sparse).WithDataType(entity.FieldTypeSparseVector)).
		WithField(entity.NewField().WithName(FieldDenseVector).WithDataType(entity.FieldTypeFloatVector).WithDim(int64(cfg.EmbeddingDimensions))).
		WithField(entity.NewField().WithName(FieldClassification).WithDataType(entity.FieldTypeVarChar).WithMaxLength(64).WithIsPartitionKey(true)).
		WithField(entity.NewField().WithName(FieldDocumentType).WithDataType(entity.FieldTypeVarChar).WithMaxLength(128)).
		WithField(entity.NewField().WithName(FieldDocumentNumber).WithDataType(entity.FieldTypeVarChar).WithMaxLength(256)).
		WithField(entity.NewField().WithName(FieldOrganizationName).WithDataType(entity.FieldTypeVarChar).WithMaxLength(512)).
		WithField(entity.NewField().WithName(FieldIsDeleted).WithDataType(entity.FieldTypeBool)).
		WithFunction(entity.NewFunction().
			WithName("corpus_content_bm25").
			WithType(entity.FunctionTypeBM25).
			WithInputFields(FieldBM25Text).
			WithOutputFields(FieldBM25Sparse)), nil
}

func BuildHybridSearchOption(req HybridSearchRequest) milvusclient.HybridSearchOption {
	collectionName := req.CollectionName
	if collectionName == "" {
		collectionName = DefaultCollectionName
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	dense := milvusclient.NewAnnRequest(FieldDenseVector, limit, entity.FloatVector(req.DenseVector))
	sparse := milvusclient.NewAnnRequest(FieldBM25Sparse, limit, entity.Text(req.QueryText))
	if req.BackendFilter != "" {
		dense.WithFilter(req.BackendFilter)
		sparse.WithFilter(req.BackendFilter)
	}
	outputFields := req.OutputFields
	if len(outputFields) == 0 {
		outputFields = []string{FieldChunkID, FieldDocumentID, FieldDocumentType, FieldDocumentNumber, FieldOrganizationName}
	}
	return milvusclient.NewHybridSearchOption(collectionName, limit, dense, sparse).
		WithReranker(milvusclient.NewRRFReranker()).
		WithOutputFields(outputFields...)
}

func BuildExactRecallQueryOption(req ExactRecallRequest) milvusclient.QueryOption {
	collectionName := req.CollectionName
	if collectionName == "" {
		collectionName = DefaultCollectionName
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	outputFields := req.OutputFields
	if len(outputFields) == 0 {
		outputFields = []string{FieldChunkID, FieldDocumentID, FieldDocumentType, FieldDocumentNumber, FieldOrganizationName}
	}
	option := milvusclient.NewQueryOption(collectionName).
		WithFilter(exactRecallFilter(req)).
		WithLimit(limit).
		WithOutputFields(outputFields...)
	if strings.TrimSpace(req.DocumentNumber) != "" {
		option.WithTemplateParam("document_number", strings.TrimSpace(req.DocumentNumber))
	}
	if strings.TrimSpace(req.OrganizationName) != "" {
		option.WithTemplateParam("organization_name", strings.TrimSpace(req.OrganizationName))
	}
	return option
}

func exactRecallFilter(req ExactRecallRequest) string {
	var exact []string
	if strings.TrimSpace(req.DocumentNumber) != "" {
		exact = append(exact, FieldDocumentNumber+" == {document_number}")
	}
	if strings.TrimSpace(req.OrganizationName) != "" {
		exact = append(exact, FieldOrganizationName+" == {organization_name}")
	}
	var filters []string
	if len(exact) > 0 {
		filters = append(filters, "("+strings.Join(exact, " and ")+")")
	}
	if strings.TrimSpace(req.BackendFilter) != "" {
		filters = append(filters, strings.TrimSpace(req.BackendFilter))
	}
	return strings.Join(filters, " and ")
}
