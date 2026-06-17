package doctype

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// 判别入口相关错误。
var (
	ErrEmptyScene               = errors.New("scene description is empty")
	ErrSceneDescriptionTooShort = errors.New("scene description too short to classify")
)

// minSceneRunes 是可判别场景描述的结构化最小字符数。语义层面的「无可判别线索」（如过于笼统的请求）
// 由模型低置信路径承接（§3 Top-N 候选 / §6 澄清），系统绝不凭空猜测文种——故此处只做结构化空/过短拦截。
const minSceneRunes = 4

// directionConflictConfidencePenalty 是行文方向规则与 LLM 线索冲突时对置信度的折减系数（MVP 经验值，待实测定档）。
const directionConflictConfidencePenalty = 0.8

// 判别调用的输出 token 上界：单结果判别用较小预算，Top-N 候选数组需更多 token。
const (
	classifyMaxTokens   = 256
	candidatesMaxTokens = 512
)

// ClassificationResult 是文种判别的结构化结果，供 §3 候选、§4 路由、§6 场景上下文契约消费。
//
// Tier 与 IsStarredRare 共同构成 2.4 的能力档标注：深做档但标黄稀缺（IsStarredRare=true）与
// 普通深做档同为 TierDeep，下游路由（§4）须据此区分（标黄稀缺降级 c07），不能仅看 Tier。
type ClassificationResult struct {
	Doctype       string
	Subtype       string
	Confidence    float64
	Direction     WritingDirection
	Tier          CapabilityTier
	IsStarredRare bool
}

// Classifier 依据自然语言场景描述判别文种 / 子类，经 c01 窄抽象发起意图分类调用（不感知底层供应商）。
type Classifier struct {
	client  llm.Client
	matrix  *Matrix
	entries []MatrixEntry
	prompt  string
}

// NewClassifier 用 c01 窄抽象 client 与分级表记录构造判别器；判别提示词与分级表解析视图在构造时一次性生成。
func NewClassifier(client llm.Client, entries []MatrixEntry) *Classifier {
	cp := make([]MatrixEntry, len(entries))
	copy(cp, entries)
	return &Classifier{
		client:  client,
		matrix:  NewMatrix(entries),
		entries: cp,
		prompt:  BuildClassificationPrompt(entries),
	}
}

// Classify 接收自然语言场景描述（唯一必填入口，无需预选文种）并返回判别结果。
//
// securityLevel 为出站请求内容密级，随 c01 窄抽象调用透传，供 c01 公网分支上的 c02 出站密级路由判定；
// 本方法不自建脱敏 / 密级口径，也不直连模型 SDK。空 / 过短描述在调用模型前被拦截。
func (c *Classifier) Classify(ctx context.Context, sceneText string, securityLevel llm.ContentSecurityLevel, actorID, requestID string) (ClassificationResult, error) {
	scene, err := validateScene(sceneText)
	if err != nil {
		return ClassificationResult{}, err
	}
	text, err := c.complete(ctx, c.prompt, scene, securityLevel, actorID, requestID, classifyMaxTokens)
	if err != nil {
		return ClassificationResult{}, err
	}
	out, err := ParseClassificationOutput(text)
	if err != nil {
		return ClassificationResult{}, err
	}
	return c.buildResult(out, scene), nil
}

// validateScene 对场景描述做结构化前置校验（空 / 过短），在调用模型前完成。
func validateScene(sceneText string) (string, error) {
	scene := strings.TrimSpace(sceneText)
	if scene == "" {
		return "", ErrEmptyScene
	}
	if utf8.RuneCountInString(scene) < minSceneRunes {
		return "", ErrSceneDescriptionTooShort
	}
	return scene, nil
}

// complete 是 c06 唯一的模型出站调用收口（保密红线，§7 / design D-06-1 / ADR-0001 D7）：
//   - 仅经 c01 窄抽象 llm.Client 发起，不直连任何模型 SDK；该 client 在装配期为 c02 装饰后的实例，
//     使判别 / 抽取调用必经 c02 脱敏网关 + 出站密级路由处置，不可旁路。
//   - 每次调用均携带 ContentSecurityLevel（由调用方据场景上下文契约传入）供 c02 出站路由；
//     c06 不自建第二套脱敏 / 密级 / 审计逻辑（出公网脱敏与路由降级/阻断审计由 c02 承载）。
//   - 错误（含 c02 在外置 NER 不可用 / 无私有可用 / 涉密时的 fail-closed 阻断）原样上抛，
//     绝不静默吞错或回退发送原文。
func (c *Classifier) complete(ctx context.Context, prompt, scene string, securityLevel llm.ContentSecurityLevel, actorID, requestID string, maxTokens int) (string, error) {
	temperature := 0.0
	mt := maxTokens
	resp, err := c.client.Complete(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: prompt},
			{Role: llm.RoleUser, Content: scene},
		},
		Params:               llm.GenerationParams{Temperature: &temperature, MaxTokens: &mt},
		ContentSecurityLevel: securityLevel,
		ActorID:              actorID,
		RequestID:            requestID,
	})
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

// buildResult 将单条判别输出解析为结构化结果：叠加行文方向规则（D-06-4，冲突折减置信度）并标注能力档（D-06-3）。
func (c *Classifier) buildResult(out ClassificationOutput, scene string) ClassificationResult {
	doctype := strings.TrimSpace(out.Doctype)
	subtype := strings.TrimSpace(out.Subtype)
	direction, ruleOverrode := ResolveDirection(doctype, out.Direction, scene)
	confidence := out.Confidence
	if ruleOverrode {
		confidence *= directionConflictConfidencePenalty
	}
	entry, _ := c.matrix.Resolve(doctype, subtype)
	return ClassificationResult{
		Doctype:       doctype,
		Subtype:       subtype,
		Confidence:    confidence,
		Direction:     direction,
		Tier:          entry.Tier,
		IsStarredRare: entry.IsStarredRare,
	}
}
