package draft

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
	retrievalcontract "github.com/tajiaoyezi/GovScribe/internal/rag/retrieval/contract"
)

func TestHighFreqDraftOrchestratorRetrievesBeforeC01GenerationUsingC06Doctype(t *testing.T) {
	var sequence []string
	examples := &recordingTemplateExampleSearcher{
		sequence: &sequence,
		result: retrievalcontract.TemplateExampleResult{
			Examples: []retrievalcontract.TemplateExample{
				{ChunkID: "c1", Text: "通知范文片段", DocumentType: "通知"},
			},
		},
	}
	contracts := &recordingCompleteContractReader{
		sequence: &sequence,
		contract: defaultCompleteContractForOrchestratorTest(t, "通知"),
	}
	model := &recordingCompleteClient{sequence: &sequence, response: llm.ChatResponse{Text: "生成正文", FinishReason: llm.FinishReasonStop}}
	orchestrator := NewHighFreqDraftOrchestrator(examples, contracts, model, HighFreqDraftOrchestratorConfig{FewShotTopK: 2})

	result, err := orchestrator.GenerateDraft(context.Background(), retrievalcontract.Principal{ID: "u1"}, HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability:     doctype.CapabilityC05,
			Doctype:              "通知",
			Subtype:              "召开会议",
			Direction:            doctype.DirectionDownward,
			Confidence:           0.91,
			SceneDescription:     "通知各部门召开年度会议",
			ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
		},
		ActorID:   "actor-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("generate draft: %v", err)
	}
	wantSequence := []string{"c03", "contract", "c01"}
	if strings.Join(sequence, ",") != strings.Join(wantSequence, ",") {
		t.Fatalf("sequence = %#v, want %#v", sequence, wantSequence)
	}
	if examples.lastReq.DocumentType != "通知" || examples.lastReq.Intent != "通知各部门召开年度会议" || examples.lastReq.TopK != 2 {
		t.Fatalf("c03 request = %#v, want c06 doctype/scene/topK", examples.lastReq)
	}
	if contracts.lastDoctype != "通知" {
		t.Fatalf("contract doctype = %q, want c06 doctype 通知", contracts.lastDoctype)
	}
	if model.lastReq.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("model security level = %q, want sensitive", model.lastReq.ContentSecurityLevel)
	}
	prompt := joinedMessages(model.lastReq.Messages)
	for _, want := range []string{"目标文种：通知", "代表子类：召开会议", "通知范文片段", "关于 + 事由 + 通知"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("model prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, forbidden := range []string{"通用兜底", "移交 c07", "fallback"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("c05 orchestrator prompt must not decide fallback via %q:\n%s", forbidden, prompt)
		}
	}
	if result.ModelResponse.Text != "生成正文" || result.Request.Context.Doctype != "通知" {
		t.Fatalf("result = %#v", result)
	}
}

func TestHighFreqDraftOrchestratorRejectsNonC05BeforeRetrievalOrGeneration(t *testing.T) {
	examples := &recordingTemplateExampleSearcher{}
	contracts := &recordingCompleteContractReader{}
	model := &recordingCompleteClient{}
	orchestrator := NewHighFreqDraftOrchestrator(examples, contracts, model, HighFreqDraftOrchestratorConfig{})

	_, err := orchestrator.GenerateDraft(context.Background(), retrievalcontract.Principal{ID: "u1"}, HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability: doctype.CapabilityC07,
			Doctype:          "命令",
			Subtype:          "任免令",
			SceneDescription: "发布一则任免某同志职务的命令",
		},
	})
	if !errors.Is(err, ErrScenarioNotForC05) {
		t.Fatalf("err = %v, want ErrScenarioNotForC05", err)
	}
	if examples.calls != 0 || contracts.calls != 0 || model.completeCalls != 0 {
		t.Fatalf("non-c05 must not enter c03/contract/c01: examples=%d contracts=%d c01=%d", examples.calls, contracts.calls, model.completeCalls)
	}
}

