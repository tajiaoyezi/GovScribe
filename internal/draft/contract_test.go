package draft

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/auth"
	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestBuildGenerationRequestConsumesC06ContextForC05(t *testing.T) {
	scenario := doctype.ScenarioContext{
		TargetCapability: doctype.CapabilityC05,
		Doctype:          "请示",
		Subtype:          "资金费用申请",
		Direction:        doctype.DirectionUpward,
		Confidence:       0.93,
		SceneDescription: "区政府向市发改委申请活动经费5万元",
		FilledSlots: map[doctype.RequiredSlot]string{
			doctype.SlotIssuer:    "区政府",
			doctype.SlotRecipient: "市发改委",
			doctype.SlotSubject:   "活动经费",
			doctype.SlotKeyMatter: "5万元",
		},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	}

	req, branch, err := BuildGenerationRequest(GenerationInput{
		Scenario:  scenario,
		ActorID:   "actor-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("BuildGenerationRequest failed: %v", err)
	}
	if branch != BranchDeepDoctype {
		t.Fatalf("branch = %q, want deep doctype", branch)
	}
	if req.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("security level = %q, want sensitive", req.ContentSecurityLevel)
	}
	if req.ActorID != "actor-1" || req.RequestID != "req-1" {
		t.Fatalf("actor/request = %q/%q, want actor-1/req-1", req.ActorID, req.RequestID)
	}
	prompt := joinedMessages(req.Messages)
	for _, want := range []string{"请示", "资金费用申请", "上行", "区政府向市发改委申请活动经费5万元", "市发改委"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt does not include %q: %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"重新判别", "文种分类", "Top-N"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt must not ask downstream to reclassify, found %q in %s", forbidden, prompt)
		}
	}
}

func TestBuildGenerationRequestRoutesC07ToFallbackWithoutChangingSecurityLevel(t *testing.T) {
	scenario := doctype.ScenarioContext{
		TargetCapability:     doctype.CapabilityC07,
		Doctype:              "命令",
		Subtype:              "任免令",
		Direction:            doctype.DirectionDownward,
		Confidence:           0.88,
		SceneDescription:     "发布一则任免某同志职务的命令",
		ContentSecurityLevel: llm.ContentSecurityLevelClassified,
	}

	req, branch, err := BuildGenerationRequest(GenerationInput{Scenario: scenario})
	if err != nil {
		t.Fatalf("BuildGenerationRequest failed: %v", err)
	}
	if branch != BranchGenericFallback {
		t.Fatalf("branch = %q, want generic fallback", branch)
	}
	if req.ContentSecurityLevel != llm.ContentSecurityLevelClassified {
		t.Fatalf("security level = %q, want classified", req.ContentSecurityLevel)
	}
	prompt := joinedMessages(req.Messages)
	if !strings.Contains(prompt, "通用兜底") {
		t.Fatalf("fallback prompt must carry fallback marker: %s", prompt)
	}
	if strings.Contains(prompt, "重新判别") || strings.Contains(prompt, "无法处理") {
		t.Fatalf("fallback must consume settled c06 target without reclassification/dead-end wording: %s", prompt)
	}
}

func TestBuildGenerationRequestPreservesUnknownSecurityLevel(t *testing.T) {
	scenario := doctype.ScenarioContext{
		TargetCapability:     doctype.CapabilityC05,
		Doctype:              "通知",
		Subtype:              "召开会议",
		Direction:            doctype.DirectionDownward,
		SceneDescription:     "通知各科室周五开会",
		ContentSecurityLevel: llm.ContentSecurityLevelUnknown,
	}

	req, _, err := BuildGenerationRequest(GenerationInput{Scenario: scenario})
	if err != nil {
		t.Fatalf("BuildGenerationRequest failed: %v", err)
	}
	if req.ContentSecurityLevel != llm.ContentSecurityLevelUnknown {
		t.Fatalf("unknown security level must stay unknown for c02 fail-closed, got %q", req.ContentSecurityLevel)
	}
}

func TestBuildGenerationRequestRejectsUnsupportedCapability(t *testing.T) {
	_, _, err := BuildGenerationRequest(GenerationInput{
		Scenario: doctype.ScenarioContext{TargetCapability: doctype.TargetCapability("c99")},
	})
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("err = %v, want ErrUnsupportedCapability", err)
	}
}

func TestStreamDraftRejectsC05BeforeC01BecauseRBACRequiresHighFreqOrchestrator(t *testing.T) {
	client := &captureClient{}
	scenario := doctype.ScenarioContext{
		TargetCapability:     doctype.CapabilityC05,
		Doctype:              "报告",
		Subtype:              "工作报告",
		Direction:            doctype.DirectionUpward,
		SceneDescription:     "向上级报告专项工作进展",
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	}

	events, branch, err := StreamDraft(context.Background(), client, GenerationInput{Scenario: scenario})
	if !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("err = %v, want auth.ErrUnauthorized", err)
	}
	if events != nil || branch != "" {
		t.Fatalf("events/branch = %#v/%q, want nil/empty for rejected c05 stream", events, branch)
	}
	if client.calls != 0 {
		t.Fatalf("c05 must be rejected before c01 call, calls = %d", client.calls)
	}
}

func TestStreamDraftCallsC01ClientWithC06SecurityLevelForC07(t *testing.T) {
	client := &captureClient{}
	scenario := doctype.ScenarioContext{
		TargetCapability:     doctype.CapabilityC07,
		Doctype:              "命令",
		Subtype:              "任免令",
		Direction:            doctype.DirectionDownward,
		SceneDescription:     "发布一则任免某同志职务的命令",
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	}

	events, branch, err := StreamDraft(context.Background(), client, GenerationInput{Scenario: scenario})
	if err != nil {
		t.Fatalf("StreamDraft failed: %v", err)
	}
	event := <-events
	if event.Type != llm.StreamEventTypeDone || branch != BranchGenericFallback {
		t.Fatalf("event/branch = %#v/%q, want done/generic fallback", event, branch)
	}
	if client.calls != 1 {
		t.Fatalf("client calls = %d, want 1", client.calls)
	}
	if client.last.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("client request level = %q, want sensitive", client.last.ContentSecurityLevel)
	}
}

func joinedMessages(messages []llm.Message) string {
	var b strings.Builder
	for _, m := range messages {
		b.WriteString(string(m.Role))
		b.WriteString(":")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

type captureClient struct {
	calls int
	last  llm.ChatRequest
	err   error
}

func (c *captureClient) Complete(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	panic("not used")
}

func (c *captureClient) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	c.calls++
	c.last = req
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop}
	close(ch)
	return ch, c.err
}

func (c *captureClient) CurrentNetwork(context.Context) (llm.Network, error) {
	return llm.NetworkPrivate, nil
}
