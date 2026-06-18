package doctype

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// TestPromptsAreDistinguishableForFakeRouting 固定 pipelineFakeClient 的提示词分流前提：
// 仅要素抽取提示词含「抽取」、判别/候选提示词不含——若未来提示词措辞变动破坏该前提，本测试先行失败告警，
// 避免端到端 fake 静默误分流导致假阳性通过。
func TestPromptsAreDistinguishableForFakeRouting(t *testing.T) {
	if !strings.Contains(BuildSlotExtractionPrompt([]RequiredSlot{SlotIssuer}), "抽取") {
		t.Fatalf("slot extraction prompt must contain 抽取 (fake routing key)")
	}
	if strings.Contains(BuildClassificationPrompt(defaultMatrix()), "抽取") {
		t.Fatalf("classification prompt must not contain 抽取 (would break fake routing)")
	}
	if strings.Contains(BuildCandidatesPrompt(defaultMatrix(), 3), "抽取") {
		t.Fatalf("candidates prompt must not contain 抽取 (would break fake routing)")
	}
}

func TestPipelineRejectsEmptyScene(t *testing.T) {
	// 9.1：空场景在管道入口即被拒（不进入判别/路由）。
	clf := NewClassifier(&pipelineFakeClient{candidates: `[]`, slots: `{}`}, DefaultMatrix())
	if _, err := clf.ClassifyCandidates(context.Background(), "   ", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds()); !errors.Is(err, ErrEmptyScene) {
		t.Fatalf("error = %v, want ErrEmptyScene", err)
	}
}

// pipelineFakeClient 按提示词类型返回不同响应，支撑端到端串联：判别/候选调用返回候选数组，要素抽取调用返回要素对象。
type pipelineFakeClient struct {
	candidates string
	slots      string
}

func (f *pipelineFakeClient) Complete(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	sys := ""
	if len(req.Messages) > 0 {
		sys = req.Messages[0].Content
	}
	if strings.Contains(sys, "抽取") { // 要素抽取提示词
		return llm.ChatResponse{Text: f.slots}, nil
	}
	return llm.ChatResponse{Text: f.candidates}, nil // 判别 / 候选提示词
}

func (f *pipelineFakeClient) Stream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return nil, nil
}

func (f *pipelineFakeClient) CurrentNetwork(_ context.Context) (llm.Network, error) {
	return llm.NetworkPrivate, nil
}