func TestHighFreqDraftOrchestratorCarriesC03InsufficientExamplesIntoPromptMetadata(t *testing.T) {
	var sequence []string
	examples := &recordingTemplateExampleSearcher{
		sequence: &sequence,
		result: retrievalcontract.TemplateExampleResult{
			Examples:             []retrievalcontract.TemplateExample{{ChunkID: "c1", Text: "仅一条通知范文", DocumentType: "通知"}},
			InsufficientExamples: true,
		},
	}
	contracts := &recordingCompleteContractReader{sequence: &sequence, contract: defaultCompleteContractForOrchestratorTest(t, "通知")}
	model := &recordingCompleteClient{sequence: &sequence, response: llm.ChatResponse{Text: "生成正文"}}
	orchestrator := NewHighFreqDraftOrchestrator(examples, contracts, model, HighFreqDraftOrchestratorConfig{FewShotTopK: 3})

	result, err := orchestrator.GenerateDraft(context.Background(), retrievalcontract.Principal{ID: "u1"}, HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability: doctype.CapabilityC05,
			Doctype:          "通知",
			SceneDescription: "通知各部门召开年度会议",
		},
	})
	if err != nil {
		t.Fatalf("generate draft: %v", err)
	}
	if !result.FewShot.Metadata.InsufficientExamples || result.FewShot.Metadata.Warning != InsufficientFewShotWarning {
		t.Fatalf("few-shot metadata = %#v, want c03 insufficient warning", result.FewShot.Metadata)
	}
	if !strings.Contains(result.Prompt, InsufficientFewShotWarning) {
		t.Fatalf("prompt missing insufficient warning:\n%s", result.Prompt)
	}
}

func defaultCompleteContractForOrchestratorTest(t *testing.T, doctypeName string) CompleteStructureContract {
	t.Helper()
	for _, contract := range DefaultStructureContracts() {
		if contract.Doctype == doctypeName {
			return CompleteStructureContract{
				StructureContract: contract,
				Template: PromptTemplateObject{
					Doctype: contract.Doctype,
					Content: BuildPromptTemplateContent(contract),
				},
			}
		}
	}
	t.Fatalf("missing default structure contract for %s", doctypeName)
	return CompleteStructureContract{}
}

type recordingTemplateExampleSearcher struct {
	sequence *[]string
	calls    int
	lastReq  retrievalcontract.TemplateExampleRequest
	result   retrievalcontract.TemplateExampleResult
	err      error
}

func (s *recordingTemplateExampleSearcher) SearchExamples(_ context.Context, _ retrievalcontract.Principal, req retrievalcontract.TemplateExampleRequest) (retrievalcontract.TemplateExampleResult, error) {
	s.calls++
	s.lastReq = req
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "c03")
	}
	return s.result, s.err
}

type recordingCompleteContractReader struct {
	sequence    *[]string
	calls       int
	lastDoctype string
	contract    CompleteStructureContract
	err         error
}

func (r *recordingCompleteContractReader) Get(_ context.Context, doctypeName string) (CompleteStructureContract, error) {
	r.calls++
	r.lastDoctype = doctypeName
	if r.sequence != nil {
		*r.sequence = append(*r.sequence, "contract")
	}
	return r.contract, r.err
}

type recordingCompleteClient struct {
	sequence      *[]string
	completeCalls int
	lastReq       llm.ChatRequest
	response      llm.ChatResponse
	err           error
}

func (c *recordingCompleteClient) Complete(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.completeCalls++
	c.lastReq = req
	if c.sequence != nil {
		*c.sequence = append(*c.sequence, "c01")
	}
	return c.response, c.err
}

func (c *recordingCompleteClient) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	panic("not used")
}

func (c *recordingCompleteClient) CurrentNetwork(context.Context) (llm.Network, error) {
	return llm.NetworkPrivate, nil
}
