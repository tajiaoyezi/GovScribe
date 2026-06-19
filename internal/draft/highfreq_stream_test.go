package draft

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tajiaoyezi/GovScribe/internal/auth"
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
	orchestrator := NewHighFreqDraftOrchestrator(examples, contracts, model, allowDraftConfig(2))

	result, err := orchestrator.StreamDraft(context.Background(), authorizedDraftPrincipal("u1"), HighFreqDraftRequestInput{
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

func TestHighFreqDraftOrchestratorStreamsC01ErrorEventWithTailMetadata(t *testing.T) {
	model := &recordingStreamClient{
		streamEvents: []llm.StreamEvent{
			{Type: llm.StreamEventTypeDelta, Delta: "半截"},
			{Type: llm.StreamEventTypeDelta, Delta: "正文"},
			{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonUpstream},
		},
	}
	orchestrator := NewHighFreqDraftOrchestrator(
		singleExampleSearcher(),
		singleContractReader(t, "通知"),
		model,
		allowDraftConfig(2),
	)

	result, err := orchestrator.StreamDraft(context.Background(), authorizedDraftPrincipal("u1"), HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability: doctype.CapabilityC05,
			Doctype:          "通知",
			Subtype:          "召开会议",
			Direction:        doctype.DirectionDownward,
			SceneDescription: "通知各部门召开年度会议",
		},
		ActorID:   "actor-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("stream draft: %v", err)
	}

	events := collectHighFreqStreamEvents(result.Events)
	if got := joinedHighFreqDeltas(events); got != "半截正文" {
		t.Fatalf("joined stream text = %q, want 半截正文", got)
	}
	if events[2].Type != llm.StreamEventTypeError || events[2].ErrorReason != llm.ErrorReasonUpstream {
		t.Fatalf("tail event = %#v, want original c01 error event", events[2])
	}
	if events[0].Metadata == nil || events[2].Metadata == nil {
		t.Fatalf("head/error tail metadata must be present: %#v", events)
	}
	if events[1].Metadata != nil {
		t.Fatalf("middle delta must not carry c05 metadata: %#v", events[1].Metadata)
	}
	if events[2].Metadata.Doctype != "通知" || events[2].Metadata.Subtype != "召开会议" || events[2].Metadata.RequestID != "req-1" {
		t.Fatalf("error tail metadata = %#v, want consumed c06 context identifiers", events[2].Metadata)
	}
}

func TestHighFreqDraftOrchestratorStopsStreamAndMarksIncompleteWhenCallerCancels(t *testing.T) {
	model := newCancelAwareStreamClient()
	orchestrator := NewHighFreqDraftOrchestrator(
		singleExampleSearcher(),
		singleContractReader(t, "通知"),
		model,
		allowDraftConfig(2),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := orchestrator.StreamDraft(ctx, authorizedDraftPrincipal("u1"), HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability: doctype.CapabilityC05,
			Doctype:          "通知",
			Subtype:          "召开会议",
			Direction:        doctype.DirectionDownward,
			SceneDescription: "通知各部门召开年度会议",
		},
		ActorID:   "actor-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("stream draft: %v", err)
	}

	first, ok := receiveHighFreqStreamEvent(t, result.Events)
	if !ok {
		t.Fatal("stream closed before first delta")
	}
	if first.Type != llm.StreamEventTypeDelta || first.Delta != "半截" {
		t.Fatalf("first event = %#v, want half draft delta", first)
	}

	cancel()
	select {
	case <-model.released:
	case <-time.After(time.Second):
		t.Fatal("model stream resources were not released after caller cancellation")
	}
	tail, ok := receiveHighFreqStreamEvent(t, result.Events)
	if !ok {
		t.Fatal("stream closed without cancellation error event")
	}
	if tail.Type != llm.StreamEventTypeError || tail.ErrorReason != llm.ErrorReasonTimeout {
		t.Fatalf("tail event = %#v, want timeout error event for caller cancellation", tail)
	}
	if !errors.Is(tail.Err, context.Canceled) {
		t.Fatalf("tail error = %v, want context.Canceled", tail.Err)
	}
	if tail.Metadata == nil || tail.Metadata.RequestID != "req-1" {
		t.Fatalf("cancellation tail metadata = %#v, want consumed c06 context identifiers", tail.Metadata)
	}
	if got, ok := receiveHighFreqStreamEvent(t, result.Events); ok {
		t.Fatalf("unexpected event after cancellation tail: %#v", got)
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
		allowDraftConfig(2),
	).GenerateDraft(context.Background(), authorizedDraftPrincipal("u1"), input)
	if err != nil {
		t.Fatalf("complete draft: %v", err)
	}
	streamResult, err := NewHighFreqDraftOrchestrator(
		singleExampleSearcher(),
		singleContractReader(t, "通知"),
		streamClient,
		allowDraftConfig(2),
	).StreamDraft(context.Background(), authorizedDraftPrincipal("u1"), input)
	if err != nil {
		t.Fatalf("stream draft: %v", err)
	}

	streamEvents := collectHighFreqStreamEvents(streamResult.Events)
	if got := joinedHighFreqDeltas(streamEvents); got != completeResult.ModelResponse.Text {
		t.Fatalf("stream joined text = %q, want complete text %q", got, completeResult.ModelResponse.Text)
	}
	if !reflect.DeepEqual(streamClient.lastStreamReq, completeClient.lastReq) {
		t.Fatalf("stream and complete must use the same c01 request\nstream=%#v\ncomplete=%#v", streamClient.lastStreamReq, completeClient.lastReq)
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

type cancelAwareStreamClient struct {
	streamCalls   int
	lastStreamReq llm.ChatRequest
	released      chan struct{}
}

func newCancelAwareStreamClient() *cancelAwareStreamClient {
	return &cancelAwareStreamClient{released: make(chan struct{})}
}

func (c *cancelAwareStreamClient) Complete(context.Context, llm.ChatRequest) (llm.ChatResponse, error) {
	panic("not used")
}

func (c *cancelAwareStreamClient) Stream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	c.streamCalls++
	c.lastStreamReq = req
	ch := make(chan llm.StreamEvent)
	go func() {
		defer close(ch)
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeDelta, Delta: "半截"}
		<-ctx.Done()
		close(c.released)
	}()
	return ch, nil
}

func (c *cancelAwareStreamClient) CurrentNetwork(context.Context) (llm.Network, error) {
	return llm.NetworkPrivate, nil
}

func TestHighFreqDraftOrchestratorStreamRequiresDraftCreateAuthorization(t *testing.T) {
	store := auth.NewMemoryStore()
	examples := &recordingTemplateExampleSearcher{}
	model := &recordingStreamClient{}
	orchestrator := NewHighFreqDraftOrchestrator(
		examples,
		&recordingCompleteContractReader{},
		model,
		HighFreqDraftOrchestratorConfig{DraftAuthorizer: auth.NewAuthorizer(auth.NewRBACService(store))},
	)
	auditor := auth.Principal{UserID: "auditor-1", Roles: []auth.RoleCode{auth.RoleAuditor}, Authenticated: true}

	_, err := orchestrator.StreamDraft(context.Background(), auditor, highFreqDraftInputForRBAC())
	if !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("err = %v, want auth.ErrUnauthorized", err)
	}
	if examples.calls != 0 || model.streamCalls != 0 {
		t.Fatalf("RBAC denial must stop stream before c03/c01: c03=%d c01=%d", examples.calls, model.streamCalls)
	}
}

func collectHighFreqStreamEvents(events <-chan HighFreqDraftStreamEvent) []HighFreqDraftStreamEvent {
	var collected []HighFreqDraftStreamEvent
	for event := range events {
		collected = append(collected, event)
	}
	return collected
}

func receiveHighFreqStreamEvent(t *testing.T, events <-chan HighFreqDraftStreamEvent) (HighFreqDraftStreamEvent, bool) {
	t.Helper()
	select {
	case event, ok := <-events:
		return event, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for high frequency stream event")
	}
	return HighFreqDraftStreamEvent{}, false
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
