package doctype

import (
	"context"
	"sort"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

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
	text, err := c.complete(ctx, BuildCandidatesPrompt(c.entries, th.TopN), scene, securityLevel, actorID, requestID)
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

	if needsConfirmation(results, th) {
		if th.TopN > 0 && len(results) > th.TopN {
			results = results[:th.TopN]
		}
		return ClassificationDecision{NeedsConfirmation: true, Candidates: results}, nil
	}
	return ClassificationDecision{NeedsConfirmation: false, Result: results[0]}, nil
}

// needsConfirmation 判定是否需用户确认（design D-06-2）：Top-1 置信度低于阈值，或 Top-1 与 Top-2 置信度差小于多义间距。
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
// 重解析能力档与行文方向后作为最终结果继续路由与要素校验；用户确认故置信度记为 1.0。
func (c *Classifier) ResolveSelection(doctype, subtype, sceneText string) ClassificationResult {
	scene := strings.TrimSpace(sceneText)
	result := c.buildResult(ClassificationOutput{Doctype: doctype, Subtype: subtype, Confidence: 1.0}, scene)
	result.Confidence = 1.0
	return result
}
