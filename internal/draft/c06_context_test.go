package draft

import (
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestConsumeC06ScenarioContextAcceptsC05AndCarriesNineFields(t *testing.T) {
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
		MissingSlots:         []doctype.RequiredSlot{doctype.SlotTimePlace},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	}

	consumed, err := ConsumeC06ScenarioContext(scenario)
	if err != nil {
		t.Fatalf("consume c06 scenario context: %v", err)
	}
	if consumed.TargetCapability != doctype.CapabilityC05 {
		t.Fatalf("capability = %q, want c05", consumed.TargetCapability)
	}
	if consumed.Doctype != "请示" || consumed.Subtype != "资金费用申请" || consumed.Direction != doctype.DirectionUpward {
		t.Fatalf("doctype/subtype/direction = %q/%q/%q", consumed.Doctype, consumed.Subtype, consumed.Direction)
	}
	if consumed.Confidence != 0.93 || consumed.SceneDescription != "区政府向市发改委申请活动经费5万元" {
		t.Fatalf("confidence/scene = %v/%q", consumed.Confidence, consumed.SceneDescription)
	}
	if consumed.FilledSlots[doctype.SlotRecipient] != "市发改委" || consumed.MissingSlots[0] != doctype.SlotTimePlace {
		t.Fatalf("slots = %#v missing %#v", consumed.FilledSlots, consumed.MissingSlots)
	}
	if consumed.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("security level = %q, want sensitive", consumed.ContentSecurityLevel)
	}

	scenario.FilledSlots[doctype.SlotRecipient] = "改写单位"
	scenario.MissingSlots[0] = doctype.SlotSubject
	if consumed.FilledSlots[doctype.SlotRecipient] != "市发改委" {
		t.Fatalf("filled slots aliased caller map: %#v", consumed.FilledSlots)
	}
	if consumed.MissingSlots[0] != doctype.SlotTimePlace {
		t.Fatalf("missing slots aliased caller slice: %#v", consumed.MissingSlots)
	}
}

func TestConsumeC06ScenarioContextRejectsNonC05WithoutFallbackDecision(t *testing.T) {
	consumed, err := ConsumeC06ScenarioContext(doctype.ScenarioContext{
		TargetCapability:     doctype.CapabilityC07,
		Doctype:              "命令",
		Subtype:              "任免令",
		Direction:            doctype.DirectionDownward,
		SceneDescription:     "发布一则任免某同志职务的命令",
		ContentSecurityLevel: llm.ContentSecurityLevelClassified,
	})
	if !errors.Is(err, ErrScenarioNotForC05) {
		t.Fatalf("err = %v, want ErrScenarioNotForC05", err)
	}
	if consumed.TargetCapability != "" || consumed.Doctype != "" || consumed.ContentSecurityLevel != "" {
		t.Fatalf("non-c05 context should not be consumed or converted: %#v", consumed)
	}
}

func TestConsumeC06ScenarioContextPreservesUnknownSecurityLevelForFailClosed(t *testing.T) {
	consumed, err := ConsumeC06ScenarioContext(doctype.ScenarioContext{
		TargetCapability:     doctype.CapabilityC05,
		Doctype:              "通知",
		Subtype:              "召开会议",
		Direction:            doctype.DirectionDownward,
		SceneDescription:     "通知各科室周五开会",
		ContentSecurityLevel: llm.ContentSecurityLevelUnknown,
	})
	if err != nil {
		t.Fatalf("consume c06 scenario context: %v", err)
	}
	if consumed.ContentSecurityLevel != llm.ContentSecurityLevelUnknown {
		t.Fatalf("unknown security level must stay unknown for c02 fail-closed, got %q", consumed.ContentSecurityLevel)
	}
}