// TestPipelineEndToEndProducesContract 9.3：场景输入 → 判别 → 路由 → 要素校验 → 一轮澄清补齐 → 契约移交，全链路产出正确契约。
func TestPipelineEndToEndProducesContract(t *testing.T) {
	candidates := `[{"doctype":"请示","subtype":"资金费用申请","direction":"upward","confidence":0.92}]`
	slots := `{"发文单位":"区政府","主送机关":"市发改委","事由":"申请活动经费"}` // 缺「关键事项」
	clf := NewClassifier(&pipelineFakeClient{candidates: candidates, slots: slots}, DefaultMatrix())
	slotStore := NewMemorySlotStore()
	th := defaultThresholds()
	ctx := context.Background()
	scene := "区政府向市发改委申请活动经费的请示"
	level := llm.ContentSecurityLevelSensitive

	// 1) 判别 + 候选决策（高置信单义 → 直选）
	dec, err := clf.ClassifyCandidates(ctx, scene, level, "u", "r", th)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if dec.NeedsConfirmation {
		t.Fatalf("want direct select for high-confidence single")
	}
	result := dec.Result

	// 2) 路由
	if label := Route(result); label.TargetCapability != CapabilityC05 {
		t.Fatalf("route target = %q, want c05", label.TargetCapability)
	}

	// 3) 要素抽取
	required, _ := slotStore.RequiredSlots(ctx, result.Doctype, result.Direction)
	filled, err := clf.ExtractSlots(ctx, scene, required, level, "u", "r")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	// 4) 澄清循环：缺「关键事项」→ 一轮补齐 → 放行
	state := ClarificationState{Doctype: result.Doctype, Direction: result.Direction, Required: required, Filled: filled, MaxRounds: th.MaxClarifyRounds}
	rounds := 0
	for {
		step := NextClarification(state)
		if step.Done {
			break
		}
		rounds++
		state = FillSlot(state, step.AskingSlot, "拨付 5 万元活动经费")
		if rounds > th.MaxClarifyRounds+1 {
			t.Fatalf("clarification did not terminate")
		}
	}
	if rounds != 1 {
		t.Fatalf("clarification rounds = %d, want 1 (only 关键事项 missing)", rounds)
	}

	// 5) 契约移交
	missing := MissingSlots(state.Required, state.Filled)
	contract := BuildScenarioContext(result, scene, state.Filled, missing, level)
	if contract.TargetCapability != CapabilityC05 || contract.Doctype != "请示" || contract.Direction != DirectionUpward {
		t.Fatalf("contract = %#v, want c05/请示/upward", contract)
	}
	if contract.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("contract level = %q, want sensitive", contract.ContentSecurityLevel)
	}
	if contract.HasUnconfirmedSlots() {
		t.Fatalf("contract still has unconfirmed slots: %#v", contract.MissingSlots)
	}
	for _, s := range []RequiredSlot{SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter} {
		if contract.FilledSlots[s] == "" {
			t.Fatalf("contract missing filled slot %q: %#v", s, contract.FilledSlots)
		}
	}
}

// TestPipelineAmbiguousThenUserSelects 9.1：多义场景返回候选 → 用户改选 → 路由 + 契约以用户选择为准。
func TestPipelineAmbiguousThenUserSelects(t *testing.T) {
	candidates := `[{"doctype":"报告","subtype":"专项工作","direction":"","confidence":0.62},{"doctype":"请示","subtype":"回复意见","direction":"","confidence":0.6}]`
	clf := NewClassifier(&pipelineFakeClient{candidates: candidates, slots: `{}`}, DefaultMatrix())
	ctx := context.Background()
	scene := "把某事项的处理情况向上级讲清楚并请其定夺"

	dec, err := clf.ClassifyCandidates(ctx, scene, llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds())
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if !dec.NeedsConfirmation || len(dec.Candidates) != 2 {
		t.Fatalf("decision = %#v, want confirmation with 2 candidates", dec)
	}
	// 用户改选「请示」（模型 Top-1 为「报告」）
	selected, err := clf.ResolveSelection("请示", "回复意见", scene)
	if err != nil {
		t.Fatalf("resolve selection: %v", err)
	}
	contract := BuildScenarioContext(selected, scene, nil, nil, llm.ContentSecurityLevelUnclassified)
	if contract.Doctype != "请示" || contract.TargetCapability != CapabilityC05 {
		t.Fatalf("contract = %#v, want 请示/c05 per user selection", contract)
	}
}

// TestPipelineScenarioRouting 9.1：spec 命名场景的分流口径（A 表→c05；标黄稀缺 / B 表 / 无法归类→c07）。
func TestPipelineScenarioRouting(t *testing.T) {
	cases := []struct {
		name       string
		candidate  string
		wantTarget TargetCapability
	}{
		{"通知-召开会议→c05", `[{"doctype":"通知","subtype":"召开会议","direction":"downward","confidence":0.95}]`, CapabilityC05},
		{"方案-调研方案(标黄)→c07", `[{"doctype":"方案","subtype":"调研方案","direction":"","confidence":0.95}]`, CapabilityC07},
		{"命令(B表)→c07", `[{"doctype":"命令","subtype":"任免令","direction":"","confidence":0.95}]`, CapabilityC07},
		{"通用公文(无法归类)→c07", `[{"doctype":"通用公文","subtype":"","direction":"","confidence":0.95}]`, CapabilityC07},
	}
	ctx := context.Background()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clf := NewClassifier(&pipelineFakeClient{candidates: c.candidate, slots: `{}`}, DefaultMatrix())
			dec, err := clf.ClassifyCandidates(ctx, "关于某具体事项的写作场景描述", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds())
			if err != nil {
				t.Fatalf("classify: %v", err)
			}
			if dec.NeedsConfirmation {
				t.Fatalf("want direct select (high confidence single)")
			}
			if got := Route(dec.Result).TargetCapability; got != c.wantTarget {
				t.Fatalf("target = %q, want %q", got, c.wantTarget)
			}
		})
	}
}

