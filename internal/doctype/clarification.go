package doctype

import (
	"context"
	"fmt"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// ClarificationState 是要素澄清的请求态（无后端会话持久化，由前端逐轮携带，对齐 design Open Question 的请求态选择）。
type ClarificationState struct {
	Doctype   string
	Direction WritingDirection
	Required  []RequiredSlot
	Filled    map[RequiredSlot]string
	Round     int
	MaxRounds int
	Skipped   bool
}

// ClarificationStep 是一轮澄清判定结果：放行，或针对某缺失要素的具体追问。
type ClarificationStep struct {
	Done         bool
	AskingSlot   RequiredSlot   // !Done 时所问的缺失要素
	Question     string         // !Done 时的针对性追问
	MissingSlots []RequiredSlot // Done 时仍缺失 / 未确认的要素（供 §6 标记移交，提示下游谨慎不臆造）
}

// MissingSlots 返回必需要素中尚未补齐（缺失或空值）的项，保序。
func MissingSlots(required []RequiredSlot, filled map[RequiredSlot]string) []RequiredSlot {
	var missing []RequiredSlot
	for _, s := range required {
		if v, ok := filled[s]; !ok || strings.TrimSpace(v) == "" {
			missing = append(missing, s)
		}
	}
	return missing
}

// NextClarification 据当前澄清态判定放行或追问（design D-06-6）：
// 要素齐备 / 用户显式跳过 / 达轮次上限三者任一 → 放行（放行时附仍缺失要素标记）；否则针对第一项缺失要素发起具体追问。
func NextClarification(state ClarificationState) ClarificationStep {
	missing := MissingSlots(state.Required, state.Filled)
	if len(missing) == 0 {
		return ClarificationStep{Done: true}
	}
	if state.Skipped || state.Round >= state.MaxRounds {
		return ClarificationStep{Done: true, MissingSlots: missing}
	}
	slot := missing[0]
	return ClarificationStep{AskingSlot: slot, Question: questionFor(slot, state.Doctype)}
}

// FillSlot 回填用户在澄清中补充的要素并消耗一轮（design 5.7）；空值不回填但仍消耗一轮（视为该轮未补齐）。
func FillSlot(state ClarificationState, slot RequiredSlot, value string) ClarificationState {
	filled := make(map[RequiredSlot]string, len(state.Filled)+1)
	for k, v := range state.Filled {
		filled[k] = v
	}
	if strings.TrimSpace(value) != "" {
		filled[slot] = strings.TrimSpace(value)
	}
	state.Filled = filled
	state.Round++
	return state
}

// questionFor 生成针对具体缺失要素的追问（design D-06-6，避免泛泛「补充更多信息」）。
func questionFor(slot RequiredSlot, doctype string) string {
	switch slot {
	case SlotRecipient:
		return fmt.Sprintf("这份%s主送哪个机关 / 单位？", fallbackDoctypeName(doctype))
	case SlotIssuer:
		return "这份公文以哪个单位 / 部门名义发出？"
	case SlotSubject:
		return "这份公文要说明或处理的事由是什么？"
	case SlotKeyMatter:
		return "需要明确的关键事项（如金额、范围、对象、目标）是什么？"
	case SlotTimePlace:
		return "涉及的关键时间或地点是什么？"
	default:
		return fmt.Sprintf("请补充「%s」的具体内容。", slot)
	}
}

func fallbackDoctypeName(doctype string) string {
	if strings.TrimSpace(doctype) == "" {
		return "公文"
	}
	return doctype
}

// ExtractSlots 经 c01 窄抽象从场景描述抽取给定必需要素的已知值（design D-06-5 轻抽取）；
// 内容密级随调用透传供 c02 出站密级路由；空 / 过短场景前置拦截，无必需要素时直接返回空。
func (c *Classifier) ExtractSlots(ctx context.Context, sceneText string, required []RequiredSlot, securityLevel llm.ContentSecurityLevel, actorID, requestID string) (map[RequiredSlot]string, error) {
	scene, err := validateScene(sceneText)
	if err != nil {
		return nil, err
	}
	if len(required) == 0 {
		return map[RequiredSlot]string{}, nil
	}
	text, err := c.complete(ctx, BuildSlotExtractionPrompt(required), scene, securityLevel, actorID, requestID, classifyMaxTokens)
	if err != nil {
		return nil, err
	}
	return ParseSlotExtraction(text, required)
}
