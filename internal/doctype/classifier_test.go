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
