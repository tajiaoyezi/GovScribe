package doctype

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// Authenticator 校验请求主体并返回其 ID。c06 不新增权限模型，仅要求已认证（受现有 RBAC 约束，design proposal）；
// 装配期由 c04 会话能力适配实现，测试期注入 fake。
type Authenticator interface {
	Authenticate(ctx context.Context, bearerToken string) (actorID string, err error)
}

// Handler 暴露 c06 文种判别 / 候选确认 / 要素澄清的请求-响应 HTTP 接口（不流式，§8.4；正文生成 SSE 属 c05/c07）。
type Handler struct {
	classifier *Classifier
	slots      SlotStore
	thresholds ThresholdStore
	auth       Authenticator
}

// NewHandler 构造 c06 HTTP 处理器，依赖判别器、必需要素清单、阈值与认证器。
func NewHandler(classifier *Classifier, slots SlotStore, thresholds ThresholdStore, auth Authenticator) *Handler {
	return &Handler{classifier: classifier, slots: slots, thresholds: thresholds, auth: auth}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/doctype/classify":
		h.handleClassify(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/doctype/clarify":
		h.handleClarify(w, r)
	default:
		http.NotFound(w, r)
	}
}

type candidateView struct {
	Doctype          string  `json:"doctype"`
	Subtype          string  `json:"subtype"`
	Direction        string  `json:"direction"`
	Confidence       float64 `json:"confidence"`
	Tier             string  `json:"tier"`
	IsStarredRare    bool    `json:"isStarredRare"`
	TargetCapability string  `json:"targetCapability"`
}

type classifyRequest struct {
	Scene         string `json:"scene"`
	SecurityLevel string `json:"securityLevel"`
}

type classifyResponse struct {
	NeedsConfirmation bool            `json:"needsConfirmation"`
	Result            *candidateView  `json:"result,omitempty"`
	Candidates        []candidateView `json:"candidates,omitempty"`
}

func (h *Handler) handleClassify(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var req classifyRequest
	if !decodeDoctypeJSON(w, r, &req) {
		return
	}
	dec, err := h.classifier.ClassifyCandidates(r.Context(), req.Scene, parseSecurityLevel(req.SecurityLevel), actorID, requestID(r), h.currentThresholds(r.Context()))
	if err != nil {
		writeDoctypeError(w, err)
		return
	}
	resp := classifyResponse{NeedsConfirmation: dec.NeedsConfirmation}
	if dec.NeedsConfirmation {
		resp.Candidates = make([]candidateView, 0, len(dec.Candidates))
		for _, c := range dec.Candidates {
			resp.Candidates = append(resp.Candidates, toCandidateView(c))
		}
	} else {
		v := toCandidateView(dec.Result)
		resp.Result = &v
	}
	writeDoctypeJSON(w, http.StatusOK, resp)
}

type clarifyRequest struct {
	Doctype       string            `json:"doctype"`
	Subtype       string            `json:"subtype"`
	Scene         string            `json:"scene"`
	SecurityLevel string            `json:"securityLevel"`
	Filled        map[string]string `json:"filled"`
	Round         int               `json:"round"`
	Skipped       bool              `json:"skipped"`
}

type scenarioContextView struct {
	TargetCapability     string            `json:"targetCapability"`
	Doctype              string            `json:"doctype"`
	Subtype              string            `json:"subtype"`
	Direction            string            `json:"direction"`
	Confidence           float64           `json:"confidence"`
	SceneDescription     string            `json:"sceneDescription"`
	FilledSlots          map[string]string `json:"filledSlots"`
	MissingSlots         []string          `json:"missingSlots"`
	ContentSecurityLevel string            `json:"contentSecurityLevel"`
}

type clarifyResponse struct {
	Done       bool                 `json:"done"`
	AskingSlot string               `json:"askingSlot,omitempty"`
	Question   string               `json:"question,omitempty"`
	Filled     map[string]string    `json:"filled"`
	Round      int                  `json:"round"`
	Context    *scenarioContextView `json:"context,omitempty"`
}

