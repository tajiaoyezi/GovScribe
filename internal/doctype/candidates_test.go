package doctype

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestClassifyCandidatesReturnsRankedTopNForAmbiguous(t *testing.T) {
	// 模型返回乱序两候选（请示 0.6、报告 0.7），Top-1 与 Top-2 差 0.1 < 多义间距 0.15 → 进入候选分支。
	resp := llm.ChatResponse{Text: `[{"doctype":"请示","subtype":"回复意见","direction":"","confidence":0.6},{"doctype":"报告","subtype":"专项工作","direction":"","confidence":0.7}]`}
	clf, _ := newTestClassifier(resp, nil)

	dec, err := clf.ClassifyCandidates(context.Background(), "就某事项的处理情况作出说明", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds())
	if err != nil {
		t.Fatalf("classify candidates: %v", err)
	}
	if !dec.NeedsConfirmation {
		t.Fatalf("NeedsConfirmation = false, want true (ambiguous)")
	}
	if len(dec.Candidates) != 2 || dec.Candidates[0].Doctype != "报告" || dec.Candidates[1].Doctype != "请示" {
		t.Fatalf("candidates = %#v, want 报告 then 请示 (desc by confidence)", dec.Candidates)
	}
}

func TestClassifyCandidatesDirectSelectsHighConfidenceSingle(t *testing.T) {
	resp := llm.ChatResponse{Text: `[{"doctype":"通知","subtype":"召开会议","direction":"downward","confidence":0.95}]`}
	clf, _ := newTestClassifier(resp, nil)

	dec, err := clf.ClassifyCandidates(context.Background(), "关于召开年度工作会议的通知", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds())
	if err != nil {
		t.Fatalf("classify candidates: %v", err)
	}
	if dec.NeedsConfirmation {
		t.Fatalf("NeedsConfirmation = true, want false (high-confidence single)")
	}
	if dec.Result.Doctype != "通知" || dec.Result.Tier != TierDeep {
		t.Fatalf("result = %#v, want 通知 deep", dec.Result)
	}
}

func TestClassifyCandidatesLowConfidenceTriggersConfirmation(t *testing.T) {
	resp := llm.ChatResponse{Text: `[{"doctype":"报告","subtype":"专项工作","direction":"","confidence":0.4}]`}
	clf, _ := newTestClassifier(resp, nil)

	dec, err := clf.ClassifyCandidates(context.Background(), "就某项工作的进展情况作出说明汇报", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds())
	if err != nil {
		t.Fatalf("classify candidates: %v", err)
	}
	if !dec.NeedsConfirmation {
		t.Fatalf("NeedsConfirmation = false, want true (confidence below threshold)")
	}
	if len(dec.Candidates) != 1 || dec.Candidates[0].Doctype != "报告" {
		t.Fatalf("candidates = %#v, want [报告]", dec.Candidates)
	}
}

func TestResolveSelectionOverridesModelChoice(t *testing.T) {
	// 模型 Top-1 可能为「报告」，用户改选「请示」→ 以用户选择为最终文种继续。
	clf, _ := newTestClassifier(llm.ChatResponse{}, nil)
	got, err := clf.ResolveSelection("请示", "组织成立", "区政府关于成立节能监测中心的事项")
	if err != nil {
		t.Fatalf("resolve selection: %v", err)
	}
	if got.Doctype != "请示" || got.Subtype != "组织成立" {
		t.Fatalf("selection = %#v, want 请示-组织成立", got)
	}
	if got.Tier != TierDeep || got.Direction != DirectionUpward {
		t.Fatalf("selection = %#v, want deep + upward", got)
	}
	if got.Confidence != 1.0 {
		t.Fatalf("confidence = %v, want 1.0 (user-confirmed)", got.Confidence)
	}
}

