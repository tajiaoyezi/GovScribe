package doctype

import (
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// ScenarioContext 是 c06 判别 + 澄清的最终产物，也是交给 c05 / c07 的**唯一下游契约**（design D-06-7，契约唯一来源由 c06 维护）。
//
// 字段固定为 9 项。下游 c05 / c07 必须以本契约的文种 / 子类 / 行文方向为准继续生成，
// 不得重复发起文种判别；并须把 ContentSecurityLevel 作为出站请求密级随 c01 窄抽象调用传递，供 c02 出站路由判定。
type ScenarioContext struct {
	TargetCapability     TargetCapability         // 1. 目标生成能力（c05 / c07）
	Doctype              string                   // 2. 目标文种
	Subtype              string                   // 3. 代表子类
	Direction            WritingDirection         // 4. 行文方向（上行 / 下行 / 平行）
	Confidence           float64                  // 5. 置信度
	SceneDescription     string                   // 6. 用户原始场景描述
	FilledSlots          map[RequiredSlot]string  // 7. 已补齐要素
	MissingSlots         []RequiredSlot           // 8. 缺失 / 未确认要素标记（提示下游谨慎、不臆造关键信息）
	ContentSecurityLevel llm.ContentSecurityLevel // 9. 内容密级（非密 / 敏感 / 涉密；"" 表未知，下游按涉密 fail-closed）
}

// BuildScenarioContext 组装结构化场景上下文契约：由判别结果经 Route 得目标能力/文种/子类/方向，
// 叠加置信度、原始场景、已补齐/缺失要素与内容密级。
//
// 内容密级由写作发起方在发起时确定、原样承载：缺失或未知（""）时**不缺省「非密」**（design D-06-7、6.3），
// 由下游 c02 出站密级路由按涉密 fail-closed 兜底。已补齐要素与缺失标记均做拷贝，避免与调用方共享底层。
func BuildScenarioContext(result ClassificationResult, sceneDescription string, filled map[RequiredSlot]string, missing []RequiredSlot, level llm.ContentSecurityLevel) ScenarioContext {
	label := Route(result)

	filledCopy := make(map[RequiredSlot]string, len(filled))
	for k, v := range filled {
		filledCopy[k] = v
	}
	// 始终分配为非 nil 切片，使契约经 JSON 跨 change 移交时空缺失序列化为 [] 而非 null，与 FilledSlots（{}）一致。
	missingCopy := make([]RequiredSlot, len(missing))
	copy(missingCopy, missing)

	return ScenarioContext{
		TargetCapability:     label.TargetCapability,
		Doctype:              label.Doctype,
		Subtype:              label.Subtype,
		Direction:            label.Direction,
		Confidence:           result.Confidence,
		SceneDescription:     strings.TrimSpace(sceneDescription),
		FilledSlots:          filledCopy,
		MissingSlots:         missingCopy,
		ContentSecurityLevel: level, // 原样承载，不缺省「非密」
	}
}

// OutboundSecurityLevel 返回随 c01 窄抽象调用透传、供 c02 出站密级路由判定的内容密级（design 6.4）。
// 原样返回（含未知 ""）：c02 对未知按涉密 fail-closed，c06 不缺省「非密」。
func (s ScenarioContext) OutboundSecurityLevel() llm.ContentSecurityLevel {
	return s.ContentSecurityLevel
}

// HasUnconfirmedSlots 报告契约是否仍带缺失 / 未确认要素，供下游决定是否谨慎处理、不臆造关键信息。
func (s ScenarioContext) HasUnconfirmedSlots() bool {
	return len(s.MissingSlots) > 0
}
