package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestDecoratorAuditsPublicSanitizationDiffAndMatches(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "请〖ORGANIZATION_01〗支付〖AMOUNT_01〗。", FinishReason: llm.FinishReasonStop},
	}
	routes := NewMemoryRouteConfigStore()
	processor := NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
	}))
	decorator := NewDecorator(next, processor, routes)

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		ActorID:              "actor-1",
		RequestID:            "req-public-1",
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局支付1234元。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	audits := routes.Audits()
	if len(audits) != 1 {
		t.Fatalf("audit count = %d, want one public audit: %#v", len(audits), audits)
	}
	audit := audits[0]
	if audit.DispositionEvent != DispositionEvent("public_sanitized") || audit.DispositionReason != DispositionReason("normal_public_call") {
		t.Fatalf("audit disposition = %s/%s, want public_sanitized/normal_public_call", audit.DispositionEvent, audit.DispositionReason)
	}
	var diff struct {
		Before  string `json:"before"`
		After   string `json:"after"`
		Changed bool   `json:"changed"`
	}
	if err := json.Unmarshal([]byte(audit.OriginalDiff), &diff); err != nil {
		t.Fatalf("decode diff %q: %v", audit.OriginalDiff, err)
	}
	if diff.Before != "请市财政局支付1234元。" || diff.After != "请〖ORGANIZATION_01〗支付〖AMOUNT_01〗。" || !diff.Changed {
		t.Fatalf("diff = %#v, want original and sanitized texts", diff)
	}
	var matches []struct {
		Text        string     `json:"text"`
		Type        EntityType `json:"type"`
		Source      Source     `json:"source"`
		Start       int        `json:"start"`
		End         int        `json:"end"`
		Placeholder string     `json:"placeholder"`
	}
	if err := json.Unmarshal([]byte(audit.MatchDetails), &matches); err != nil {
		t.Fatalf("decode match details %q: %v", audit.MatchDetails, err)
	}
	assertMatchDetail(t, matches, "市财政局", EntityTypeOrganization, SourceDictionary, "〖ORGANIZATION_01〗")
	assertMatchDetail(t, matches, "1234元", EntityTypeAmount, SourceRegex, "〖AMOUNT_01〗")
}

func TestDecoratorFailsClosedWhenPublicAuditStoreMissing(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "ok", FinishReason: llm.FinishReasonStop},
	}
	decorator := NewDecorator(next, NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
	})), noAuditRouteStore{})

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局处理。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if !errors.Is(err, ErrDispositionAuditRequired) {
		t.Fatalf("error = %v, want ErrDispositionAuditRequired", err)
	}
	if next.lastRequest.Messages != nil {
		t.Fatalf("request reached upstream without audit store: %#v", next.lastRequest)
	}
}