// TestPipelineClarificationSkipMarksMissing 9.2：用户跳过澄清 → 放行，契约标记仍缺失要素供下游谨慎。
func TestPipelineClarificationSkipMarksMissing(t *testing.T) {
	result := ClassificationResult{Doctype: "请示", Subtype: "资金费用申请", Direction: DirectionUpward, Tier: TierDeep, Confidence: 0.9}
	required := []RequiredSlot{SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter}
	state := ClarificationState{Doctype: "请示", Direction: DirectionUpward, Required: required, Filled: map[RequiredSlot]string{SlotIssuer: "区政府"}, MaxRounds: 3, Skipped: true}

	step := NextClarification(state)
	if !step.Done {
		t.Fatalf("skip should release immediately")
	}
	contract := BuildScenarioContext(result, "区政府申请经费的请示", state.Filled, step.MissingSlots, llm.ContentSecurityLevelUnclassified)
	if !contract.HasUnconfirmedSlots() || len(contract.MissingSlots) != 3 {
		t.Fatalf("contract missing = %#v, want 3 unconfirmed (主送机关/事由/关键事项)", contract.MissingSlots)
	}
}

// TestPipelineClarificationRoundLimitReleases 9.2：达轮次上限即便未补齐也放行，并标记缺失。
func TestPipelineClarificationRoundLimitReleases(t *testing.T) {
	required := []RequiredSlot{SlotIssuer, SlotRecipient}
	state := ClarificationState{Doctype: "请示", Required: required, Filled: map[RequiredSlot]string{}, MaxRounds: 2}
	rounds := 0
	for {
		step := NextClarification(state)
		if step.Done {
			if len(step.MissingSlots) == 0 {
				t.Fatalf("expected missing slots marked at release")
			}
			break
		}
		rounds++
		// 用户每轮都不补齐（空答复）
		state = FillSlot(state, step.AskingSlot, "")
		if rounds > 5 {
			t.Fatalf("round limit not enforced")
		}
	}
	if rounds != 2 {
		t.Fatalf("rounds = %d, want 2 (MaxRounds)", rounds)
	}
}

// TestPipelineRollbackManualSelection 9.4：停用判别（手选文种）回滚路径——不经判别，用户手选文种 → 路由 + 契约仍有效、无数据损失。
func TestPipelineRollbackManualSelection(t *testing.T) {
	// 不调用 ClassifyCandidates；直接以用户手选文种走 ResolveSelection → Route → 契约。
	clf := NewClassifier(&pipelineFakeClient{candidates: `[]`, slots: `{}`}, DefaultMatrix())
	scene := "关于召开年度工作会议的通知场景"
	selected, err := clf.ResolveSelection("通知", "召开会议", scene)
	if err != nil {
		t.Fatalf("manual selection: %v", err)
	}
	contract := BuildScenarioContext(selected, scene, nil, nil, llm.ContentSecurityLevelUnclassified)
	if contract.TargetCapability != CapabilityC05 || contract.Doctype != "通知" {
		t.Fatalf("rollback contract = %#v, want c05/通知 (config & contract preserved)", contract)
	}
	if contract.Confidence != 1.0 {
		t.Fatalf("manual selection confidence = %v, want 1.0", contract.Confidence)
	}
}
