// Package draft 提供 c05 / c07 共用的起草生成入口契约。
package draft

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/auth"
	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type PipelineBranch string

const (
	BranchDeepDoctype     PipelineBranch = "c05_deep_doctype"
	BranchGenericFallback PipelineBranch = "c07_generic_fallback"
)

var ErrUnsupportedCapability = errors.New("unsupported draft target capability")

type GenerationInput struct {
	Scenario  doctype.ScenarioContext
	ActorID   string
	RequestID string
}

func BuildGenerationRequest(input GenerationInput) (llm.ChatRequest, PipelineBranch, error) {
	branch, err := branchFor(input.Scenario.TargetCapability)
	if err != nil {
		return llm.ChatRequest{}, "", err
	}

	systemPrompt := "你是 GovScribe 公文起草编排层。消费上游 c06 结构化场景上下文，以上下文字段为唯一依据，直接起草规范正文；不得修改内容密级，不得编造缺失要素。"
	if branch == BranchGenericFallback {
		systemPrompt = "你是 GovScribe 通用兜底起草编排层。消费上游 c06 已落定的结构化场景上下文，生成通用兜底公文初稿；不得修改内容密级，不得编造缺失要素。"
	}

	return llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: renderScenario(input.Scenario, branch)},
		},
		ContentSecurityLevel: input.Scenario.OutboundSecurityLevel(),
		ActorID:              input.ActorID,
		RequestID:            input.RequestID,
	}, branch, nil
}

func StreamDraft(ctx context.Context, client llm.Client, input GenerationInput) (<-chan llm.StreamEvent, PipelineBranch, error) {
	req, branch, err := BuildGenerationRequest(input)
	if err != nil {
		return nil, "", err
	}
	if branch == BranchDeepDoctype {
		return nil, "", auth.ErrUnauthorized
	}
	events, err := client.Stream(ctx, req)
	if err != nil {
		return nil, "", err
	}
	return events, branch, nil
}

func branchFor(capability doctype.TargetCapability) (PipelineBranch, error) {
	switch capability {
	case doctype.CapabilityC05:
		return BranchDeepDoctype, nil
	case doctype.CapabilityC07:
		return BranchGenericFallback, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedCapability, capability)
	}
}

func renderScenario(s doctype.ScenarioContext, branch PipelineBranch) string {
	var b strings.Builder
	if branch == BranchGenericFallback {
		b.WriteString("生成模式：通用兜底\n")
	} else {
		b.WriteString("生成模式：高频文种深做\n")
	}
	b.WriteString("目标 capability：")
	b.WriteString(string(s.TargetCapability))
	b.WriteString("\n目标文种：")
	b.WriteString(s.Doctype)
	b.WriteString("\n代表子类：")
	b.WriteString(s.Subtype)
	b.WriteString("\n行文方向：")
	b.WriteString(directionLabel(s.Direction))
	b.WriteString("\n置信度：")
	b.WriteString(fmt.Sprintf("%.2f", s.Confidence))
	b.WriteString("\n用户原始场景描述：")
	b.WriteString(strings.TrimSpace(s.SceneDescription))
	b.WriteString("\n已补齐要素：")
	b.WriteString(renderSlots(s.FilledSlots))
	b.WriteString("\n缺失 / 未确认要素：")
	b.WriteString(renderMissingSlots(s.MissingSlots))
	b.WriteString("\n内容密级：由请求字段透传给 c01 / c02 路由，本提示不得改写。")
	return b.String()
}

func renderSlots(slots map[doctype.RequiredSlot]string) string {
	if len(slots) == 0 {
		return "无"
	}
	keys := make([]string, 0, len(slots))
	byKey := make(map[string]string, len(slots))
	for slot, value := range slots {
		key := string(slot)
		keys = append(keys, key)
		byKey[key] = value
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+byKey[key])
	}
	return strings.Join(parts, "；")
}

func renderMissingSlots(slots []doctype.RequiredSlot) string {
	if len(slots) == 0 {
		return "无"
	}
	out := make([]string, len(slots))
	for i, slot := range slots {
		out[i] = string(slot)
	}
	sort.Strings(out)
	return strings.Join(out, "；")
}

func directionLabel(direction doctype.WritingDirection) string {
	switch direction {
	case doctype.DirectionUpward:
		return "上行"
	case doctype.DirectionDownward:
		return "下行"
	case doctype.DirectionHorizontal:
		return "平行"
	default:
		return "未指定"
	}
}