func TestDecoratorAuditsMessageIndexForMultiMessageMatches(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "ok", FinishReason: llm.FinishReasonStop},
	}
	routes := NewMemoryRouteConfigStore()
	decorator := NewDecorator(next, NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
		{Text: "张三", Type: dictionary.EntryTypePerson},
	})), routes)

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "请市财政局处理。"},
			{Role: llm.RoleUser, Content: "张三负责。"},
		},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	var matches []struct {
		Text         string   `json:"text"`
		MessageIndex int      `json:"message_index"`
		Role         llm.Role `json:"role"`
	}
	if err := json.Unmarshal([]byte(routes.Audits()[0].MatchDetails), &matches); err != nil {
		t.Fatalf("decode match details: %v", err)
	}
	if !strings.Contains(routes.Audits()[0].MatchDetails, `"message_index":0`) {
		t.Fatalf("match details should explicitly include message_index 0: %s", routes.Audits()[0].MatchDetails)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %#v, want two match details", matches)
	}
	if matches[0].Text != "市财政局" || matches[0].MessageIndex != 0 || matches[0].Role != llm.RoleSystem {
		t.Fatalf("first match = %#v, want message 0 system", matches[0])
	}
	if matches[1].Text != "张三" || matches[1].MessageIndex != 1 || matches[1].Role != llm.RoleUser {
		t.Fatalf("second match = %#v, want message 1 user", matches[1])
	}
	var diff struct {
		Messages []struct {
			MessageIndex int      `json:"message_index"`
			Role         llm.Role `json:"role"`
			Before       string   `json:"before"`
			After        string   `json:"after"`
			Changed      bool     `json:"changed"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(routes.Audits()[0].OriginalDiff), &diff); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if len(diff.Messages) != 2 {
		t.Fatalf("message diffs = %#v, want two message diffs", diff.Messages)
	}
	if diff.Messages[0].MessageIndex != 0 || diff.Messages[0].Role != llm.RoleSystem ||
		diff.Messages[0].Before != "请市财政局处理。" || diff.Messages[0].After != "请〖ORGANIZATION_01〗处理。" ||
		!diff.Messages[0].Changed {
		t.Fatalf("first message diff = %#v, want structured system diff", diff.Messages[0])
	}
}

func TestRoutePolicyServiceAuditsPolicyChanges(t *testing.T) {
	routes := NewMemoryRouteConfigStore()
	svc := NewRoutePolicyService(routes, &recordingRouteAuthorizer{})
	now := time.Unix(1700000000, 0).UTC()
	svc.now = func() time.Time { return now }

	_, err := svc.SavePolicy(context.Background(), Principal{ID: "admin-1"}, RoutePolicy{
		Level:               llm.ContentSecurityLevelSensitive,
		TargetNetwork:       llm.NetworkPublic,
		ModelConfigID:       "cfg-public",
		AllowDegradedPublic: true,
	})
	if err != nil {
		t.Fatalf("save policy: %v", err)
	}

	audits := routes.Audits()
	if len(audits) != 1 {
		t.Fatalf("audit count = %d, want one route policy audit: %#v", len(audits), audits)
	}
	audit := audits[0]
	if audit.ActorID != "admin-1" || audit.RequestID != "route-policy:sensitive" {
		t.Fatalf("audit actor/request = %s/%s, want admin-1/route-policy:sensitive", audit.ActorID, audit.RequestID)
	}
	if audit.DispositionEvent != DispositionEvent("route_config_changed") ||
		audit.DispositionReason != DispositionReason("admin_policy_update") {
		t.Fatalf("audit disposition = %s/%s, want route_config_changed/admin_policy_update", audit.DispositionEvent, audit.DispositionReason)
	}
	var diff struct {
		Before RoutePolicy `json:"before"`
		After  RoutePolicy `json:"after"`
	}
	if err := json.Unmarshal([]byte(audit.OriginalDiff), &diff); err != nil {
		t.Fatalf("decode policy diff %q: %v", audit.OriginalDiff, err)
	}
	if diff.Before.AllowDegradedPublic || !diff.After.AllowDegradedPublic || diff.After.ModelConfigID != "cfg-public" {
		t.Fatalf("policy diff = %#v, want degraded switch change recorded", diff)
	}
}

func TestRoutePolicyServiceRollsBackPostgresPolicyWhenAuditFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresRouteConfigStore(db)
	svc := NewRoutePolicyService(store, &recordingRouteAuthorizer{})
	now := time.Unix(1700000000, 0).UTC()
	svc.now = func() time.Time { return now }
	policy := RoutePolicy{
		Level:               llm.ContentSecurityLevelSensitive,
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: true,
		UpdatedBy:           "admin-1",
		UpdatedAt:           now,
	}

	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock\\(hashtext\\(\\$1\\)\\)").
		WithArgs(string(llm.ContentSecurityLevelSensitive)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT classification, target_network, model_config_id, allow_degraded_public, updated_by, updated_at FROM security_classification_routes WHERE classification = \\$1").
		WithArgs(string(llm.ContentSecurityLevelSensitive)).
		WillReturnRows(sqlmock.NewRows(routePolicyColumns()))
	mock.ExpectExec("INSERT INTO security_classification_routes").
		WithArgs(string(policy.Level), string(policy.TargetNetwork), nil, policy.AllowDegradedPublic, policy.UpdatedBy, policy.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO desensitization_audit_logs").
		WillReturnError(errors.New("audit unavailable"))
	mock.ExpectRollback()

	_, err = svc.SavePolicy(context.Background(), Principal{ID: "admin-1"}, policy)
	if err == nil {
		t.Fatal("expected audit failure to abort policy save")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestRoutePolicyServiceRequiresAtomicAuditStore(t *testing.T) {
	svc := NewRoutePolicyService(noAuditRouteStore{}, &recordingRouteAuthorizer{})
	_, err := svc.SavePolicy(context.Background(), Principal{ID: "admin-1"}, RoutePolicy{
		Level:         llm.ContentSecurityLevelSensitive,
		TargetNetwork: llm.NetworkPublic,
	})
	if !errors.Is(err, ErrDispositionAuditRequired) {
		t.Fatalf("error = %v, want ErrDispositionAuditRequired", err)
	}
}

func TestDispositionAuditQueryServiceRequiresAuditReadAndClassificationACL(t *testing.T) {
	routes := NewMemoryRouteConfigStore()
	if err := routes.AppendDispositionAudit(context.Background(), DispositionAuditEntry{
		ActorID:               "actor-1",
		RequestID:             "req-1",
		ContentClassification: llm.ContentSecurityLevelSensitive,
		DispositionEvent:      DispositionEvent("public_sanitized"),
		DispositionReason:     DispositionReason("normal_public_call"),
	}); err != nil {
		t.Fatalf("seed audit: %v", err)
	}

	svc := NewDispositionAuditQueryService(routes, &recordingRouteAuthorizer{}, staticAuditACL{
		allowed: []llm.ContentSecurityLevel{llm.ContentSecurityLevelSensitive},
	})
	entries, err := svc.List(context.Background(), Principal{ID: "auditor-1"}, DispositionAuditQuery{
		ContentClassifications: []llm.ContentSecurityLevel{llm.ContentSecurityLevelSensitive},
	})
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	if len(entries) != 1 || entries[0].RequestID != "req-1" {
		t.Fatalf("entries = %#v, want seeded sensitive audit", entries)
	}

	deniedSvc := NewDispositionAuditQueryService(routes, denyingRouteAuthorizer{}, staticAuditACL{
		allowed: []llm.ContentSecurityLevel{llm.ContentSecurityLevelSensitive},
	})
	if _, err := deniedSvc.List(context.Background(), Principal{ID: "user-1"}, DispositionAuditQuery{
		ContentClassifications: []llm.ContentSecurityLevel{llm.ContentSecurityLevelSensitive},
	}); !errors.Is(err, ErrUnauthorizedDispositionAudit) {
		t.Fatalf("denied RBAC error = %v, want ErrUnauthorizedDispositionAudit", err)
	}

	aclDeniedSvc := NewDispositionAuditQueryService(routes, &recordingRouteAuthorizer{}, staticAuditACL{})
	if _, err := aclDeniedSvc.List(context.Background(), Principal{ID: "auditor-1"}, DispositionAuditQuery{
		ContentClassifications: []llm.ContentSecurityLevel{llm.ContentSecurityLevelSensitive},
	}); !errors.Is(err, ErrUnauthorizedDispositionAudit) {
		t.Fatalf("denied ACL error = %v, want ErrUnauthorizedDispositionAudit", err)
	}
}

func TestPostgresRouteConfigStoreListsDispositionAudits(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresRouteConfigStore(db)
	at := time.Unix(1700000000, 0).UTC()
	rows := sqlmock.NewRows(dispositionAuditColumns()).AddRow(
		"actor-1",
		"req-1",
		string(llm.ContentSecurityLevelSensitive),
		`{"changed":true}`,
		`[]`,
		"public_sanitized",
		"normal_public_call",
		at,
	)
	mock.ExpectQuery("SELECT actor_id, request_id, content_classification, original_diff, match_details, disposition_event, disposition_reason, created_at FROM desensitization_audit_logs WHERE content_classification IN \\(\\$1\\) ORDER BY created_at DESC, id DESC LIMIT 100").
		WithArgs(string(llm.ContentSecurityLevelSensitive)).
		WillReturnRows(rows)

	entries, err := store.ListDispositionAudits(context.Background(), DispositionAuditQuery{
		ContentClassifications: []llm.ContentSecurityLevel{llm.ContentSecurityLevelSensitive},
	})
	if err != nil {
		t.Fatalf("list audits: %v", err)
	}
	if len(entries) != 1 || entries[0].DispositionEvent != DispositionEvent("public_sanitized") {
		t.Fatalf("entries = %#v, want public audit", entries)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

type staticAuditACL struct {
	allowed []llm.ContentSecurityLevel
}

func (a staticAuditACL) AllowedAuditClassifications(context.Context, Principal) ([]llm.ContentSecurityLevel, error) {
	return a.allowed, nil
}

type noAuditRouteStore struct{}

func (noAuditRouteStore) GetPolicy(context.Context, llm.ContentSecurityLevel) (RoutePolicy, error) {
	return RoutePolicy{Level: llm.ContentSecurityLevelSensitive, TargetNetwork: llm.NetworkPublic}, nil
}

func (noAuditRouteStore) SavePolicy(context.Context, RoutePolicy) error {
	return nil
}

func assertMatchDetail(t *testing.T, matches []struct {
	Text        string     `json:"text"`
	Type        EntityType `json:"type"`
	Source      Source     `json:"source"`
	Start       int        `json:"start"`
	End         int        `json:"end"`
	Placeholder string     `json:"placeholder"`
}, text string, entityType EntityType, source Source, placeholder string) {
	t.Helper()
	for _, match := range matches {
		if match.Text == text && match.Type == entityType && match.Source == source && match.Placeholder == placeholder && match.Start >= 0 && match.End > match.Start {
			return
		}
	}
	t.Fatalf("missing match text=%q type=%q source=%q placeholder=%q in %#v", text, entityType, source, placeholder, matches)
}
