package doctype

import (
	"context"
	"reflect"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// goDirectProviderStub 表示信创阶段 c01 切「Go 直连官方 SDK」后的底层供应商：与公网 provider 是
// 不同的 llm.Client 具体实现（不同类型、CurrentNetwork 报私有线），但对同一 ChatRequest 返回相同模型输出。
// 用于在组件级证明 c06 仅经 c01 窄抽象接口发起调用、不感知底层供应商实现。
type goDirectProviderStub struct {
	resp   llm.ChatResponse
	gotReq llm.ChatRequest
}

func (s *goDirectProviderStub) Complete(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	s.gotReq = req
	return s.resp, nil
}

func (s *goDirectProviderStub) Stream(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return nil, nil
}

func (s *goDirectProviderStub) CurrentNetwork(context.Context) (llm.Network, error) {
	return llm.NetworkPrivate, nil // 信创私有线
}

// TestClassificationInvariantUnderProviderSwap 组件级落地 §10.2「信创阶段 c01 直连切换对本 change 无感」
// 的可验证部分：c06 仅依赖 c01 窄抽象 llm.Client 接口。底层供应商由公网 provider 切换为信创「Go 直连官方
// SDK」（不同的 llm.Client 具体实现）后，只要接口契约与模型输出一致，c06 经窄抽象发出的出站请求、判别结果
// 与输出的结构化场景上下文契约逐字不变、业务代码零改动（对齐 ADR-0001 D1/D3）。
//
// 边界：真实国产模型判别效果与国产软硬件上的端到端运行属信创集成期 PoC（§10.1/§10.3，需国产模型 / 龙芯·麒麟），
// 此处仅固化「切换无感」的结构前提——c06 写死在接口上，不耦合任何具体供应商类型。
func TestClassificationInvariantUnderProviderSwap(t *testing.T) {
	const scene = "区政府向市发改委申请活动经费的请示"
	const modelOut = `[{"doctype":"请示","subtype":"资金费用申请","direction":"upward","confidence":0.91}]`
	level := llm.ContentSecurityLevelSensitive

	// 公网 provider（复用既有 fake）与信创 Go 直连 provider（不同具体类型）对同一请求返回相同模型输出。
	publicClient := &fakeLLMClient{resp: llm.ChatResponse{Text: modelOut}}
	goDirectClient := &goDirectProviderStub{resp: llm.ChatResponse{Text: modelOut}}

	run := func(client llm.Client) (ClassificationResult, ScenarioContext) {
		clf := NewClassifier(client, DefaultMatrix())
		dec, err := clf.ClassifyCandidates(context.Background(), scene, level, "actor-1", "req-1", defaultThresholds())
		if err != nil {
			t.Fatalf("classify: %v", err)
		}
		if dec.NeedsConfirmation {
			t.Fatalf("single high-confidence candidate must be a direct result, got NeedsConfirmation")
		}
		// 契约组装（BuildScenarioContext）为纯函数，不触供应商；与判别结果一并校验整条出参不变。
		return dec.Result, BuildScenarioContext(dec.Result, scene, map[RequiredSlot]string{}, nil, level)
	}

	pubResult, pubCtx := run(publicClient)
	goResult, goCtx := run(goDirectClient)

	// 判别功能不变：不同供应商实现产出逐字相同的判别结果。
	if !reflect.DeepEqual(pubResult, goResult) {
		t.Fatalf("判别结果随供应商而变:\n public=%#v\n go-direct=%#v", pubResult, goResult)
	}
	// 契约不变：结构化场景上下文逐字相同（capability / 文种 / 方向 / 置信度 / 要素 / 内容密级）。
	if !reflect.DeepEqual(pubCtx, goCtx) {
		t.Fatalf("场景上下文契约随供应商而变:\n public=%#v\n go-direct=%#v", pubCtx, goCtx)
	}
	// 业务调用不变：c06 经窄抽象发出的出站请求与供应商无关（提示词 / 参数 / 密级 / 主体一致）。
	if !reflect.DeepEqual(publicClient.gotReq, goDirectClient.gotReq) {
		t.Fatalf("出站 ChatRequest 随供应商而变:\n public=%#v\n go-direct=%#v", publicClient.gotReq, goDirectClient.gotReq)
	}
	// 内容密级原样透传，供 c02 在任一供应商分支上做出站路由；信创私有线分支同样不缺省非密。
	if pubCtx.ContentSecurityLevel != level {
		t.Fatalf("内容密级 = %q, want %q（原样透传不缺省）", pubCtx.ContentSecurityLevel, level)
	}
}