func TestResolveSelectionAcrossMatrixTiers(t *testing.T) {
	// 用户可改选任意候选，覆盖 B 表 / 标黄稀缺 / A 表未登记子类各路径。
	clf, _ := newTestClassifier(llm.ChatResponse{}, nil)
	scene := "关于某具体事项的写作场景描述"
	cases := []struct {
		doctype, subtype string
		wantTier         CapabilityTier
		wantStarred      bool
	}{
		{"命令", "任免令", TierTemplateAssist, false}, // B 表
		{"方案", "调研方案", TierDeep, true},           // A 表标黄稀缺
		{"通知", "某未登记子类", TierFallback, false},    // A 表深做 + 未登记子类 → 兜底
	}
	for _, c := range cases {
		got, err := clf.ResolveSelection(c.doctype, c.subtype, scene)
		if err != nil {
			t.Fatalf("%s-%s: %v", c.doctype, c.subtype, err)
		}
		if got.Tier != c.wantTier || got.IsStarredRare != c.wantStarred {
			t.Fatalf("%s-%s = tier %q starred %v, want %q %v", c.doctype, c.subtype, got.Tier, got.IsStarredRare, c.wantTier, c.wantStarred)
		}
		if got.Confidence != 1.0 {
			t.Fatalf("%s-%s confidence = %v, want 1.0", c.doctype, c.subtype, got.Confidence)
		}
	}
}

func TestResolveSelectionRejectsInvalidInput(t *testing.T) {
	clf, _ := newTestClassifier(llm.ChatResponse{}, nil)
	if _, err := clf.ResolveSelection("", "x", "关于某事项的场景描述"); !errors.Is(err, ErrEmptyDoctypeSelection) {
		t.Fatalf("empty doctype error = %v, want ErrEmptyDoctypeSelection", err)
	}
	if _, err := clf.ResolveSelection("请示", "组织成立", "  "); !errors.Is(err, ErrEmptyScene) {
		t.Fatalf("empty scene error = %v, want ErrEmptyScene", err)
	}
}

func TestResolveSelectionTrimsWhitespace(t *testing.T) {
	// 含空格的用户选择不得破坏分级表精确匹配（否则误降级为兜底）。
	clf, _ := newTestClassifier(llm.ChatResponse{}, nil)
	got, err := clf.ResolveSelection("  通知  ", "  召开会议  ", "关于召开年度工作会议的场景描述")
	if err != nil {
		t.Fatalf("resolve selection: %v", err)
	}
	if got.Doctype != "通知" || got.Subtype != "召开会议" {
		t.Fatalf("selection = %q/%q, want 通知/召开会议 (trimmed)", got.Doctype, got.Subtype)
	}
	if got.Tier != TierDeep {
		t.Fatalf("tier = %q, want deep (whitespace must not break matrix lookup)", got.Tier)
	}
}

func TestClassifyCandidatesDeduplicatesIdenticalCandidates(t *testing.T) {
	// 模型返回两条相同 (文种,子类)，去重后只剩一条 → 不应制造假多义而要求确认。
	resp := llm.ChatResponse{Text: `[{"doctype":"通知","subtype":"召开会议","direction":"downward","confidence":0.7},{"doctype":"通知","subtype":"召开会议","direction":"downward","confidence":0.65}]`}
	clf, _ := newTestClassifier(resp, nil)

	dec, err := clf.ClassifyCandidates(context.Background(), "关于召开年度工作会议的通知", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds())
	if err != nil {
		t.Fatalf("classify candidates: %v", err)
	}
	if dec.NeedsConfirmation {
		t.Fatalf("NeedsConfirmation = true, want false (duplicate collapses to single high-confidence)")
	}
	if dec.Result.Doctype != "通知" {
		t.Fatalf("result = %#v, want 通知", dec.Result)
	}
}

func TestClassifyCandidatesClampsZeroTopN(t *testing.T) {
	// TopN=0 应被夹为 1：即便模型返回多个候选，需确认时也只保留 1 个。
	resp := llm.ChatResponse{Text: `[{"doctype":"报告","subtype":"","direction":"","confidence":0.5},{"doctype":"请示","subtype":"","direction":"","confidence":0.45}]`}
	clf, _ := newTestClassifier(resp, nil)
	th := Thresholds{ConfidenceThreshold: 0.6, AmbiguityGap: 0.15, TopN: 0, MaxClarifyRounds: 3}

	dec, err := clf.ClassifyCandidates(context.Background(), "就某事项的处理情况作出说明", llm.ContentSecurityLevelUnclassified, "u", "r", th)
	if err != nil {
		t.Fatalf("classify candidates: %v", err)
	}
	if !dec.NeedsConfirmation {
		t.Fatalf("want NeedsConfirmation (low confidence)")
	}
	if len(dec.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1 (TopN=0 clamped to 1)", len(dec.Candidates))
	}
}

