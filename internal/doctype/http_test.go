package doctype

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeAuth struct {
	actorID string
	err     error
}

func (f fakeAuth) Authenticate(_ context.Context, token string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if token == "" {
		return "", errors.New("missing token")
	}
	return f.actorID, nil
}

func newTestHandler(candidates, slots string) *Handler {
	clf := NewClassifier(&pipelineFakeClient{candidates: candidates, slots: slots}, DefaultMatrix())
	return NewHandler(clf, NewMemorySlotStore(), NewMemoryThresholdStore(), fakeAuth{actorID: "actor-1"})
}

func doJSON(t *testing.T, h http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandlerClassifyDirectResult(t *testing.T) {
	h := newTestHandler(`[{"doctype":"通知","subtype":"召开会议","direction":"downward","confidence":0.95}]`, `{}`)
	rec := doJSON(t, h, "/api/doctype/classify", classifyRequest{Scene: "关于召开年度工作会议的通知", SecurityLevel: "unclassified"}, "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	var resp classifyResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.NeedsConfirmation || resp.Result == nil {
		t.Fatalf("resp = %#v, want direct result", resp)
	}
	if resp.Result.Doctype != "通知" || resp.Result.TargetCapability != "c05" {
		t.Fatalf("result = %#v, want 通知/c05", resp.Result)
	}
}

func TestHandlerClassifyReturnsCandidates(t *testing.T) {
	h := newTestHandler(`[{"doctype":"报告","subtype":"专项工作","direction":"","confidence":0.62},{"doctype":"请示","subtype":"回复意见","direction":"","confidence":0.6}]`, `{}`)
	rec := doJSON(t, h, "/api/doctype/classify", classifyRequest{Scene: "把某事项的处理情况向上级讲清楚", SecurityLevel: "unclassified"}, "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp classifyResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.NeedsConfirmation || len(resp.Candidates) != 2 {
		t.Fatalf("resp = %#v, want 2 candidates needing confirmation", resp)
	}
}

func TestHandlerClassifyRejectsEmptyScene(t *testing.T) {
	h := newTestHandler(`[]`, `{}`)
	rec := doJSON(t, h, "/api/doctype/classify", classifyRequest{Scene: "  ", SecurityLevel: "unclassified"}, "tok")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandlerRequiresAuthentication(t *testing.T) {
	h := newTestHandler(`[]`, `{}`)
	rec := doJSON(t, h, "/api/doctype/classify", classifyRequest{Scene: "关于召开会议的通知", SecurityLevel: "unclassified"}, "") // 无 token
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestHandlerClarifyAsksThenCompletes(t *testing.T) {
	// 首轮：抽取播种 3 个要素（缺关键事项）→ 追问关键事项。
	slots := `{"发文单位":"区政府","主送机关":"市发改委","事由":"申请活动经费"}`
	h := newTestHandler(`[]`, slots)
	rec := doJSON(t, h, "/api/doctype/clarify", clarifyRequest{
		Doctype: "请示", Subtype: "资金费用申请", Scene: "区政府向市发改委申请活动经费的请示",
		SecurityLevel: "sensitive", Filled: map[string]string{}, Round: 0, Skipped: false,
	}, "tok")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	var r1 clarifyResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &r1)
	if r1.Done || r1.AskingSlot != string(SlotKeyMatter) {
		t.Fatalf("r1 = %#v, want asking 关键事项", r1)
	}
	if len(r1.Filled) != 3 {
		t.Fatalf("seeded filled = %#v, want 3", r1.Filled)
	}

	// 次轮：前端补齐关键事项后再请求 → 放行并返回契约。
	filled := r1.Filled
	filled[string(SlotKeyMatter)] = "拨付 5 万元活动经费"
	rec2 := doJSON(t, h, "/api/doctype/clarify", clarifyRequest{
		Doctype: "请示", Subtype: "资金费用申请", Scene: "区政府向市发改委申请活动经费的请示",
		SecurityLevel: "sensitive", Filled: filled, Round: 1, Skipped: false,
	}, "tok")
	var r2 clarifyResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &r2)
	if !r2.Done || r2.Context == nil {
		t.Fatalf("r2 = %#v, want done with context", r2)
	}
	if r2.Context.TargetCapability != "c05" || r2.Context.Doctype != "请示" || r2.Context.Direction != "upward" {
		t.Fatalf("context = %#v, want c05/请示/upward", r2.Context)
	}
	if r2.Context.ContentSecurityLevel != "sensitive" {
		t.Fatalf("context level = %q, want sensitive", r2.Context.ContentSecurityLevel)
	}
	if len(r2.Context.MissingSlots) != 0 {
		t.Fatalf("context missing = %#v, want none", r2.Context.MissingSlots)
	}
}

func TestHandlerClarifySkipReturnsContextWithMissing(t *testing.T) {
	h := newTestHandler(`[]`, `{}`)
	rec := doJSON(t, h, "/api/doctype/clarify", clarifyRequest{
		Doctype: "请示", Subtype: "资金费用申请", Scene: "区政府申请经费的请示场景",
		SecurityLevel: "unclassified", Filled: map[string]string{"发文单位": "区政府"}, Round: 0, Skipped: true,
	}, "tok")
	var resp clarifyResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Done || resp.Context == nil {
		t.Fatalf("resp = %#v, want done with context", resp)
	}
	if len(resp.Context.MissingSlots) != 3 {
		t.Fatalf("missing = %#v, want 3 (主送机关/事由/关键事项)", resp.Context.MissingSlots)
	}
}

func TestHandlerClarifyRejectsEmptyDoctype(t *testing.T) {
	h := newTestHandler(`[]`, `{}`)
	rec := doJSON(t, h, "/api/doctype/clarify", clarifyRequest{
		Doctype: "", Subtype: "x", Scene: "区政府申请经费的请示场景", SecurityLevel: "unclassified",
		Filled: map[string]string{}, Round: 0,
	}, "tok")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
