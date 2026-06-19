package retrieval

import (
	"context"
	"errors"
	"strings"

	retrievalcontract "github.com/tajiaoyezi/GovScribe/internal/rag/retrieval/contract"
	"github.com/tajiaoyezi/GovScribe/internal/rag/vector"
)

type Permission string

const PermissionTemplateSearch Permission = "template.search"

type Principal = retrievalcontract.Principal

type Authorizer interface {
	Authorize(context.Context, Principal, Permission) error
}

type SearchRequest struct {
	Intent         string
	DocumentType   string
	DocumentNumber string
	Organization   string
	TopK           int
	FrontendFilter string
}

type SearchResult struct {
	Hits                 []Hit
	RerankDegraded       bool
	InsufficientExamples bool
	FallbackDecision     string
}

type Hit struct {
	ChunkID          string
	DocumentID       string
	Text             string
	DocumentType     string
	DocumentNumber   string
	OrganizationName string
	Score            float64
}

type HybridQuery struct {
	Intent        string
	DocumentType  string
	TopK          int
	BackendFilter string
}

type ExactQuery struct {
	DocumentNumber string
	Organization   string
	TopK           int
	BackendFilter  string
}

type HybridSearcher interface {
	SearchHybrid(context.Context, HybridQuery) ([]Hit, error)
}

type ExactSearcher interface {
	SearchExact(context.Context, ExactQuery) ([]Hit, error)
}

type AccessController interface {
	BackendFilter(context.Context, Principal, SearchRequest) (string, error)
}

type ResultAccessFilter interface {
	FilterVisibleHits(context.Context, Principal, []Hit) ([]Hit, error)
}

var ErrUnauthorized = errors.New("unauthorized corpus retrieval")

type Service struct {
	auth            Authorizer
	acl             AccessController
	hybrid          HybridSearcher
	exact           ExactSearcher
	reranker        vector.Reranker
	scarceThreshold int
}

func NewService(auth Authorizer, acl AccessController, hybrid HybridSearcher, exact ExactSearcher, reranker vector.Reranker, scarceThreshold int) *Service {
	return &Service{
		auth:            auth,
		acl:             acl,
		hybrid:          hybrid,
		exact:           exact,
		reranker:        reranker,
		scarceThreshold: scarceThreshold,
	}
}

func (s *Service) Search(ctx context.Context, principal Principal, req SearchRequest) (SearchResult, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return SearchResult{}, err
	}
	resultFilter, err := s.resultFilter()
	if err != nil {
		return SearchResult{}, err
	}
	backendFilter, err := s.backendFilter(ctx, principal, req)
	if err != nil {
		return SearchResult{}, err
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}

	var hits []Hit
	if strings.TrimSpace(req.Intent) != "" && s.hybrid != nil {
		hybridHits, err := s.hybrid.SearchHybrid(ctx, HybridQuery{
			Intent:        req.Intent,
			DocumentType:  req.DocumentType,
			TopK:          topK,
			BackendFilter: backendFilter,
		})
		if err != nil {
			return SearchResult{}, err
		}
		hits = append(hits, hybridHits...)
	}
	if (strings.TrimSpace(req.DocumentNumber) != "" || strings.TrimSpace(req.Organization) != "") && s.exact != nil {
		exactHits, err := s.exact.SearchExact(ctx, ExactQuery{
			DocumentNumber: req.DocumentNumber,
			Organization:   req.Organization,
			TopK:           topK,
			BackendFilter:  backendFilter,
		})
		if err != nil {
			return SearchResult{}, err
		}
		hits = appendUnique(hits, exactHits)
	}
	filtered, err := resultFilter.FilterVisibleHits(ctx, principal, hits)
	if err != nil {
		return SearchResult{}, ErrUnauthorized
	}
	hits = filtered

	result := SearchResult{Hits: hits}
	if s.reranker != nil && len(result.Hits) > 1 && strings.TrimSpace(req.Intent) != "" {
		reranked, err := s.rerank(ctx, req.Intent, result.Hits)
		if err != nil {
			if errors.Is(err, vector.ErrRerankUnavailable) {
				result.RerankDegraded = true
			} else {
				return SearchResult{}, err
			}
		} else {
			result.Hits = reranked
		}
	}
	if s.scarceThreshold > 0 && len(result.Hits) < s.scarceThreshold {
		result.InsufficientExamples = true
	}
	return result, nil
}

func (s *Service) authorize(ctx context.Context, principal Principal) error {
	if s.auth == nil {
		return ErrUnauthorized
	}
	if err := s.auth.Authorize(ctx, principal, PermissionTemplateSearch); err != nil {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) backendFilter(ctx context.Context, principal Principal, req SearchRequest) (string, error) {
	if s.acl == nil {
		return "", ErrUnauthorized
	}
	filter, err := s.acl.BackendFilter(ctx, principal, req)
	if err != nil || strings.TrimSpace(filter) == "" {
		return "", ErrUnauthorized
	}
	return joinFilters(filter, "is_deleted == false"), nil
}

func (s *Service) resultFilter() (ResultAccessFilter, error) {
	if s.acl == nil {
		return nil, ErrUnauthorized
	}
	filter, ok := s.acl.(ResultAccessFilter)
	if !ok {
		return nil, ErrUnauthorized
	}
	return filter, nil
}

func (s *Service) rerank(ctx context.Context, query string, hits []Hit) ([]Hit, error) {
	docs := make([]string, len(hits))
	for i, hit := range hits {
		docs[i] = hit.Text
	}
	result, err := s.reranker.Rerank(ctx, vector.RerankRequest{Query: query, Documents: docs})
	if err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(result.Results))
	used := make(map[int]struct{}, len(result.Results))
	for _, score := range result.Results {
		if score.Index < 0 || score.Index >= len(hits) {
			continue
		}
		hit := hits[score.Index]
		hit.Score = score.Score
		out = append(out, hit)
		used[score.Index] = struct{}{}
	}
	for i, hit := range hits {
		if _, ok := used[i]; !ok {
			out = append(out, hit)
		}
	}
	return out, nil
}

func appendUnique(existing, incoming []Hit) []Hit {
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, hit := range existing {
		seen[hit.ChunkID] = struct{}{}
	}
	out := append([]Hit(nil), existing...)
	for _, hit := range incoming {
		if _, ok := seen[hit.ChunkID]; ok {
			continue
		}
		out = append(out, hit)
		seen[hit.ChunkID] = struct{}{}
	}
	return out
}

func joinFilters(parts ...string) string {
	var cleaned []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			cleaned = append(cleaned, strings.TrimSpace(part))
		}
	}
	return strings.Join(cleaned, " and ")
}
