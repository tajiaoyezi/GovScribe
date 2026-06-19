package draft

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
	retrievalcontract "github.com/tajiaoyezi/GovScribe/internal/rag/retrieval/contract"
)

func TestHighFreqDraftOrchestratorStreamsC01EventsWithC05MetadataAtHeadAndTail(t *testing.T) {
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
	model := &recordingStreamClient{
		sequence: &sequence,
		streamEvents: []llm.StreamEvent{
			{Type: llm.StreamEventTypeDelta, Delta: "正文"},
			{Type: llm.StreamEventTypeDelta, Delta: "片段"},
			{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop},
		},
	}
	orchestrator := NewHighFreqDraftOrchestrator(examples, contracts, model, HighFreqDraftOrchestratorConfig{FewShotTopK: 2})

	result, err := orchestrator.StreamDraft(context.Background(), retrievalcontract.Principal{ID: "u1"}, HighFreqDraftRequestInput{
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
		t.Fatalf("stream draft: %v", err)
	}

	events := collectHighFreqStreamEvents(result.Events)
	if got, want := strings.Join(sequence, ","), "c03,contract,c01-stream"; got != want {
		t.Fatalf("sequence = %s, want %s", got, want)
	}
	if got := joinedHighFreqDeltas(events); got != "正文片段" {
		t.Fatalf("joined stream text = %q, want 正文片段", got)
	}
	if events[0].Type != llm.StreamEventTypeDelta || events[1].Type != llm.StreamEventTypeDelta || events[2].Type != llm.StreamEventTypeDone {
		t.Fatalf("event types = %#v", events)
	}
	if events[0].Metadata == nil || events[2].Metadata == nil {
		t.Fatalf("head/tail metadata must be present: %#v", events)
	}
	if events[1].Metadata != nil {
		t.Fatalf("middle delta must not carry c05 metadata: %#v", events[1].Metadata)
	}
	if events[0].Metadata.Doctype != "通知" || events[0].Metadata.Subtype != "召开会议" || events[0].Metadata.RequestID != "req-1" {
		t.Fatalf("head metadata = %#v, want consumed c06 context identifiers", events[0].Metadata)
	}
	if events[2].Metadata.Doctype != "通知" || events[2].Metadata.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("tail metadata = %#v, want consumed c06 context identifiers", events[2].Metadata)
	}
	if model.lastStreamReq.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("stream c01 security level = %q, want sensitive", model.lastStreamReq.ContentSecurityLevel)
	}
}

func TestHighFreqDraftStreamDeltasAreEquivalentToCompleteDraftTextForSameRequest(t *testing.T) {
	completeClient := &recordingCompleteClient{response: llm.ChatResponse{Text: "正文片段", FinishReason: llm.FinishReasonStop}}
	streamClient := &recordingStreamClient{
		streamEvents: []llm.StreamEvent{
			{Type: llm.StreamEventTypeDelta, Delta: "正文"},
			{Type: llm.StreamEventTypeDelta, Delta: "片段"},
			{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop},
		},
	}
	input := HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability: doctype.CapabilityC05,
			Doctype:          "通知",
			Subtype:          "召开会议",
			Direction:        doctype.DirectionDownward,
			SceneDescription: "通知各部门召开年度会议",
		},
		ActorID:   "actor-1",
		RequestID: "req-1",
	}

	completeResult, err := NewHighFreqDraftOrchestrator(
		singleExampleSearcher(),
		singleContractReader(t, "通知"),
		completeClient,
		HighFreqDraftOrchestratorConfig{FewShotTopK: 2},
	).GenerateDraft(context.Background(), retrievalcontract.Principal{ID: "u1"}, input)
	if err != nil {
		t.Fatalf("complete draft: %v", err)
	}
	streamResult, err := NewHighFreqDraftOrchestrator(
		singleExampleSearcher(),
		singleContractReader(t, "通知"),
		streamClient,
		HighFreqDraftOrchestratorConfig{FewShotTopK: 2},
	).StreamDraft(context.Background(), retrievalcontract.Principal{ID: "u1"}, input)
	if err != nil {
		t.Fatalf("stream draft: %v", err)
	}

	streamEvents := collectHighFreqStreamEvents(streamResult.Events)
	if got := joinedHighFreqDeltas(streamEvents); got != completeResult.ModelResponse.Text {
		t.Fatalf("stream joined text = %q, want complete text %q", got, completeResult.ModelResponse.Text)
	}
	if !reflect.DeepEqual(streamClient.lastStreamReq.Messages, completeClient.lastReq.Messages) {
		t.Fatalf("stream and complete must use the same prompt\nstream=%#v\ncomplete=%#v", streamClient.lastStreamReq.Messages, completeClient.lastReq.Messages)
	}
}

func singleExampleSearcher() *recordingTemplateExampleSearcher {
	return &recordingTemplateExampleSearcher{
		result: retrievalcontract.TemplateExampleResult{
			Examples: []retrievalcontract.TemplateExample{
				{ChunkID: "c1", Text: "通知范文片段", DocumentType: "通知"},
			},
		},
	}
}

func singleContractReader(t *testing.T, doctypeName string) *recordingCompleteContractReader {
	t.Helper()
	return &recordingCompleteContractReader{contract: defaultCompleteContractForOrchestratorTest(t, doctypeName)}
}

type recordingStreamClient struct {
	sequence      *[]string
	streamCalls   int
	lastStreamReq llm.ChatRequest
	streamEvents  []llm.StreamEvent
	err           error
}

func (c *recordingStreamClient) Complete(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	panic("not used")
}

func (c *recordingStreamClient) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	c.streamCalls++
	c.lastStreamReq = req
	if c.sequence != nil {
		*c.sequence = append(*c.sequence, "c01-stream")
	}
	ch := make(chan llm.StreamEvent, len(c.streamEvents))
	for _, event := range c.streamEvents {
		ch <- event
	}
	close(ch)
	return ch, c.err
}

func (c *recordingStreamClient) CurrentNetwork(context.Context) (llm.Network, error) {
	return llm.NetworkPrivate, nil
}

func collectHighFreqStreamEvents(events <-chan HighFreqDraftStreamEvent) []HighFreqDraftStreamEvent {
	var collected []HighFreqDraftStreamEvent
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}

func joinedHighFreqDeltas(events []HighFreqDraftStreamEvent) string {
	var b strings.Builder
	for _, event := range events {
		if event.Type == llm.StreamEventTypeDelta {
			b.WriteString(event.Delta)
		}
	}
	return b.String()
}
