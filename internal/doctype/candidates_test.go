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
	got := clf.ResolveSelection("请示", "组织成立", "区政府关于成立节能监测中心的事项")
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
