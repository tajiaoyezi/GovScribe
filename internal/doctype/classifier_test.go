package doctype

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// fakeLLMClient 是 c01 窄抽象 llm.Client 的本地 fake，按约定用简单 struct 而非反射 mock。
type fakeLLMClient struct {
	resp   llm.ChatResponse
	err    error
	gotReq llm.ChatRequest
	calls  int
}

func (f *fakeLLMClient) Complete(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	f.gotReq = req
	f.calls++
	return f.resp, f.err
}

func (f *fakeLLMClient) Stream(_ context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return nil, nil
}

func (f *fakeLLMClient) CurrentNetwork(_ context.Context) (llm.Network, error) {
	return llm.NetworkPublic, nil
}

func newTestClassifier(resp llm.ChatResponse, err error) (*Classifier, *fakeLLMClient) {
	fake := &fakeLLMClient{resp: resp, err: err}
	return NewClassifier(fake, DefaultMatrix()), fake
}

func TestClassifyRejectsEmptyAndTooShortBeforeCallingModel(t *testing.T) {
	clf, fake := newTestClassifier(llm.ChatResponse{}, nil)

	if _, err := clf.Classify(context.Background(), "   ", llm.ContentSecurityLevelUnclassified, "u", "r"); !errors.Is(err, ErrEmptyScene) {
		t.Fatalf("empty error = %v, want ErrEmptyScene", err)
	}
	if _, err := clf.Classify(context.Background(), "通知", llm.ContentSecurityLevelUnclassified, "u", "r"); !errors.Is(err, ErrSceneDescriptionTooShort) {
		t.Fatalf("short error = %v, want ErrSceneDescriptionTooShort", err)
	}
	if fake.calls != 0 {
		t.Fatalf("model called %d times, want 0 (rejected before outbound call)", fake.calls)
	}
}

