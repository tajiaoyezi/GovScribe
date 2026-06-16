package retrieval

import "context"

type TemplateExampleRequest struct {
	Intent       string
	DocumentType string
	TopK         int
}

type TemplateExampleResult struct {
	Examples             []TemplateExample
	RerankDegraded       bool
	InsufficientExamples bool
}

type TemplateExample struct {
	ChunkID          string
	DocumentID       string
	Text             string
	DocumentType     string
	DocumentNumber   string
	OrganizationName string
	Score            float64
}

type TemplateExampleAPI struct {
	service *Service
}

func NewTemplateExampleAPI(service *Service) *TemplateExampleAPI {
	return &TemplateExampleAPI{service: service}
}

func (api *TemplateExampleAPI) SearchExamples(ctx context.Context, principal Principal, req TemplateExampleRequest) (TemplateExampleResult, error) {
	result, err := api.service.Search(ctx, principal, SearchRequest{
		Intent:       req.Intent,
		DocumentType: req.DocumentType,
		TopK:         req.TopK,
	})
	if err != nil {
		return TemplateExampleResult{}, err
	}
	examples := make([]TemplateExample, 0, len(result.Hits))
	for _, hit := range result.Hits {
		examples = append(examples, TemplateExample{
			ChunkID:          hit.ChunkID,
			DocumentID:       hit.DocumentID,
			Text:             hit.Text,
			DocumentType:     hit.DocumentType,
			DocumentNumber:   hit.DocumentNumber,
			OrganizationName: hit.OrganizationName,
			Score:            hit.Score,
		})
	}
	return TemplateExampleResult{
		Examples:             examples,
		RerankDegraded:       result.RerankDegraded,
		InsufficientExamples: result.InsufficientExamples,
	}, nil
}
