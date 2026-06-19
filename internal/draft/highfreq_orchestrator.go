package draft

import (
	"context"
	"errors"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
	retrievalcontract "github.com/tajiaoyezi/GovScribe/internal/rag/retrieval/contract"
)

type TemplateExampleSearcher interface {
	SearchExamples(context.Context, retrievalcontract.Principal, retrievalcontract.TemplateExampleRequest) (retrievalcontract.TemplateExampleResult, error)
}

type CompleteStructureContractReader interface {
	Get(context.Context, string) (CompleteStructureContract, error)
}

type HighFreqDraftOrchestratorConfig struct {
	FewShotTopK               int
	MinimumSufficientExamples int
}

type HighFreqDraftOrchestrator struct {
	examples  TemplateExampleSearcher
	contracts CompleteStructureContractReader
	model     llm.Client
	config    HighFreqDraftOrchestratorConfig
}

type HighFreqDraftGenerationResult struct {
	Request       HighFreqDraftRequest
	FewShot       FewShotPrompt
	Prompt        string
	ModelRequest  llm.ChatRequest
	ModelResponse llm.ChatResponse
}

func NewHighFreqDraftOrchestrator(
	examples TemplateExampleSearcher,
	contracts CompleteStructureContractReader,
	model llm.Client,
	config HighFreqDraftOrchestratorConfig,
) *HighFreqDraftOrchestrator {
	return &HighFreqDraftOrchestrator{
		examples:  examples,
		contracts: contracts,
		model:     model,
		config:    config,
	}
}

func (o *HighFreqDraftOrchestrator) GenerateDraft(ctx context.Context, principal retrievalcontract.Principal, input HighFreqDraftRequestInput) (HighFreqDraftGenerationResult, error) {
	if o == nil {
		return HighFreqDraftGenerationResult{}, errors.New("high frequency draft orchestrator is required")
	}
	request, err := NewHighFreqDraftRequest(input)
	if err != nil {
		return HighFreqDraftGenerationResult{}, err
	}
	if o.examples == nil {
		return HighFreqDraftGenerationResult{}, errors.New("template example searcher is required")
	}
	if o.contracts == nil {
		return HighFreqDraftGenerationResult{}, errors.New("structure contract reader is required")
	}
	if o.model == nil {
		return HighFreqDraftGenerationResult{}, errors.New("llm client is required")
	}

	topK := o.config.FewShotTopK
	if topK <= 0 {
		topK = DefaultFewShotTopK
	}
	c03Result, err := o.examples.SearchExamples(ctx, principal, retrievalcontract.TemplateExampleRequest{
		Intent:       request.Context.SceneDescription,
		DocumentType: request.Context.Doctype,
		TopK:         topK,
	})
	if err != nil {
		return HighFreqDraftGenerationResult{}, err
	}
	contract, err := o.contracts.Get(ctx, request.Context.Doctype)
	if err != nil {
		return HighFreqDraftGenerationResult{}, err
	}
	fewShot, err := AssembleFewShotPrompt(FewShotInput{
		Doctype:                   request.Context.Doctype,
		Subtype:                   request.Context.Subtype,
		SceneDescription:          request.Context.SceneDescription,
		FilledSlots:               request.Context.FilledSlots,
		MissingSlots:              request.Context.MissingSlots,
		MaxExamples:               topK,
		MinimumSufficientExamples: o.config.MinimumSufficientExamples,
		C03InsufficientExamples:   c03Result.InsufficientExamples,
		StructureContract:         contract.StructureContract,
		C03RetrievedExamples:      c03Result.Examples,
	})
	if err != nil {
		return HighFreqDraftGenerationResult{}, err
	}
	prompt := contract.Template.Content + "\n\n" + fewShot.Content
	modelReq := llm.ChatRequest{
		Messages: []llm.Message{
			{
				Role:    llm.RoleSystem,
				Content: "你是 GovScribe 高频文种初稿生成编排层。文种、子类、行文方向以 c06 上下文为准；仅输出规范正文初稿；不得改判文种、不得复算能力档、不得编造缺失要素。",
			},
			{Role: llm.RoleUser, Content: prompt},
		},
		ContentSecurityLevel: request.Context.ContentSecurityLevel,
		ActorID:              request.ActorID,
		RequestID:            request.RequestID,
	}
	modelResp, err := o.model.Complete(ctx, modelReq)
	if err != nil {
		return HighFreqDraftGenerationResult{}, err
	}
	return HighFreqDraftGenerationResult{
		Request:       request,
		FewShot:       fewShot,
		Prompt:        prompt,
		ModelRequest:  modelReq,
		ModelResponse: modelResp,
	}, nil
}
