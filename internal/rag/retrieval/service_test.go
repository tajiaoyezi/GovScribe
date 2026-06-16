package retrieval

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	ragmilvus "github.com/tajiaoyezi/GovScribe/internal/rag/milvus"
	"github.com/tajiaoyezi/GovScribe/internal/rag/vector"

	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

func TestSearchRequiresTemplateSearchBeforeRetrieval(t *testing.T) {
	auth := &recordingAuthorizer{err: errors.New("deny")}
	hybrid := &recordingHybridSearcher{}
	svc := NewService(auth, allowACL{filter: `classification in ["internal"]`}, hybrid, nil, nil, 0)

	_, err := svc.Search(context.Background(), Principal{ID: "u1"}, SearchRequest{Intent: "起草通知", TopK: 5})

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	if auth.lastPermission != PermissionTemplateSearch {
		t.Fatalf("permission = %s, want template.search", auth.lastPermission)
	}
	if hybrid.called {
		t.Fatal("hybrid search ran after authorization denial")
	}
}

func TestSearchInjectsBackendFilterIgnoresFrontendFilterAndForcesNotDeleted(t *testing.T) {
	hybrid := &recordingHybridSearcher{hits: []Hit{{ChunkID: "c1", Text: "样例", Score: 0.7}}}
	svc := NewService(allowRetrievalAuthorizer{}, allowACL{filter: `classification in ["internal"] and document_type == "通知"`}, hybrid, nil, nil, 0)

	result, err := svc.Search(context.Background(), Principal{ID: "u1"}, SearchRequest{
		Intent:         "起草通知",
		DocumentType:   "通知",
		TopK:           5,
		FrontendFilter: `classification == "secret"`,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Hits) != 1 {
		t.Fatalf("hits = %#v", result.Hits)
	}
	if strings.Contains(hybrid.lastQuery.BackendFilter, "secret") {
		t.Fatalf("frontend filter leaked into backend filter: %q", hybrid.lastQuery.BackendFilter)
	}
	if !strings.Contains(hybrid.lastQuery.BackendFilter, `classification in ["internal"]`) ||
		!strings.Contains(hybrid.lastQuery.BackendFilter, `document_type == "通知"`) ||
		!strings.Contains(hybrid.lastQuery.BackendFilter, `is_deleted == false`) {
		t.Fatalf("backend filter = %q", hybrid.lastQuery.BackendFilter)
	}
}

func TestSearchCombinesExactRecallWithHybridResults(t *testing.T) {
	hybrid := &recordingHybridSearcher{hits: []Hit{{ChunkID: "c1", Text: "语义命中", Score: 0.7}}}
	exact := &recordingExactSearcher{hits: []Hit{{ChunkID: "c2", Text: "文号命中", Score: 1.0}}}
	svc := NewService(allowRetrievalAuthorizer{}, allowACL{filter: `classification in ["internal"]`}, hybrid, exact, nil, 0)

	result, err := svc.Search(context.Background(), Principal{ID: "u1"}, SearchRequest{
		Intent:         "起草通知",
		DocumentNumber: "〔2024〕12号",
		TopK:           5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !exact.called || exact.lastQuery.DocumentNumber != "〔2024〕12号" {
		t.Fatalf("exact query = %#v", exact.lastQuery)
	}
	if len(result.Hits) != 2 {
		t.Fatalf("hits = %#v, want merged hybrid + exact", result.Hits)
	}
}

func TestSearchPostFiltersMilvusBypassAndSoftDeletedHitsThroughAuthorityACL(t *testing.T) {
	hybrid := &recordingHybridSearcher{hits: []Hit{
		{ChunkID: "allowed", Text: "可见样例", Score: 0.8},
		{ChunkID: "secret", Text: "越级样例", Score: 0.7},
		{ChunkID: "deleted", Text: "软删样例", Score: 0.6},
	}}
	acl := allowACL{
		filter:     `classification in ["internal"]`,
		visibleIDs: map[string]struct{}{"allowed": {}},
	}
	svc := NewService(allowRetrievalAuthorizer{}, acl, hybrid, nil, nil, 0)

	result, err := svc.Search(context.Background(), Principal{ID: "u1"}, SearchRequest{Intent: "起草通知", TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.Hits) != 1 || result.Hits[0].ChunkID != "allowed" {
		t.Fatalf("hits = %#v, want only ACL-visible non-deleted hit", result.Hits)
	}
}

func TestSearchFailsClosedWhenAuthorityResultFilterIsMissing(t *testing.T) {
	hybrid := &recordingHybridSearcher{hits: []Hit{{ChunkID: "secret", Text: "越级样例", Score: 0.7}}}
	svc := NewService(allowRetrievalAuthorizer{}, backendOnlyACL{filter: `classification in ["internal"]`}, hybrid, nil, nil, 0)

	_, err := svc.Search(context.Background(), Principal{ID: "u1"}, SearchRequest{Intent: "起草通知", TopK: 5})

	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	if hybrid.called {
		t.Fatal("hybrid search ran without mandatory authority result filter")
	}
}

func TestSearchRerankUnavailableDegradesToRRFResults(t *testing.T) {
	hybrid := &recordingHybridSearcher{hits: []Hit{
		{ChunkID: "c1", Text: "候选 1", Score: 0.7},
		{ChunkID: "c2", Text: "候选 2", Score: 0.6},
	}}
	reranker := &recordingReranker{err: vector.ErrRerankUnavailable}
	svc := NewService(allowRetrievalAuthorizer{}, allowACL{filter: `classification in ["internal"]`}, hybrid, nil, reranker, 0)

	result, err := svc.Search(context.Background(), Principal{ID: "u1"}, SearchRequest{Intent: "起草通知", TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !result.RerankDegraded {
		t.Fatalf("rerank degraded = false, want true")
	}
	if len(result.Hits) != 2 || result.Hits[0].ChunkID != "c1" {
		t.Fatalf("hits = %#v, want original RRF order", result.Hits)
	}
}

func TestSearchMarksInsufficientExamplesWithoutChoosingFallback(t *testing.T) {
	hybrid := &recordingHybridSearcher{hits: []Hit{{ChunkID: "c1", Text: "样例", Score: 0.7}}}
	svc := NewService(allowRetrievalAuthorizer{}, allowACL{filter: `classification in ["internal"]`}, hybrid, nil, nil, 3)

	result, err := svc.Search(context.Background(), Principal{ID: "u1"}, SearchRequest{Intent: "起草通知", TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !result.InsufficientExamples {
		t.Fatalf("insufficient examples = false, want true")
	}
	if result.FallbackDecision != "" {
		t.Fatalf("fallback decision = %q, c03 must not decide c07 fallback", result.FallbackDecision)
	}
}

func TestMilvusHybridSearcherEmbedsIntentBuildsDenseBM25RRFAndReturnsNonEmptyRoute(t *testing.T) {
	embedder := &recordingEmbedder{embedding: vector.Embedding{Model: "bge-test", Values: []float64{0.1, 0.2, 0.3}}}
	executor := &recordingMilvusHybridExecutor{hits: []Hit{{ChunkID: "bm25-only", Text: "关键词命中", Score: 0.8}}}
	searcher := NewMilvusHybridSearcher(embedder, executor, MilvusHybridSearcherConfig{CollectionName: "govscribe_corpus_chunks"})

	hits, err := searcher.SearchHybrid(context.Background(), HybridQuery{
		Intent:        "起草通知",
		DocumentType:  "通知",
		TopK:          5,
		BackendFilter: `classification in ["internal"] and is_deleted == false`,
	})
	if err != nil {
		t.Fatalf("search hybrid: %v", err)
	}
	if embedder.lastText != "起草通知" {
		t.Fatalf("embedded text = %q", embedder.lastText)
	}
	req, err := executor.lastOption.HybridRequest()
	if err != nil {
		t.Fatalf("hybrid request: %v", err)
	}
	if len(req.Requests) != 2 {
		t.Fatalf("requests = %#v, want dense + BM25", req.Requests)
	}
	if milvusParams(req.Requests[0].SearchParams)["anns_field"] != ragmilvus.FieldDenseVector {
		t.Fatalf("dense request params = %#v", milvusParams(req.Requests[0].SearchParams))
	}
	if milvusParams(req.Requests[1].SearchParams)["anns_field"] != ragmilvus.FieldBM25Sparse {
		t.Fatalf("bm25 request params = %#v", milvusParams(req.Requests[1].SearchParams))
	}
	if milvusParams(req.RankParams)["strategy"] != "rrf" {
		t.Fatalf("rank params = %#v, want RRF", milvusParams(req.RankParams))
	}
	if !strings.Contains(req.Requests[0].Dsl, `document_type == "通知"`) ||
		!strings.Contains(req.Requests[0].Dsl, `is_deleted == false`) {
		t.Fatalf("backend filter = %q", req.Requests[0].Dsl)
	}
	if len(hits) != 1 || hits[0].ChunkID != "bm25-only" {
		t.Fatalf("hits = %#v, want non-empty route results", hits)
	}
}

func TestTemplateExampleAPIReturnsSamplesAndMetadataOnly(t *testing.T) {
	hybrid := &recordingHybridSearcher{hits: []Hit{{
		ChunkID:          "c1",
		DocumentID:       "doc-1",
		Text:             "范文片段",
		DocumentType:     "通知",
		DocumentNumber:   "〔2024〕12号",
		OrganizationName: "XX市人民政府办公厅",
		Score:            0.87,
	}}}
	svc := NewService(allowRetrievalAuthorizer{}, allowACL{filter: `classification in ["internal"]`}, hybrid, nil, nil, 0)
	api := NewTemplateExampleAPI(svc)

	result, err := api.SearchExamples(context.Background(), Principal{ID: "u1"}, TemplateExampleRequest{
		Intent:       "起草通知",
		DocumentType: "通知",
		TopK:         1,
	})
	if err != nil {
		t.Fatalf("search examples: %v", err)
	}
	if len(result.Examples) != 1 {
		t.Fatalf("examples = %#v", result.Examples)
	}
	example := result.Examples[0]
	if example.Text != "范文片段" ||
		example.DocumentNumber != "〔2024〕12号" ||
		example.OrganizationName != "XX市人民政府办公厅" {
		t.Fatalf("example = %#v", example)
	}
	if _, ok := reflect.TypeOf(example).FieldByName("Prompt"); ok {
		t.Fatalf("template example API must not expose prompt templates")
	}
}

func TestSensitiveIdentifierExactSearcherResolvesAuthorityChunkIDsBeforeDerivedLookup(t *testing.T) {
	authority := &recordingIdentifierAuthority{chunkIDs: []string{"c1"}}
	derived := &recordingChunkIDSearcher{hits: []Hit{{ChunkID: "c1", Text: "精确命中"}}}
	searcher := NewSensitiveIdentifierExactSearcher(authority, derived)

	hits, err := searcher.SearchExact(context.Background(), ExactQuery{
		DocumentNumber: "〔2024〕12号",
		Organization:   "XX市人民政府办公厅",
		TopK:           3,
		BackendFilter:  `classification in ["internal"] and is_deleted == false`,
	})
	if err != nil {
		t.Fatalf("search exact: %v", err)
	}
	if authority.lastQuery.DocumentNumber != "〔2024〕12号" ||
		authority.lastQuery.Organization != "XX市人民政府办公厅" {
		t.Fatalf("authority query = %#v", authority.lastQuery)
	}
	if len(derived.lastChunkIDs) != 1 || derived.lastChunkIDs[0] != "c1" {
		t.Fatalf("derived chunk ids = %#v", derived.lastChunkIDs)
	}
	if len(hits) != 1 || hits[0].ChunkID != "c1" {
		t.Fatalf("hits = %#v", hits)
	}
}

type recordingAuthorizer struct {
	err            error
	lastPermission Permission
}

func (a *recordingAuthorizer) Authorize(_ context.Context, _ Principal, permission Permission) error {
	a.lastPermission = permission
	return a.err
}

type allowRetrievalAuthorizer struct{}

func (allowRetrievalAuthorizer) Authorize(context.Context, Principal, Permission) error {
	return nil
}

type allowACL struct {
	filter     string
	err        error
	visibleIDs map[string]struct{}
}

func (a allowACL) BackendFilter(context.Context, Principal, SearchRequest) (string, error) {
	return a.filter, a.err
}

func (a allowACL) FilterVisibleHits(_ context.Context, _ Principal, hits []Hit) ([]Hit, error) {
	if a.visibleIDs == nil {
		return hits, nil
	}
	var out []Hit
	for _, hit := range hits {
		if _, ok := a.visibleIDs[hit.ChunkID]; ok {
			out = append(out, hit)
		}
	}
	return out, nil
}

type backendOnlyACL struct {
	filter string
	err    error
}

func (a backendOnlyACL) BackendFilter(context.Context, Principal, SearchRequest) (string, error) {
	return a.filter, a.err
}

type recordingHybridSearcher struct {
	called    bool
	lastQuery HybridQuery
	hits      []Hit
}

func (s *recordingHybridSearcher) SearchHybrid(_ context.Context, query HybridQuery) ([]Hit, error) {
	s.called = true
	s.lastQuery = query
	return append([]Hit(nil), s.hits...), nil
}

type recordingExactSearcher struct {
	called    bool
	lastQuery ExactQuery
	hits      []Hit
}

func (s *recordingExactSearcher) SearchExact(_ context.Context, query ExactQuery) ([]Hit, error) {
	s.called = true
	s.lastQuery = query
	return append([]Hit(nil), s.hits...), nil
}

type recordingReranker struct {
	err error
}

func (r *recordingReranker) Rerank(_ context.Context, req vector.RerankRequest) (vector.RerankResult, error) {
	if r.err != nil {
		return vector.RerankResult{}, r.err
	}
	return vector.RerankResult{Results: []vector.RerankScore{{Index: len(req.Documents) - 1, Score: 0.99}}}, nil
}

type recordingEmbedder struct {
	embedding vector.Embedding
	lastText  string
}

func (e *recordingEmbedder) Embed(_ context.Context, text string) (vector.Embedding, error) {
	e.lastText = text
	return e.embedding, nil
}

func (e *recordingEmbedder) Profile() vector.EmbeddingProfile {
	return vector.EmbeddingProfile{Model: e.embedding.Model, Dimensions: len(e.embedding.Values)}
}

type recordingMilvusHybridExecutor struct {
	lastOption milvusclient.HybridSearchOption
	hits       []Hit
}

func (e *recordingMilvusHybridExecutor) ExecuteHybridSearch(_ context.Context, option milvusclient.HybridSearchOption) ([]Hit, error) {
	e.lastOption = option
	return append([]Hit(nil), e.hits...), nil
}

func milvusParams[T interface {
	GetKey() string
	GetValue() string
}](kvs []T) map[string]string {
	out := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		out[kv.GetKey()] = kv.GetValue()
	}
	return out
}

type recordingIdentifierAuthority struct {
	lastQuery ExactQuery
	chunkIDs  []string
}

func (a *recordingIdentifierAuthority) ResolveExactChunkIDs(_ context.Context, query ExactQuery) ([]string, error) {
	a.lastQuery = query
	return append([]string(nil), a.chunkIDs...), nil
}

type recordingChunkIDSearcher struct {
	lastChunkIDs []string
	lastQuery    ExactQuery
	hits         []Hit
}

func (s *recordingChunkIDSearcher) SearchByChunkIDs(_ context.Context, chunkIDs []string, query ExactQuery) ([]Hit, error) {
	s.lastChunkIDs = append([]string(nil), chunkIDs...)
	s.lastQuery = query
	return append([]Hit(nil), s.hits...), nil
}
