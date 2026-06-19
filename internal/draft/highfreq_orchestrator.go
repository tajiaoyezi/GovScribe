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

type HighFreqDraftStreamMetadata struct {
	HighFreqDraftResponseContext
	InsufficientExamples bool
	Warning              string
}

type HighFreqDraftStreamEvent struct {
	llm.StreamEvent
	Metadata *HighFreqDraftStreamMetadata
}

type HighFreqDraftStreamResult struct {
	Request      HighFreqDraftRequest
	FewShot      FewShotPrompt
	Prompt       string
	ModelRequest llm.ChatRequest
	Events       <-chan HighFreqDraftStreamEvent
}

type highFreqDraftGenerationPlan struct {
	Request      HighFreqDraftRequest
	FewShot      FewShotPrompt
	Prompt       string
	ModelRequest llm.ChatRequest
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
	plan, err := o.prepareGeneration(ctx, principal, input)
	if err != nil {
		return HighFreqDraftGenerationResult{}, err
	}
	modelResp, err := o.model.Complete(ctx, plan.ModelRequest)
	if err != nil {
		return HighFreqDraftGenerationResult{}, err
	}
	return HighFreqDraftGenerationResult{
		Request:       plan.Request,
		FewShot:       plan.FewShot,
		Prompt:        plan.Prompt,
		ModelRequest:  plan.ModelRequest,
		ModelResponse: modelResp,
	}, nil
}

func (o *HighFreqDraftOrchestrator) StreamDraft(ctx context.Context, principal retrievalcontract.Principal, input HighFreqDraftRequestInput) (HighFreqDraftStreamResult, error) {
	plan, err := o.prepareGeneration(ctx, principal, input)
	if err != nil {
		return HighFreqDraftStreamResult{}, err
	}
	upstream, err := o.model.Stream(ctx, plan.ModelRequest)
	if err != nil {
		return HighFreqDraftStreamResult{}, err
	}
	return HighFreqDraftStreamResult{
		Request:      plan.Request,
		FewShot:      plan.FewShot,
		Prompt:       plan.Prompt,
		ModelRequest: plan.ModelRequest,
		Events:       appendHighFreqStreamMetadata(upstream, highFreqStreamMetadata(plan.Request, plan.FewShot)),
	}, nil
}

func (o *HighFreqDraftOrchestrator) prepareGeneration(ctx context.Context, principal retrievalcontract.Principal, input HighFreqDraftRequestInput) (highFreqDraftGenerationPlan, error) {
	if o == nil {
		return highFreqDraftGenerationPlan{}, errors.New("high frequency draft orchestrator is required")
	}
	request, err := NewHighFreqDraftRequest(input)
	if err != nil {
		return highFreqDraftGenerationPlan{}, err
	}
	if o.examples == nil {
		return highFreqDraftGenerationPlan{}, errors.New("template example searcher is required")
	}
	if o.contracts == nil {
		return highFreqDraftGenerationPlan{}, errors.New("structure contract reader is required")
	}
	if o.model == nil {
		return highFreqDraftGenerationPlan{}, errors.New("llm client is required")
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
		return highFreqDraftGenerationPlan{}, err
	}
	contract, err := o.contracts.Get(ctx, request.Context.Doctype)
	if err != nil {
		return highFreqDraftGenerationPlan{}, err
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
		return highFreqDraftGenerationPlan{}, err
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
	return highFreqDraftGenerationPlan{
		Request:      request,
		FewShot:      fewShot,
		Prompt:       prompt,
		ModelRequest: modelReq,
	}, nil
}

func highFreqStreamMetadata(request HighFreqDraftRequest, fewShot FewShotPrompt) HighFreqDraftStreamMetadata {
	return HighFreqDraftStreamMetadata{
		HighFreqDraftResponseContext: NewHighFreqDraftResponse(request, StructuredDraftBody{}).Context,
		InsufficientExamples:         fewShot.Metadata.InsufficientExamples,
		Warning:                      fewShot.Metadata.Warning,
	}
}

func appendHighFreqStreamMetadata(upstream <-chan llm.StreamEvent, metadata HighFreqDraftStreamMetadata) <-chan HighFreqDraftStreamEvent {
	out := make(chan HighFreqDraftStreamEvent)
	go func() {
		defer close(out)
		started := false
		for event := range upstream {
			wrapped := HighFreqDraftStreamEvent{StreamEvent: event}
			if !started || isTerminalStreamEvent(event) {
				eventMetadata := metadata
				wrapped.Metadata = &eventMetadata
			}
			started = true
			out <- wrapped
		}
	}()
	return out
}

func isTerminalStreamEvent(event llm.StreamEvent) bool {
	return event.Type == llm.StreamEventTypeDone || event.Type == llm.StreamEventTypeError
}
