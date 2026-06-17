package doctype

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestMissingSlotsIdentifiesUnfilled(t *testing.T) {
	required := []RequiredSlot{SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter}
	filled := map[RequiredSlot]string{SlotIssuer: "区政府", SlotSubject: "申请活动经费", SlotRecipient: "  "}
	missing := MissingSlots(required, filled)
	want := []RequiredSlot{SlotRecipient, SlotKeyMatter} // Recipient 空白视为缺失
	if len(missing) != len(want) || missing[0] != want[0] || missing[1] != want[1] {
		t.Fatalf("missing = %#v, want %#v", missing, want)
	}
}

func TestNextClarificationReleasesWhenComplete(t *testing.T) {
	// 5.3：要素齐备直接放行，不追问。
	state := ClarificationState{Doctype: "请示", Required: []RequiredSlot{SlotIssuer, SlotSubject}, Filled: map[RequiredSlot]string{SlotIssuer: "区政府", SlotSubject: "申请经费"}, MaxRounds: 3}
	step := NextClarification(state)
	if !step.Done || len(step.MissingSlots) != 0 {
		t.Fatalf("step = %#v, want done with no missing", step)
	}
}

func TestNextClarificationAsksSpecificQuestionForMissing(t *testing.T) {
	// 5.4：针对第一项缺失要素发起具体追问（点名主送机关 + 文种），非泛泛补充。
	state := ClarificationState{Doctype: "请示", Required: []RequiredSlot{SlotRecipient, SlotKeyMatter}, Filled: map[RequiredSlot]string{}, MaxRounds: 3}
	step := NextClarification(state)
	if step.Done || step.AskingSlot != SlotRecipient {
		t.Fatalf("step = %#v, want asking 主送机关", step)
	}
	if !strings.Contains(step.Question, "主送") || !strings.Contains(step.Question, "请示") {
		t.Fatalf("question = %q, want specific 主送机关/请示", step.Question)
	}
}

func TestNextClarificationReleasesAtRoundLimit(t *testing.T) {
	// 5.5：达轮次上限即便仍缺失也停止追问、放行并标记缺失。
	state := ClarificationState{Doctype: "请示", Required: []RequiredSlot{SlotTimePlace}, Filled: map[RequiredSlot]string{}, Round: 3, MaxRounds: 3}
	step := NextClarification(state)
	if !step.Done || len(step.MissingSlots) != 1 || step.MissingSlots[0] != SlotTimePlace {
		t.Fatalf("step = %#v, want done marking 关键时间地点 missing", step)
	}
}

func TestNextClarificationReleasesOnSkip(t *testing.T) {
	// 5.6：用户显式跳过 → 放行并标记仍缺失要素。
	state := ClarificationState{Doctype: "请示", Required: []RequiredSlot{SlotRecipient}, Filled: map[RequiredSlot]string{}, MaxRounds: 3, Skipped: true}
	step := NextClarification(state)
	if !step.Done || len(step.MissingSlots) != 1 || step.MissingSlots[0] != SlotRecipient {
		t.Fatalf("step = %#v, want done marking 主送机关 missing", step)
	}
}

func TestFillSlotBackfillsAndConsumesRound(t *testing.T) {
	// 5.7：补齐回填到 Filled 且消耗一轮；原状态不被修改（值拷贝）。
	state := ClarificationState{Required: []RequiredSlot{SlotRecipient}, Filled: map[RequiredSlot]string{}, Round: 0, MaxRounds: 3}
	next := FillSlot(state, SlotRecipient, " 市发改委 ")
	if next.Filled[SlotRecipient] != "市发改委" {
		t.Fatalf("filled = %#v, want 市发改委 (trimmed)", next.Filled)
	}
	if next.Round != 1 {
		t.Fatalf("round = %d, want 1", next.Round)
	}
	if len(state.Filled) != 0 {
		t.Fatalf("original state mutated: %#v", state.Filled)
	}
}

