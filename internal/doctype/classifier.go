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

// ClassificationResult 是文种判别的结构化结果，供 §3 候选、§4 路由、§6 场景上下文契约消费。
type ClassificationResult struct {
	Doctype    string
	Subtype    string
	Confidence float64
	Direction  WritingDirection
	Tier       CapabilityTier
}

// Classifier 依据自然语言场景描述判别文种 / 子类，经 c01 窄抽象发起意图分类调用（不感知底层供应商）。
type Classifier struct {
	client llm.Client
	matrix *Matrix
	prompt string
}

// NewClassifier 用 c01 窄抽象 client 与分级表记录构造判别器；判别提示词与分级表解析视图在构造时一次性生成。
func NewClassifier(client llm.Client, entries []MatrixEntry) *Classifier {
	return &Classifier{
		client: client,
		matrix: NewMatrix(entries),
		prompt: BuildClassificationPrompt(entries),
	}
}

// Classify 接收自然语言场景描述（唯一必填入口，无需预选文种）并返回判别结果。
//
// securityLevel 为出站请求内容密级，随 c01 窄抽象调用透传，供 c01 公网分支上的 c02 出站密级路由判定；
// 本方法不自建脱敏 / 密级口径，也不直连模型 SDK。空 / 过短描述在调用模型前被拦截。
func (c *Classifier) Classify(ctx context.Context, sceneText string, securityLevel llm.ContentSecurityLevel, actorID, requestID string) (ClassificationResult, error) {
	scene := strings.TrimSpace(sceneText)
	if scene == "" {
		return ClassificationResult{}, ErrEmptyScene
	}
	if utf8.RuneCountInString(scene) < minSceneRunes {
		return ClassificationResult{}, ErrSceneDescriptionTooShort
	}

	temperature := 0.0
	maxTokens := 256
	resp, err := c.client.Complete(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: c.prompt},
			{Role: llm.RoleUser, Content: scene},
		},
		Params:               llm.GenerationParams{Temperature: &temperature, MaxTokens: &maxTokens},
		ContentSecurityLevel: securityLevel,
		ActorID:              actorID,
		RequestID:            requestID,
	})
	if err != nil {
		return ClassificationResult{}, err
	}

	out, err := ParseClassificationOutput(resp.Text)
	if err != nil {
		return ClassificationResult{}, err
	}

	// 行文方向：文种默认方向 + 机关关系线索修正，与 LLM 线索取并、冲突以规则为准并折减置信度（D-06-4）。
	direction, ruleOverrode := ResolveDirection(out.Doctype, out.Direction, scene)
	confidence := out.Confidence
	if ruleOverrode {
		confidence *= directionConflictConfidencePenalty
	}

	// 能力档标注（含 B 表文种）：查分级表解析视图（D-06-3）；未知文种合成兜底档。
	entry, _ := c.matrix.Resolve(out.Doctype, out.Subtype)

	return ClassificationResult{
		Doctype:    out.Doctype,
		Subtype:    out.Subtype,
		Confidence: confidence,
		Direction:  direction,
		Tier:       entry.Tier,
	}, nil
}