func TestClassifyCandidatesRejectsEmptySceneBeforeModel(t *testing.T) {
	clf, fake := newTestClassifier(llm.ChatResponse{}, nil)
	if _, err := clf.ClassifyCandidates(context.Background(), "  ", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds()); !errors.Is(err, ErrEmptyScene) {
		t.Fatalf("error = %v, want ErrEmptyScene", err)
	}
	if fake.calls != 0 {
		t.Fatalf("model called %d times, want 0", fake.calls)
	}
}

func TestClassifyCandidatesPropagatesParseError(t *testing.T) {
	clf, _ := newTestClassifier(llm.ChatResponse{Text: `{"doctype":"通知"}`}, nil) // 对象而非数组 → 解析失败
	if _, err := clf.ClassifyCandidates(context.Background(), "关于召开年度工作会议的通知", llm.ContentSecurityLevelUnclassified, "u", "r", defaultThresholds()); !errors.Is(err, ErrInvalidClassificationOutput) {
		t.Fatalf("error = %v, want ErrInvalidClassificationOutput", err)
	}
}

func TestNeedsConfirmationBoundaries(t *testing.T) {
	th := defaultThresholds() // ConfidenceThreshold 0.6, AmbiguityGap 0.15
	cases := []struct {
		name    string
		results []ClassificationResult
		want    bool
	}{
		{"置信度恰等于阈值 → 直选", []ClassificationResult{{Confidence: 0.6}}, false},
		{"置信度略低于阈值 → 候选", []ClassificationResult{{Confidence: 0.59}}, true},
		// 注：恰好等于多义间距受浮点表示影响，故验证明显高于/低于间距两侧而非精确相等点。
		{"间距明显大于多义间距 → 直选", []ClassificationResult{{Confidence: 0.9}, {Confidence: 0.7}}, false},
		{"间距明显小于多义间距 → 候选", []ClassificationResult{{Confidence: 0.9}, {Confidence: 0.8}}, true},
		{"空候选 → 候选（fail-safe）", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := needsConfirmation(c.results, th); got != c.want {
				t.Fatalf("needsConfirmation = %v, want %v", got, c.want)
			}
		})
	}
}

func TestClassifyCandidatesTruncatesToTopN(t *testing.T) {
	// 4 个低置信候选 + TopN=2 → 触发确认并按降序截断为 2。
	resp := llm.ChatResponse{Text: `[{"doctype":"通知","subtype":"","direction":"","confidence":0.5},{"doctype":"通报","subtype":"","direction":"","confidence":0.45},{"doctype":"报告","subtype":"","direction":"","confidence":0.4},{"doctype":"请示","subtype":"","direction":"","confidence":0.35}]`}
	clf, _ := newTestClassifier(resp, nil)
	th := Thresholds{ConfidenceThreshold: 0.6, AmbiguityGap: 0.15, TopN: 2, MaxClarifyRounds: 3}

	dec, err := clf.ClassifyCandidates(context.Background(), "就某事项的处理情况作出说明", llm.ContentSecurityLevelUnclassified, "u", "r", th)
	if err != nil {
		t.Fatalf("classify candidates: %v", err)
	}
	if !dec.NeedsConfirmation {
		t.Fatalf("want NeedsConfirmation")
	}
	if len(dec.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2 (truncated to TopN)", len(dec.Candidates))
	}
	if dec.Candidates[0].Doctype != "通知" || dec.Candidates[1].Doctype != "通报" {
		t.Fatalf("top2 = %#v, want 通知,通报", dec.Candidates)
	}
}

func TestClassifyCandidatesKeepsAllWhenCountEqualsTopN(t *testing.T) {
	resp := llm.ChatResponse{Text: `[{"doctype":"通知","subtype":"","direction":"","confidence":0.5},{"doctype":"通报","subtype":"","direction":"","confidence":0.45},{"doctype":"报告","subtype":"","direction":"","confidence":0.4}]`}
	clf, _ := newTestClassifier(resp, nil)
	th := Thresholds{ConfidenceThreshold: 0.6, AmbiguityGap: 0.15, TopN: 3, MaxClarifyRounds: 3}

	dec, err := clf.ClassifyCandidates(context.Background(), "就某事项的处理情况作出说明", llm.ContentSecurityLevelUnclassified, "u", "r", th)
	if err != nil {
		t.Fatalf("classify candidates: %v", err)
	}
	if len(dec.Candidates) != 3 {
		t.Fatalf("candidates = %d, want 3 (count == TopN, no truncation)", len(dec.Candidates))
	}
}
