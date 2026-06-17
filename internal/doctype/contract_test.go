package doctype

import (
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestBuildScenarioContextAssemblesNineFields(t *testing.T) {
	// 6.1：A 表深做结果 → 契约 9 项字段齐备、目标能力 c05。
	result := ClassificationResult{Doctype: "请示", Subtype: "组织成立", Direction: DirectionUpward, Tier: TierDeep, Confidence: 0.92}
	filled := map[RequiredSlot]string{SlotIssuer: "区政府", SlotRecipient: "市发改委"}
	ctx := BuildScenarioContext(result, "区政府关于成立节能监测中心的请示", filled, nil, llm.ContentSecurityLevelSensitive)

	if ctx.TargetCapability != CapabilityC05 {
		t.Fatalf("capability = %q, want c05", ctx.TargetCapability)
	}
	if ctx.Doctype != "请示" || ctx.Subtype != "组织成立" || ctx.Direction != DirectionUpward {
		t.Fatalf("doctype/subtype/direction = %q/%q/%q", ctx.Doctype, ctx.Subtype, ctx.Direction)
	}
	if ctx.Confidence != 0.92 {
		t.Fatalf("confidence = %v, want 0.92", ctx.Confidence)
	}
	if ctx.SceneDescription != "区政府关于成立节能监测中心的请示" {
		t.Fatalf("scene = %q", ctx.SceneDescription)
	}
	if ctx.FilledSlots[SlotIssuer] != "区政府" || ctx.FilledSlots[SlotRecipient] != "市发改委" {
		t.Fatalf("filled = %#v", ctx.FilledSlots)
	}
	if ctx.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("level = %q, want sensitive", ctx.ContentSecurityLevel)
	}
}

func TestBuildScenarioContextMarksMissingSlots(t *testing.T) {
	// 6.2：放行时仍缺失的要素在契约中标记移交。
	result := ClassificationResult{Doctype: "请示", Tier: TierDeep, Confidence: 0.8}
	ctx := BuildScenarioContext(result, "申请一笔活动经费的请示", nil, []RequiredSlot{SlotRecipient, SlotKeyMatter}, llm.ContentSecurityLevelUnclassified)
	if !ctx.HasUnconfirmedSlots() || len(ctx.MissingSlots) != 2 {
		t.Fatalf("missing = %#v, want [主送机关 关键事项]", ctx.MissingSlots)
	}
	if ctx.MissingSlots[0] != SlotRecipient || ctx.MissingSlots[1] != SlotKeyMatter {
		t.Fatalf("missing = %#v", ctx.MissingSlots)
	}
}

func TestBuildScenarioContextCarriesContentSecurityLevelWithoutDefaulting(t *testing.T) {
	// 6.3：内容密级原样承载；缺失/未知（""）不缺省「非密」，由下游 fail-closed。
	result := ClassificationResult{Doctype: "通知", Subtype: "召开会议", Tier: TierDeep, Confidence: 0.9}
	for _, level := range []llm.ContentSecurityLevel{
		llm.ContentSecurityLevelUnclassified,
		llm.ContentSecurityLevelSensitive,
		llm.ContentSecurityLevelClassified,
		llm.ContentSecurityLevelUnknown, // ""
	} {
		ctx := BuildScenarioContext(result, "关于召开年度会议的通知", nil, nil, level)
		if ctx.ContentSecurityLevel != level {
			t.Fatalf("level = %q, want %q (must not coerce, esp. unknown)", ctx.ContentSecurityLevel, level)
		}
		if ctx.OutboundSecurityLevel() != level {
			t.Fatalf("outbound = %q, want %q", ctx.OutboundSecurityLevel(), level)
		}
	}
	// 显式确认未知不被改写为非密。
	ctx := BuildScenarioContext(result, "关于召开年度会议的通知", nil, nil, llm.ContentSecurityLevelUnknown)
	if ctx.ContentSecurityLevel == llm.ContentSecurityLevelUnclassified {
		t.Fatalf("unknown level wrongly defaulted to unclassified")
	}
}

func TestBuildScenarioContextRoutesStarredRareToC07(t *testing.T) {
	// 契约目标能力与路由口径一致：标黄稀缺 → c07。
	result := ClassificationResult{Doctype: "方案", Subtype: "调研方案", Tier: TierDeep, IsStarredRare: true, Confidence: 0.7}
	ctx := BuildScenarioContext(result, "关于推进试点工作的调研方案", nil, nil, llm.ContentSecurityLevelUnclassified)
	if ctx.TargetCapability != CapabilityC07 {
		t.Fatalf("capability = %q, want c07 (starred rare)", ctx.TargetCapability)
	}
}

func TestBuildScenarioContextCopiesSlotCollections(t *testing.T) {
	// 契约对已补齐/缺失要素做拷贝，调用方后续修改不应影响契约。
	result := ClassificationResult{Doctype: "请示", Tier: TierDeep, Confidence: 0.8}
	filled := map[RequiredSlot]string{SlotIssuer: "区政府"}
	missing := []RequiredSlot{SlotRecipient}
	ctx := BuildScenarioContext(result, "申请经费的请示场景", filled, missing, llm.ContentSecurityLevelUnclassified)

	filled[SlotIssuer] = "改了"
	missing[0] = SlotSubject
	if ctx.FilledSlots[SlotIssuer] != "区政府" {
		t.Fatalf("filled aliased: %#v", ctx.FilledSlots)
	}
	if ctx.MissingSlots[0] != SlotRecipient {
		t.Fatalf("missing aliased: %#v", ctx.MissingSlots)
	}
}
