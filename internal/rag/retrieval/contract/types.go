package contract

type Principal struct {
	ID string
}

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
