package draft

import (
	"errors"
	"reflect"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestNewHighFreqDraftRequestConsumesC06ContextOnlyForC05(t *testing.T) {
	request, err := NewHighFreqDraftRequest(HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
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
			MissingSlots:         []doctype.RequiredSlot{doctype.SlotTimePlace},
			ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
		},
		ActorID:   "actor-1",
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("new high frequency draft request: %v", err)
	}
	if request.ActorID != "actor-1" || request.RequestID != "req-1" {
		t.Fatalf("actor/request = %q/%q, want actor-1/req-1", request.ActorID, request.RequestID)
	}
	ctx := request.Context
	if ctx.TargetCapability != doctype.CapabilityC05 || ctx.Doctype != "请示" || ctx.Subtype != "资金费用申请" {
		t.Fatalf("consumed context identifiers = %#v", ctx)
	}
	if ctx.Direction != doctype.DirectionUpward || ctx.Confidence != 0.93 {
		t.Fatalf("direction/confidence = %q/%v", ctx.Direction, ctx.Confidence)
	}
	if ctx.FilledSlots[doctype.SlotRecipient] != "市发改委" || ctx.MissingSlots[0] != doctype.SlotTimePlace {
		t.Fatalf("slots = %#v missing %#v", ctx.FilledSlots, ctx.MissingSlots)
	}
	if ctx.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("security level = %q, want sensitive", ctx.ContentSecurityLevel)
	}
}

func TestNewHighFreqDraftRequestRejectsNonC05WithoutFallback(t *testing.T) {
	request, err := NewHighFreqDraftRequest(HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability:     doctype.CapabilityC07,
			Doctype:              "命令",
			Subtype:              "任免令",
			Direction:            doctype.DirectionDownward,
			SceneDescription:     "发布一则任免某同志职务的命令",
			ContentSecurityLevel: llm.ContentSecurityLevelClassified,
		},
	})
	if !errors.Is(err, ErrScenarioNotForC05) {
		t.Fatalf("err = %v, want ErrScenarioNotForC05", err)
	}
	if request.Context.TargetCapability != "" || request.Context.Doctype != "" {
		t.Fatalf("non-c05 request should not be consumed or converted: %#v", request)
	}
}

func TestHighFreqDraftResponseEchoesContextAndStructuredBody(t *testing.T) {
	request, err := NewHighFreqDraftRequest(HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability:     doctype.CapabilityC05,
			Doctype:              "通知",
			Subtype:              "召开会议",
			Direction:            doctype.DirectionDownward,
			Confidence:           0.88,
			SceneDescription:     "通知各部门召开年度会议",
			ContentSecurityLevel: llm.ContentSecurityLevelUnclassified,
		},
		RequestID: "req-2",
	})
	if err != nil {
		t.Fatalf("new high frequency draft request: %v", err)
	}
	body := StructuredDraftBody{
		Title:      "关于召开年度会议的通知",
		Salutation: "各部门：",
		Recipient:  "各部门",
		Paragraphs: []string{"经研究，定于本周五召开年度会议。", "请各部门按时参会。"},
		Signature:  "市政府办公室\n2026年6月19日",
	}

	response := NewHighFreqDraftResponse(request, body)
	if response.Body.Title != body.Title || response.Body.Recipient != body.Recipient || len(response.Body.Paragraphs) != 2 {
		t.Fatalf("response body = %#v, want structured body", response.Body)
	}
	if response.Context.RequestID != "req-2" || response.Context.TargetCapability != doctype.CapabilityC05 {
		t.Fatalf("response context = %#v", response.Context)
	}
	if response.Context.Doctype != "通知" || response.Context.Subtype != "召开会议" || response.Context.Direction != doctype.DirectionDownward {
		t.Fatalf("response context identifiers = %#v", response.Context)
	}
	if response.Context.ContentSecurityLevel != llm.ContentSecurityLevelUnclassified {
		t.Fatalf("response security level = %q", response.Context.ContentSecurityLevel)
	}

	body.Paragraphs[0] = "调用方后续改写"
	if response.Body.Paragraphs[0] != "经研究，定于本周五召开年度会议。" {
		t.Fatalf("response body aliased caller paragraphs: %#v", response.Body.Paragraphs)
	}
}

func TestHighFreqDraftContractDoesNotCarryClassificationOrLayoutFields(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(HighFreqDraftRequest{}),
		reflect.TypeOf(HighFreqDraftResponse{}),
		reflect.TypeOf(HighFreqDraftResponseContext{}),
	} {
		for _, forbidden := range []string{"Tier", "IsStarredRare", "CapabilityTier", "FallbackDecision", "FontSize", "RedHead", "GBT9704"} {
			if _, ok := typ.FieldByName(forbidden); ok {
				t.Fatalf("%s must not carry c06 classification/layout field %s", typ.Name(), forbidden)
			}
		}
	}
}