func TestClassifyParsesOutputAnnotatesDeepTierAndCarriesSecurityLevel(t *testing.T) {
	resp := llm.ChatResponse{Text: `{"doctype":"请示","subtype":"组织成立","direction":"upward","confidence":0.9}`}
	clf, fake := newTestClassifier(resp, nil)

	got, err := clf.Classify(context.Background(), "区政府想成立节能监测中心，需向上级请求批准", llm.ContentSecurityLevelSensitive, "actor-1", "req-1")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	want := ClassificationResult{Doctype: "请示", Subtype: "组织成立", Confidence: 0.9, Direction: DirectionUpward, Tier: TierDeep}
	if got != want {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
	// 2.3：经 c01 窄抽象发起、系统提示词随调用、内容密级透传供 c02 出站路由。
	if fake.gotReq.ContentSecurityLevel != llm.ContentSecurityLevelSensitive {
		t.Fatalf("ContentSecurityLevel = %q, want sensitive", fake.gotReq.ContentSecurityLevel)
	}
	if len(fake.gotReq.Messages) != 2 || fake.gotReq.Messages[0].Role != llm.RoleSystem || fake.gotReq.Messages[1].Role != llm.RoleUser {
		t.Fatalf("messages = %#v, want system+user", fake.gotReq.Messages)
	}
	if fake.gotReq.ActorID != "actor-1" || fake.gotReq.RequestID != "req-1" {
		t.Fatalf("actor/request = %q/%q, want actor-1/req-1", fake.gotReq.ActorID, fake.gotReq.RequestID)
	}
}

func TestClassifyAnnotatesBTableTier(t *testing.T) {
	resp := llm.ChatResponse{Text: `{"doctype":"命令","subtype":"任免令","direction":"","confidence":0.85}`}
	clf, _ := newTestClassifier(resp, nil)

	got, err := clf.Classify(context.Background(), "要发布一则任免某同志职务的命令", llm.ContentSecurityLevelUnclassified, "u", "r")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Tier != TierTemplateAssist {
		t.Fatalf("命令 tier = %q, want template_assist (B 表标注)", got.Tier)
	}
}

func TestClassifyResolvesDirectionAndPenalizesConflict(t *testing.T) {
	// 模型给「函」平行，但场景含「向上级请求批准」线索 → 规则改上行、与模型冲突 → 折减置信度。
	resp := llm.ChatResponse{Text: `{"doctype":"函","subtype":"请求批准","direction":"horizontal","confidence":0.8}`}
	clf, _ := newTestClassifier(resp, nil)

	got, err := clf.Classify(context.Background(), "向上级请求批准更新执法车辆事项", llm.ContentSecurityLevelUnclassified, "u", "r")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Direction != DirectionUpward {
		t.Fatalf("direction = %q, want upward (rule overrides model)", got.Direction)
	}
	if got.Confidence >= 0.8 {
		t.Fatalf("confidence = %v, want penalized below model 0.8", got.Confidence)
	}
}

func TestClassifyPropagatesModelAndParseErrors(t *testing.T) {
	modelErr := errors.New("upstream down")
	clf, _ := newTestClassifier(llm.ChatResponse{}, modelErr)
	if _, err := clf.Classify(context.Background(), "写一份关于召开年度会议的通知", llm.ContentSecurityLevelUnclassified, "u", "r"); !errors.Is(err, modelErr) {
		t.Fatalf("error = %v, want propagated model error", err)
	}

	clfBad, _ := newTestClassifier(llm.ChatResponse{Text: "这不是 JSON"}, nil)
	if _, err := clfBad.Classify(context.Background(), "写一份关于召开年度会议的通知", llm.ContentSecurityLevelUnclassified, "u", "r"); !errors.Is(err, ErrInvalidClassificationOutput) {
		t.Fatalf("error = %v, want ErrInvalidClassificationOutput", err)
	}
}

func TestClassifyMarksStarredRareSubtype(t *testing.T) {
	// A 表深做文种下的标黄稀缺子类：Tier 仍为 deep，但 IsStarredRare 标记应携出，供 §4 降级路由。
	resp := llm.ChatResponse{Text: `{"doctype":"方案","subtype":"调研方案","direction":"","confidence":0.75}`}
	clf, _ := newTestClassifier(resp, nil)

	got, err := clf.Classify(context.Background(), "关于推进我区社会治理创新试点工作的调研方案", llm.ContentSecurityLevelUnclassified, "u", "r")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Tier != TierDeep || !got.IsStarredRare {
		t.Fatalf("result = %#v, want deep + starred-rare", got)
	}
	// 标黄稀缺降级：路由（§4）据 Matrix.Resolve 得 c07，而非仅看 Tier=deep 误路由 c05。
	if _, target := NewMatrix(DefaultMatrix()).Resolve("方案", "调研方案"); target != CapabilityC07 {
		t.Fatalf("方案-调研方案 routing target = %q, want c07", target)
	}
}

func TestClassifyFallbackForGenericEscapeLabel(t *testing.T) {
	// 模型无法稳定归类时返回逃逸标签「通用公文」→ 兜底档，无死路。
	resp := llm.ChatResponse{Text: `{"doctype":"通用公文","subtype":"","direction":"","confidence":0.3}`}
	clf, _ := newTestClassifier(resp, nil)

	got, err := clf.Classify(context.Background(), "帮我写一份其它类别的材料说明有关情况", llm.ContentSecurityLevelUnclassified, "u", "r")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Tier != TierFallback || got.IsStarredRare {
		t.Fatalf("result = %#v, want fallback tier, non-starred", got)
	}
}

func TestClassifyDeepDoctypeWithUnknownSubtypeFallsBack(t *testing.T) {
	// A 表深做文种 + 未登记子类 → 命中文种级兜底档（Tier=fallback），而非误判 deep。
	resp := llm.ChatResponse{Text: `{"doctype":"通知","subtype":"某种未登记子类","direction":"downward","confidence":0.7}`}
	clf, _ := newTestClassifier(resp, nil)

	got, err := clf.Classify(context.Background(), "关于某项未登记事务的通知说明", llm.ContentSecurityLevelUnclassified, "u", "r")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Tier != TierFallback {
		t.Fatalf("tier = %q, want fallback (deep doctype + unknown subtype)", got.Tier)
	}
}

func TestClassifyDoesNotPenalizeWhenDirectionAgrees(t *testing.T) {
	// 请示默认上行、模型也给上行 → 不冲突，置信度不折减。
	resp := llm.ChatResponse{Text: `{"doctype":"请示","subtype":"资金费用申请","direction":"upward","confidence":0.88}`}
	clf, _ := newTestClassifier(resp, nil)

	got, err := clf.Classify(context.Background(), "区政府就活动经费事项报市发改委审定的请示", llm.ContentSecurityLevelUnclassified, "u", "r")
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if got.Direction != DirectionUpward {
		t.Fatalf("direction = %q, want upward", got.Direction)
	}
	if got.Confidence != 0.88 {
		t.Fatalf("confidence = %v, want unchanged 0.88 (no rule/model conflict)", got.Confidence)
	}
}
