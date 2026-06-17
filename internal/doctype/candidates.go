package doctype

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// ErrEmptyDoctypeSelection 表示用户选择覆盖时未给出文种。
var ErrEmptyDoctypeSelection = errors.New("empty doctype selection")

// ClassificationDecision 是判别后的分支决策（design D-06-2）：
// 高置信单义直接放行单一结果；低置信或多义返回按置信度降序的 Top-N 候选供用户确认，绝不静默选定单一文种。
type ClassificationDecision struct {
	NeedsConfirmation bool
	Result            ClassificationResult   // NeedsConfirmation=false 时的直选结果
	Candidates        []ClassificationResult // NeedsConfirmation=true 时按置信度降序的候选
}

// ClassifyCandidates 判别场景并据阈值决定直选或返回 Top-N 候选。
// th 为运行时可调阈值（§1.5），由调用方读取 ThresholdStore 后传入，便于不改代码调整判定口径。
func (c *Classifier) ClassifyCandidates(ctx context.Context, sceneText string, securityLevel llm.ContentSecurityLevel, actorID, requestID string, th Thresholds) (ClassificationDecision, error) {
	scene, err := validateScene(sceneText)
	if err != nil {
		return ClassificationDecision{}, err
	}
	topN := th.TopN
	if topN < 1 {
		topN = 1
	}
	text, err := c.complete(ctx, BuildCandidatesPrompt(c.entries, topN), scene, securityLevel, actorID, requestID, candidatesMaxTokens)
	if err != nil {
		return ClassificationDecision{}, err
	}
	outs, err := ParseClassificationCandidates(text)
	if err != nil {
		return ClassificationDecision{}, err
	}

	results := make([]ClassificationResult, 0, len(outs))
	for _, out := range outs {
		results = append(results, c.buildResult(out, scene))
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Confidence > results[j].Confidence })
	results = dedupeResults(results) // 去重 (文种,子类)，保留置信度最高项，避免重复候选制造假多义或污染列表

	if needsConfirmation(results, th) {
		if len(results) > topN {
			results = results[:topN]
		}
		return ClassificationDecision{NeedsConfirmation: true, Candidates: results}, nil
	}
	return ClassificationDecision{NeedsConfirmation: false, Result: results[0]}, nil
}

// dedupeResults 按 (文种,子类) 去重，保留先出现项（调用方已按置信度降序，故保留最高置信度项）。
func dedupeResults(results []ClassificationResult) []ClassificationResult {
	seen := make(map[[2]string]bool, len(results))
	out := make([]ClassificationResult, 0, len(results))
	for _, r := range results {
		key := [2]string{r.Doctype, r.Subtype}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

// needsConfirmation 判定是否需用户确认（design D-06-2）：Top-1 置信度低于阈值，或 Top-1 与 Top-2 置信度差小于多义间距。
// 采用严格小于（<）：置信度恰等于阈值、或差恰等于多义间距时不触发确认，与 spec「低于阈值」「差小于间距」语义一致。
func needsConfirmation(results []ClassificationResult, th Thresholds) bool {
	if len(results) == 0 {
		return true
	}
	if results[0].Confidence < th.ConfidenceThreshold {
		return true
	}
	if len(results) >= 2 && results[0].Confidence-results[1].Confidence < th.AmbiguityGap {
		return true
	}
	return false
}

// ResolveSelection 以用户最终选择的文种 / 子类覆盖模型判别（design D-06-2，人为最终把关），
// 据所选文种重解析能力档与行文方向后作为最终结果继续路由与要素校验；用户确认故置信度记为 1.0。
//
// 防御性前置校验：文种不可为空、场景描述须通过空/过短校验（与判别入口同口径，防止被绕过直接调用）。
// 用户选择不携带模型方向线索，行文方向取文种默认 + 场景线索修正（不发生规则/模型冲突，无置信度折减）。
func (c *Classifier) ResolveSelection(doctype, subtype, sceneText string) (ClassificationResult, error) {
	doctype = strings.TrimSpace(doctype)
	subtype = strings.TrimSpace(subtype)
	if doctype == "" {
		return ClassificationResult{}, ErrEmptyDoctypeSelection
	}
	scene, err := validateScene(sceneText)
	if err != nil {
		return ClassificationResult{}, err
	}
	direction, _ := ResolveDirection(doctype, DirectionUnspecified, scene)
	entry, _ := c.matrix.Resolve(doctype, subtype)
	return ClassificationResult{
		Doctype:       doctype,
		Subtype:       subtype,
		Confidence:    1.0,
		Direction:     direction,
		Tier:          entry.Tier,
		IsStarredRare: entry.IsStarredRare,
	}, nil
}
