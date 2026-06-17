package doctype

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ClassificationOutput 是判别 LLM 必须返回的结构化 JSON（对齐 design D-06-1）：
// 文种 / 代表子类 / 行文方向线索 / 归一化置信度。
type ClassificationOutput struct {
	Doctype    string           `json:"doctype"`
	Subtype    string           `json:"subtype"`
	Direction  WritingDirection `json:"direction"`
	Confidence float64          `json:"confidence"`
}

// ErrInvalidClassificationOutput 表示 LLM 返回的判别结果无法解析或不合约（非严格 JSON、置信度越界等）。
var ErrInvalidClassificationOutput = errors.New("invalid classification output")

// ErrInvalidSlotExtraction 表示 LLM 返回的要素抽取结果无法解析为 JSON 对象。
var ErrInvalidSlotExtraction = errors.New("invalid slot extraction output")

// BuildClassificationPrompt 依分级表生成判别系统提示词：把文种映射表作为受限标签集嵌入，
// 要求 LLM 仅在受限文种内判别并输出严格 JSON（对齐 design D-06-1、doctype-classification spec）。
func BuildClassificationPrompt(entries []MatrixEntry) string {
	var b strings.Builder
	b.WriteString("你是公文文种判别助手。请依据用户的自然语言写作场景描述，判别其对应的公文文种与代表子类。\n")
	b.WriteString("必须遵守：\n")
	b.WriteString("1. 文种取值只能从下列受限标签集中选择，不得自创文种；无法稳定归入任一文种时，doctype 返回\"通用公文\"。\n")
	b.WriteString("2. 子类应取所判文种下的代表子类；若无合适子类可留空。\n")
	b.WriteString("3. direction 为行文方向线索，取值 upward(上行)/downward(下行)/horizontal(平行)，不确定留空字符串。\n")
	b.WriteString("4. confidence 为归一化置信度，取 0 到 1 之间的小数。\n")
	b.WriteString("5. 只输出一个 JSON 对象，不要输出任何额外解释或 Markdown 代码块。\n")
	b.WriteString("输出 JSON 格式：{\"doctype\":\"\",\"subtype\":\"\",\"direction\":\"\",\"confidence\":0.0}\n\n")
	b.WriteString("受限文种标签集（文种：代表子类）：\n")
	for _, line := range labelSetLines(entries) {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// labelSetLines 把分级表整理为「文种：子类1、子类2…」的受限标签集行（按文种聚合、稳定排序）。
func labelSetLines(entries []MatrixEntry) []string {
	subtypes := make(map[string][]string)
	order := make([]string, 0)
	seen := make(map[string]bool)
	for _, e := range entries {
		if _, ok := subtypes[e.Doctype]; !ok {
			order = append(order, e.Doctype)
			subtypes[e.Doctype] = nil
		}
		if e.Subtype != "" {
			key := e.Doctype + "\x00" + e.Subtype
			if !seen[key] { // 防御性去重，避免重复子类污染受限标签集
				seen[key] = true
				subtypes[e.Doctype] = append(subtypes[e.Doctype], e.Subtype)
			}
		}
	}
	sort.Strings(order)
	lines := make([]string, 0, len(order))
	for _, doctype := range order {
		if subs := subtypes[doctype]; len(subs) > 0 {
			lines = append(lines, fmt.Sprintf("%s：%s", doctype, strings.Join(subs, "、")))
		} else {
			lines = append(lines, doctype)
		}
	}
	return lines
}

// ParseClassificationOutput 解析 LLM 返回的判别 JSON，容忍包裹的 Markdown 代码块，并校验合约。
func ParseClassificationOutput(raw string) (ClassificationOutput, error) {
	trimmed := stripCodeFence(strings.TrimSpace(raw))
	var out ClassificationOutput
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return ClassificationOutput{}, fmt.Errorf("%w: %v", ErrInvalidClassificationOutput, err)
	}
	if err := validateClassificationOutput(out); err != nil {
		return ClassificationOutput{}, err
	}
	return out, nil
}

// validateClassificationOutput 校验单条判别输出合约：非空文种、置信度 ∈ [0,1]、方向取值合法。
func validateClassificationOutput(out ClassificationOutput) error {
	if out.Doctype == "" {
		return fmt.Errorf("%w: empty doctype", ErrInvalidClassificationOutput)
	}
	if out.Confidence < 0 || out.Confidence > 1 {
		return fmt.Errorf("%w: confidence %v out of [0,1]", ErrInvalidClassificationOutput, out.Confidence)
	}
	switch out.Direction {
	case DirectionUpward, DirectionDownward, DirectionHorizontal, DirectionUnspecified:
	default:
		return fmt.Errorf("%w: invalid direction %q", ErrInvalidClassificationOutput, out.Direction)
	}
	return nil
}

// BuildCandidatesPrompt 生成 Top-N 候选判别提示词：要求 LLM 在受限标签集内按置信度降序返回最多 topN 个候选 JSON 数组（design D-06-2）。
func BuildCandidatesPrompt(entries []MatrixEntry, topN int) string {
	if topN < 1 {
		topN = 1
	}
	var b strings.Builder
	b.WriteString("你是公文文种判别助手。请依据用户的自然语言写作场景描述，判别其可能对应的公文文种与代表子类。\n")
	b.WriteString("必须遵守：\n")
	b.WriteString(fmt.Sprintf("1. 返回一个 JSON 数组，按置信度从高到低排列，最多 %d 个候选。请列出你认为有任何可能的全部文种候选并给出各自归一化置信度，不要自行排除有一定可能的文种、也不要自行只保留一个——是否需用户确认由系统据置信度判定，你只需如实给出候选与置信度。\n", topN))
	b.WriteString("2. 文种取值只能从下列受限标签集中选择，不得自创文种；无法稳定归入任一文种时，doctype 返回\"通用公文\"。\n")
	b.WriteString("3. 每个候选含 doctype/subtype/direction/confidence 四字段；direction 取 upward/downward/horizontal，不确定留空字符串；confidence 为 0 到 1 之间小数。\n")
	b.WriteString("4. 只输出 JSON 数组，不要输出任何额外解释或 Markdown 代码块。\n")
	b.WriteString("输出格式：[{\"doctype\":\"\",\"subtype\":\"\",\"direction\":\"\",\"confidence\":0.0}]\n\n")
	b.WriteString("受限文种标签集（文种：代表子类）：\n")
	for _, line := range labelSetLines(entries) {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// ParseClassificationCandidates 解析 LLM 返回的候选 JSON 数组，容忍代码块包裹并逐项校验合约；空数组视为不合约。
func ParseClassificationCandidates(raw string) ([]ClassificationOutput, error) {
	trimmed := stripCodeFence(strings.TrimSpace(raw))
	var outs []ClassificationOutput
	if err := json.Unmarshal([]byte(trimmed), &outs); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidClassificationOutput, err)
	}
	if len(outs) == 0 {
		return nil, fmt.Errorf("%w: empty candidate list", ErrInvalidClassificationOutput)
	}
	for _, out := range outs {
		if err := validateClassificationOutput(out); err != nil {
			return nil, err
		}
	}
	return outs, nil
}

// BuildSlotExtractionPrompt 生成必需要素抽取提示词：从场景描述抽取给定要素的已知值，未提及留空、不臆造，输出严格 JSON 对象（design D-06-5）。
func BuildSlotExtractionPrompt(required []RequiredSlot) string {
	names := make([]string, 0, len(required))
	for _, s := range required {
		names = append(names, string(s))
	}
	var b strings.Builder
	b.WriteString("请从用户的自然语言场景描述中抽取下列公文要素的已知值；场景中未提及的要素留空字符串，不要臆造。\n")
	b.WriteString("只输出一个 JSON 对象，键为要素名、值为抽取到的文本（无则空字符串），不要输出任何额外解释或 Markdown 代码块。\n")
	b.WriteString("需要抽取的要素：" + strings.Join(names, "、") + "\n")
	b.WriteString("输出格式示例：{")
	for i, n := range names {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf("%q:\"\"", n))
	}
	b.WriteString("}\n")
	return b.String()
}

// ParseSlotExtraction 解析要素抽取 JSON 对象，仅保留 required 中登记且非空的要素值（去除两端空白）。
func ParseSlotExtraction(raw string, required []RequiredSlot) (map[RequiredSlot]string, error) {
	trimmed := stripCodeFence(strings.TrimSpace(raw))
	var m map[string]string
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSlotExtraction, err)
	}
	allowed := make(map[RequiredSlot]bool, len(required))
	for _, s := range required {
		allowed[s] = true
	}
	out := make(map[RequiredSlot]string)
	for k, v := range m {
		slot := RequiredSlot(k)
		if allowed[slot] {
			if val := strings.TrimSpace(v); val != "" {
				out[slot] = val
			}
		}
	}
	return out, nil
}

// stripCodeFence 去除 LLM 输出常见的 ```json ... ``` 代码块包裹。
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:] // 去掉 ```json 这一行剩余的语言标注
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}
