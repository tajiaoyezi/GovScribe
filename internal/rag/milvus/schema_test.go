package milvus

import (
	"strings"
	"testing"

	"github.com/milvus-io/milvus/client/v2/entity"
)

func TestBuildCollectionSchemaDefinesDenseBM25PartitionAndExactFields(t *testing.T) {
	schema, err := BuildCollectionSchema(CollectionConfig{
		CollectionName:      "govscribe_corpus_chunks",
		EmbeddingDimensions: 1024,
	})
	if err != nil {
		t.Fatalf("build schema: %v", err)
	}
	if schema.CollectionName != "govscribe_corpus_chunks" {
		t.Fatalf("collection name = %q", schema.CollectionName)
	}

	chunkID := fieldByName(t, schema, FieldChunkID)
	if !chunkID.PrimaryKey || chunkID.AutoID || chunkID.DataType != entity.FieldTypeVarChar {
		t.Fatalf("chunk_id field = %#v, want varchar manual primary key", chunkID)
	}

	dense := fieldByName(t, schema, FieldDenseVector)
	if dense.DataType != entity.FieldTypeFloatVector {
		t.Fatalf("dense field type = %s, want FloatVector", dense.DataType)
	}
	if dim, err := dense.GetDim(); err != nil || dim != 1024 {
		t.Fatalf("dense dim = %d, %v; want 1024", dim, err)
	}

	text := fieldByName(t, schema, FieldBM25Text)
	if text.DataType != entity.FieldTypeVarChar ||
		text.TypeParams["enable_analyzer"] != "true" ||
		text.TypeParams["analyzer_params"] == "" {
		t.Fatalf("bm25 text field = %#v, want analyzer-enabled varchar", text)
	}
	sparse := fieldByName(t, schema, FieldBM25Sparse)
	if sparse.DataType != entity.FieldTypeSparseVector {
		t.Fatalf("bm25 sparse type = %s, want SparseVector", sparse.DataType)
	}
	if len(schema.Functions) != 1 ||
		schema.Functions[0].Type != entity.FunctionTypeBM25 ||
		schema.Functions[0].InputFieldNames[0] != FieldBM25Text ||
		schema.Functions[0].OutputFieldNames[0] != FieldBM25Sparse {
		t.Fatalf("bm25 function = %#v", schema.Functions)
	}

	classification := fieldByName(t, schema, FieldClassification)
	if !classification.IsPartitionKey {
		t.Fatalf("classification is not partition key")
	}
	docType := fieldByName(t, schema, FieldDocumentType)
	if docType.TypeParams["enable_analyzer"] == "true" {
		t.Fatalf("document type should be scalar filter, got analyzer-enabled")
	}
	for _, name := range []string{FieldDocumentNumber, FieldOrganizationName} {
		f := fieldByName(t, schema, name)
		if f.DataType != entity.FieldTypeVarChar || f.TypeParams["enable_analyzer"] == "true" || f.TypeParams["enable_match"] == "true" {
			t.Fatalf("%s field = %#v, want scalar varchar without analyzer or text match", name, f)
		}
	}
}

func TestBuildHybridSearchOptionUsesDenseBM25RRFAndBackendFilter(t *testing.T) {
	option := BuildHybridSearchOption(HybridSearchRequest{
		CollectionName: "govscribe_corpus_chunks",
		QueryText:      "起草通知",
		DenseVector:    []float32{0.1, 0.2, 0.3},
		Limit:          5,
		BackendFilter:  `classification in ["internal"] and document_type == "通知" and is_deleted == false`,
		OutputFields:   []string{FieldChunkID, FieldDocumentType},
	})

	req, err := option.HybridRequest()
	if err != nil {
		t.Fatalf("hybrid request: %v", err)
	}
	if req.CollectionName != "govscribe_corpus_chunks" || len(req.Requests) != 2 {
		t.Fatalf("hybrid request = %#v", req)
	}
	if params(req.Requests[0].SearchParams)["anns_field"] != FieldDenseVector {
		t.Fatalf("dense ann params = %#v", params(req.Requests[0].SearchParams))
	}
	if params(req.Requests[1].SearchParams)["anns_field"] != FieldBM25Sparse {
		t.Fatalf("bm25 ann params = %#v", params(req.Requests[1].SearchParams))
	}
	for _, ann := range req.Requests {
		if ann.Dsl != `classification in ["internal"] and document_type == "通知" and is_deleted == false` {
			t.Fatalf("ann filter = %q", ann.Dsl)
		}
	}
	if params(req.RankParams)["strategy"] != "rrf" {
		t.Fatalf("rank params = %#v, want RRF", params(req.RankParams))
	}
	if len(req.OutputFields) != 2 || req.OutputFields[0] != FieldChunkID {
		t.Fatalf("output fields = %#v", req.OutputFields)
	}
}

func TestBuildExactRecallQueryOptionUsesScalarEqualityOnly(t *testing.T) {
	option := BuildExactRecallQueryOption(ExactRecallRequest{
		CollectionName:   "govscribe_corpus_chunks",
		DocumentNumber:   "〔2024〕12号",
		OrganizationName: "XX市人民政府办公厅",
		Limit:            3,
		BackendFilter:    `classification in ["internal"] and is_deleted == false`,
		OutputFields:     []string{FieldChunkID, FieldDocumentNumber, FieldOrganizationName},
	})

	req, err := option.Request()
	if err != nil {
		t.Fatalf("query request: %v", err)
	}
	if strings.Contains(req.Expr, "text_match") || strings.Contains(req.Expr, FieldBM25Text) || strings.Contains(req.Expr, FieldBM25Sparse) {
		t.Fatalf("exact recall expr = %q, must not use analyzer/BM25 fields", req.Expr)
	}
	if strings.Contains(req.Expr, " or ") {
		t.Fatalf("exact recall expr = %q, simultaneous exact fields must narrow with AND", req.Expr)
	}
	if !strings.Contains(req.Expr, FieldDocumentNumber+" == {document_number}") ||
		!strings.Contains(req.Expr, FieldOrganizationName+" == {organization_name}") ||
		!strings.Contains(req.Expr, `classification in ["internal"]`) {
		t.Fatalf("exact recall expr = %q", req.Expr)
	}
	if req.ExprTemplateValues["document_number"].GetStringVal() != "〔2024〕12号" ||
		req.ExprTemplateValues["organization_name"].GetStringVal() != "XX市人民政府办公厅" {
		t.Fatalf("template values = %#v", req.ExprTemplateValues)
	}
	if params(req.QueryParams)["limit"] != "3" {
		t.Fatalf("query params = %#v", params(req.QueryParams))
	}
}

func fieldByName(t *testing.T, schema *entity.Schema, name string) *entity.Field {
	t.Helper()
	for _, field := range schema.Fields {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("field %s not found", name)
	return nil
}

func params[T interface {
	GetKey() string
	GetValue() string
}](kvs []T) map[string]string {
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		out[kv.GetKey()] = kv.GetValue()
	}
	return out
}