func TestExtractSlotsParsesKnownElementsAndCarriesSecurityLevel(t *testing.T) {
	// 5.1：从场景抽取已知要素；空值与未登记键被过滤；内容密级透传供 c02。
	resp := llm.ChatResponse{Text: `{"发文单位":"区政府","主送机关":"市发改委","事由":"申请活动经费","关键事项":"","无关键":"x"}`}
	clf, fake := newTestClassifier(resp, nil)
	required := []RequiredSlot{SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter}

	got, err := clf.ExtractSlots(context.Background(), "区政府向市发改委申请活动经费5万元", required, llm.ContentSecurityLevelSensitive, "actor-1", "req-1")
	if err != nil {
		t.Fatalf("extract slots: %v", err)
	}
	want := map[RequiredSlot]string{SlotIssuer: "区政府", SlotRecipient: "市发改委", SlotSubject: "申请活动经费"}
	if len(got) != len(want) {
		t.Fatalf("slots = %#v, want %#v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("slot %q = %q, want %q", k, got[k], v)
		}
	}
	if fake.gotReq.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("ContentSecurityLevel = %q, want sensitive (透传 c02)", fake.gotReq.ContentSecurityLevel)
	}
}

func TestExtractSlotsRejectsEmptySceneBeforeModel(t *testing.T) {
	clf, fake := newTestClassifier(llm.ChatResponse{}, nil)
	if _, err := clf.ExtractSlots(context.Background(), "  ", []RequiredSlot{SlotIssuer}, llm.ContentSecurityLevelUnclassified, "u", "r"); !errors.Is(err, ErrEmptyScene) {
		t.Fatalf("error = %v, want ErrEmptyScene", err)
	}
	if fake.calls != 0 {
		t.Fatalf("model called %d times, want 0", fake.calls)
	}
}

func TestExtractSlotsPropagatesParseError(t *testing.T) {
	clf, _ := newTestClassifier(llm.ChatResponse{Text: `["不是对象"]`}, nil)
	if _, err := clf.ExtractSlots(context.Background(), "区政府申请经费的场景描述", []RequiredSlot{SlotIssuer}, llm.ContentSecurityLevelUnclassified, "u", "r"); !errors.Is(err, ErrInvalidSlotExtraction) {
		t.Fatalf("error = %v, want ErrInvalidSlotExtraction", err)
	}
}

func TestFillSlotEmptyValueConsumesRoundWithoutBackfill(t *testing.T) {
	// 空/空白补充不回填，但仍消耗一轮（该轮视为未补齐）。
	state := ClarificationState{Required: []RequiredSlot{SlotRecipient}, Filled: map[RequiredSlot]string{}, Round: 0, MaxRounds: 3}
	next := FillSlot(state, SlotRecipient, "   ")
	if _, ok := next.Filled[SlotRecipient]; ok {
		t.Fatalf("filled = %#v, want 主送机关 not backfilled (empty value)", next.Filled)
	}
	if next.Round != 1 {
		t.Fatalf("round = %d, want 1 (empty answer still consumes a round)", next.Round)
	}
}

func TestParseSlotExtractionStripsCodeFence(t *testing.T) {
	got, err := ParseSlotExtraction("```json\n{\"发文单位\":\"区政府\"}\n```", []RequiredSlot{SlotIssuer})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got[SlotIssuer] != "区政府" {
		t.Fatalf("slots = %#v, want 发文单位=区政府 (code fence stripped)", got)
	}
}

func TestParseSlotExtractionFiltersToRequired(t *testing.T) {
	got, err := ParseSlotExtraction(`{"发文单位":"区政府","主送机关":"市发改委"}`, []RequiredSlot{SlotIssuer})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[SlotIssuer] != "区政府" {
		t.Fatalf("slots = %#v, want only 发文单位", got)
	}
}

func TestBuildSlotExtractionPromptListsRequiredSlots(t *testing.T) {
	prompt := BuildSlotExtractionPrompt([]RequiredSlot{SlotIssuer, SlotRecipient})
	for _, must := range []string{"发文单位", "主送机关", "JSON", "不要臆造"} {
		if !strings.Contains(prompt, must) {
			t.Fatalf("prompt missing %q", must)
		}
	}
}
