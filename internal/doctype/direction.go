package doctype

import "strings"

// WritingDirection 是公文行文方向（影响下游正文的称谓与口吻），契约一等字段。
type WritingDirection string

const (
	DirectionUpward      WritingDirection = "upward"     // 上行（向上级）
	DirectionDownward    WritingDirection = "downward"   // 下行（向下级）
	DirectionHorizontal  WritingDirection = "horizontal" // 平行（向同级）
	DirectionUnspecified WritingDirection = ""           // 不适用 / 未定（如讲话稿、方案）
)

// defaultDoctypeDirections 是各文种的默认行文方向（design D-06-4）。
// 讲话稿 / 方案不属机关间行文，默认不指定，交由 LLM 线索判定。
func defaultDoctypeDirections() map[string]WritingDirection {
	return map[string]WritingDirection{
		"请示":   DirectionUpward,
		"报告":   DirectionUpward,
		"批复":   DirectionDownward,
		"通知":   DirectionDownward,
		"通报":   DirectionDownward,
		"会议纪要": DirectionDownward,
		"函":    DirectionHorizontal,
		"讲话稿":  DirectionUnspecified,
		"方案":   DirectionUnspecified,
	}
}

// DirectionClue 是机关关系线索 → 行文方向的修正规则（design D-06-4）。
type DirectionClue struct {
	Keywords  []string
	Direction WritingDirection
}

// defaultDirectionClues 是默认的机关关系线索修正规则；命中线索优先于文种默认方向。
func defaultDirectionClues() []DirectionClue {
	return []DirectionClue{
		{Keywords: []string{"向上级", "报请", "请求批准", "报送上级", "呈报", "上报"}, Direction: DirectionUpward},
		{Keywords: []string{"对下级", "答复下级", "批复", "责成", "部署下发"}, Direction: DirectionDownward},
		{Keywords: []string{"与同级", "商洽", "平级", "函商", "向同级"}, Direction: DirectionHorizontal},
	}
}

// ResolveDirection 按 design D-06-4 综合文种默认方向、机关关系线索与 LLM 方向线索产出最终行文方向。
// 返回最终方向与「规则是否覆盖了 LLM 线索」标志：冲突时以规则为准并返回 true，供调用方降低置信度。
//
//   - 规则无意见（文种默认未指定且无线索命中）→ 采信 LLM 线索，不算覆盖。
//   - 规则有意见且与 LLM 不冲突（一致或 LLM 未给）→ 取规则，不算覆盖。
//   - 规则有意见且与 LLM 冲突 → 取规则、标记覆盖（降置信度）。
func ResolveDirection(doctype string, modelDirection WritingDirection, sceneText string) (WritingDirection, bool) {
	ruleDir := ruleDirection(doctype, sceneText)
	switch {
	case ruleDir == DirectionUnspecified:
		return modelDirection, false
	case modelDirection == DirectionUnspecified || modelDirection == ruleDir:
		return ruleDir, false
	default:
		return ruleDir, true
	}
}

// ruleDirection 取规则方向：先按机关关系线索修正，无线索命中则取文种默认方向。
func ruleDirection(doctype, sceneText string) WritingDirection {
	for _, clue := range defaultDirectionClues() {
		for _, kw := range clue.Keywords {
			if strings.Contains(sceneText, kw) {
				return clue.Direction
			}
		}
	}
	return defaultDoctypeDirections()[doctype]
}
