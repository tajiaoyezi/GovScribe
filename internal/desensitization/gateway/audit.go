package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type DispositionEvent string

const (
	DispositionEventPublicSanitized DispositionEvent = "public_sanitized"
	DispositionEventRoutePrivate    DispositionEvent = "route_private"
	DispositionEventDegradedPublic  DispositionEvent = "degraded_public"
	DispositionEventBlocked         DispositionEvent = "blocked"
	DispositionEventRouteConfig     DispositionEvent = "route_config_changed"
)

type DispositionReason string

const (
	DispositionReasonNormalPublicCall                 DispositionReason = "normal_public_call"
	DispositionReasonClassificationPrivate            DispositionReason = "classification_private"
	DispositionReasonProcessorMissingPrivate          DispositionReason = "processor_missing_private"
	DispositionReasonNERUnavailablePrivateAvailable   DispositionReason = "ner_unavailable_private_available"
	DispositionReasonNERUnavailableDegradedPublic     DispositionReason = "ner_unavailable_degraded_public"
	DispositionReasonNERUnavailableNoPrivateNoDegrade DispositionReason = "ner_unavailable_no_private_no_degrade"
	DispositionReasonPrivateRuntimeFailure            DispositionReason = "private_runtime_failure"
	DispositionReasonNoAvailablePrivateConfig         DispositionReason = "no_available_private_config"
	DispositionReasonDesensitizationIncomplete        DispositionReason = "desensitization_incomplete"
	DispositionReasonAdminPolicyUpdate                DispositionReason = "admin_policy_update"
)

type DispositionAuditEntry struct {
	ActorID               string
	RequestID             string
	ContentClassification llm.ContentSecurityLevel
	OriginalDiff          string
	MatchDetails          string
	DispositionEvent      DispositionEvent
	DispositionReason     DispositionReason
	At                    time.Time
}

type MatchDetail struct {
	Text         string     `json:"text"`
	Type         EntityType `json:"type"`
	Source       Source     `json:"source"`
	Start        int        `json:"start"`
	End          int        `json:"end"`
	Placeholder  string     `json:"placeholder"`
	MessageIndex int        `json:"message_index"`
	Role         llm.Role   `json:"role,omitempty"`
}

type MessageDiff struct {
	MessageIndex int      `json:"message_index"`
	Role         llm.Role `json:"role,omitempty"`
	Before       string   `json:"before"`
	After        string   `json:"after"`
	Changed      bool     `json:"changed"`
}

type DispositionAuditQuery struct {
	ActorID                string
	RequestID              string
	ContentClassifications []llm.ContentSecurityLevel
	Limit                  int
}

type DispositionAuditStore interface {
	AppendDispositionAudit(context.Context, DispositionAuditEntry) error
}

type DispositionAuditReadStore interface {
	ListDispositionAudits(context.Context, DispositionAuditQuery) ([]DispositionAuditEntry, error)
}

type AuditAccessController interface {
	AllowedAuditClassifications(context.Context, Principal) ([]llm.ContentSecurityLevel, error)
}

const PermissionAuditRead Permission = "audit.read"

var ErrUnauthorizedDispositionAudit = errors.New("unauthorized disposition audit access")
var ErrDispositionAuditRequired = errors.New("disposition audit store is required")

type DispositionAuditQueryService struct {
	store DispositionAuditReadStore
	auth  Authorizer
	acl   AuditAccessController
}

func NewDispositionAuditQueryService(store DispositionAuditReadStore, auth Authorizer, acl AuditAccessController) *DispositionAuditQueryService {
	return &DispositionAuditQueryService{store: store, auth: auth, acl: acl}
}

func (s *DispositionAuditQueryService) List(ctx context.Context, principal Principal, query DispositionAuditQuery) ([]DispositionAuditEntry, error) {
	if s.store == nil || s.auth == nil || s.acl == nil {
		return nil, ErrUnauthorizedDispositionAudit
	}
	if err := s.auth.Authorize(ctx, principal, PermissionAuditRead); err != nil {
		return nil, ErrUnauthorizedDispositionAudit
	}
	allowed, err := s.acl.AllowedAuditClassifications(ctx, principal)
	if err != nil || len(allowed) == 0 {
		return nil, ErrUnauthorizedDispositionAudit
	}
	query.ContentClassifications = intersectClassifications(query.ContentClassifications, allowed)
	if len(query.ContentClassifications) == 0 {
		return nil, ErrUnauthorizedDispositionAudit
	}
	return s.store.ListDispositionAudits(ctx, query)
}

func normalizeDispositionAuditEntry(entry DispositionAuditEntry) DispositionAuditEntry {
	if entry.ActorID == "" {
		entry.ActorID = "system"
	}
	if entry.RequestID == "" {
		entry.RequestID = "unknown"
	}
	entry.ContentClassification = normalizeLevel(entry.ContentClassification)
	if entry.OriginalDiff == "" {
		entry.OriginalDiff = "{}"
	}
	if entry.MatchDetails == "" {
		entry.MatchDetails = "[]"
	}
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	return entry
}

func auditDiffJSON(before, after string) string {
	return mustAuditJSON(struct {
		Before  string `json:"before"`
		After   string `json:"after"`
		Changed bool   `json:"changed"`
	}{
		Before:  before,
		After:   after,
		Changed: before != after,
	})
}

func auditDiffWithMessagesJSON(before, after string, messages []MessageDiff) string {
	if len(messages) == 0 {
		return auditDiffJSON(before, after)
	}
	return mustAuditJSON(struct {
		Before   string        `json:"before"`
		After    string        `json:"after"`
		Changed  bool          `json:"changed"`
		Messages []MessageDiff `json:"messages"`
	}{
		Before:   before,
		After:    after,
		Changed:  before != after,
		Messages: messages,
	})
}

func matchDetailsJSON(matches []MatchDetail) string {
	if len(matches) == 0 {
		return "[]"
	}
	return mustAuditJSON(matches)
}

func policyDiffJSON(before, after RoutePolicy) string {
	return mustAuditJSON(struct {
		Before RoutePolicy `json:"before"`
		After  RoutePolicy `json:"after"`
	}{Before: before, After: after})
}

func mustAuditJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func intersectClassifications(requested, allowed []llm.ContentSecurityLevel) []llm.ContentSecurityLevel {
	allowedSet := make(map[llm.ContentSecurityLevel]struct{}, len(allowed))
	for _, level := range allowed {
		allowedSet[normalizeLevel(level)] = struct{}{}
	}
	source := requested
	if len(source) == 0 {
		source = allowed
	}
	out := make([]llm.ContentSecurityLevel, 0, len(source))
	seen := make(map[llm.ContentSecurityLevel]struct{}, len(source))
	for _, level := range source {
		level = normalizeLevel(level)
		if _, ok := allowedSet[level]; !ok {
			continue
		}
		if _, ok := seen[level]; ok {
			continue
		}
		seen[level] = struct{}{}
		out = append(out, level)
	}
	return out
}

func classificationSet(levels []llm.ContentSecurityLevel) map[llm.ContentSecurityLevel]struct{} {
	out := make(map[llm.ContentSecurityLevel]struct{}, len(levels))
	for _, level := range levels {
		out[normalizeLevel(level)] = struct{}{}
	}
	return out
}

func auditQueryLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