func (h *Handler) handleClarify(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	var req clarifyRequest
	if !decodeDoctypeJSON(w, r, &req) {
		return
	}
	level := parseSecurityLevel(req.SecurityLevel)
	// 据用户最终选择重解析能力档与行文方向（手选 / 候选确认统一口径）。
	result, err := h.classifier.ResolveSelection(req.Doctype, req.Subtype, req.Scene)
	if err != nil {
		writeDoctypeError(w, err)
		return
	}
	required, err := h.slots.RequiredSlots(r.Context(), result.Doctype, result.Direction)
	if err != nil {
		writeDoctypeError(w, err)
		return
	}
	filled := toSlotMap(req.Filled)
	// 首轮且未携带已知要素、未跳过 → 经 c01→c02 轻抽取播种已知要素。
	if len(filled) == 0 && req.Round == 0 && !req.Skipped {
		seeded, err := h.classifier.ExtractSlots(r.Context(), req.Scene, required, level, actorID, requestID(r))
		if err != nil {
			writeDoctypeError(w, err)
			return
		}
		filled = seeded
	}
	th := h.currentThresholds(r.Context())
	state := ClarificationState{
		Doctype:   result.Doctype,
		Direction: result.Direction,
		Required:  required,
		Filled:    filled,
		Round:     req.Round,
		MaxRounds: th.MaxClarifyRounds,
		Skipped:   req.Skipped,
	}
	step := NextClarification(state)
	resp := clarifyResponse{Done: step.Done, Filled: fromSlotMap(filled), Round: req.Round}
	if step.Done {
		ctx := BuildScenarioContext(result, req.Scene, filled, step.MissingSlots, level)
		v := toScenarioContextView(ctx)
		resp.Context = &v
	} else {
		resp.AskingSlot = string(step.AskingSlot)
		resp.Question = step.Question
	}
	writeDoctypeJSON(w, http.StatusOK, resp)
}

func (h *Handler) currentThresholds(ctx context.Context) Thresholds {
	if h.thresholds == nil {
		return defaultThresholds()
	}
	th, err := h.thresholds.Get(ctx)
	if err != nil {
		return defaultThresholds()
	}
	return th
}

func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h.auth == nil {
		writeError(w, http.StatusUnauthorized)
		return "", false
	}
	actorID, err := h.auth.Authenticate(r.Context(), bearerToken(r))
	if err != nil {
		writeError(w, http.StatusUnauthorized)
		return "", false
	}
	return actorID, true
}

func toCandidateView(r ClassificationResult) candidateView {
	return candidateView{
		Doctype:          r.Doctype,
		Subtype:          r.Subtype,
		Direction:        string(r.Direction),
		Confidence:       r.Confidence,
		Tier:             string(r.Tier),
		IsStarredRare:    r.IsStarredRare,
		TargetCapability: string(routeCapability(r.Tier, r.IsStarredRare)),
	}
}

func toScenarioContextView(s ScenarioContext) scenarioContextView {
	missing := make([]string, 0, len(s.MissingSlots))
	for _, m := range s.MissingSlots {
		missing = append(missing, string(m))
	}
	return scenarioContextView{
		TargetCapability:     string(s.TargetCapability),
		Doctype:              s.Doctype,
		Subtype:              s.Subtype,
		Direction:            string(s.Direction),
		Confidence:           s.Confidence,
		SceneDescription:     s.SceneDescription,
		FilledSlots:          fromSlotMap(s.FilledSlots),
		MissingSlots:         missing,
		ContentSecurityLevel: string(s.ContentSecurityLevel),
	}
}

func toSlotMap(in map[string]string) map[RequiredSlot]string {
	out := make(map[RequiredSlot]string, len(in))
	for k, v := range in {
		out[RequiredSlot(k)] = v
	}
	return out
}

func fromSlotMap(in map[RequiredSlot]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[string(k)] = v
	}
	return out
}

// parseSecurityLevel 解析请求中的内容密级；未知 / 缺省映射为 Unknown（""），不缺省「非密」（design 6.3），由 c02 fail-closed。
func parseSecurityLevel(s string) llm.ContentSecurityLevel {
	switch s {
	case string(llm.ContentSecurityLevelUnclassified):
		return llm.ContentSecurityLevelUnclassified
	case string(llm.ContentSecurityLevelSensitive):
		return llm.ContentSecurityLevelSensitive
	case string(llm.ContentSecurityLevelClassified):
		return llm.ContentSecurityLevelClassified
	default:
		return llm.ContentSecurityLevelUnknown
	}
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

func requestID(r *http.Request) string {
	return r.Header.Get("X-Request-Id")
}

func decodeDoctypeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest)
		return false
	}
	return true
}

func writeDoctypeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// writeDoctypeError 将领域错误映射为 HTTP 状态：输入类 → 400；模型输出不合约 / c02 fail-closed 阻断 / 上游模型错误 → 502；其余内部错误 → 500。
func writeDoctypeError(w http.ResponseWriter, err error) {
	var provErr *llm.ProviderError
	switch {
	case errors.Is(err, ErrEmptyScene), errors.Is(err, ErrSceneDescriptionTooShort), errors.Is(err, ErrEmptyDoctypeSelection):
		writeError(w, http.StatusBadRequest)
	case errors.Is(err, ErrInvalidClassificationOutput), errors.Is(err, ErrInvalidSlotExtraction):
		writeError(w, http.StatusBadGateway)
	case errors.Is(err, llm.ErrNoAvailablePrivateConfig), errors.As(err, &provErr):
		writeError(w, http.StatusBadGateway) // c02 fail-closed / 上游模型错误
	default:
		writeError(w, http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}
