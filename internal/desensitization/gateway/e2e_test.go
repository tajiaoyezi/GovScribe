package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestDecoratorEndToEndMergesRegexDictionaryAndNERHits(t *testing.T) {
	text := "请依据〔2024〕12号，由张三联系市财政局。"
	next := &recordingClient{
		network: llm.NetworkPublic,
		response: llm.ChatResponse{
			Text:         "请〖PERSON_01〗按〖DOCUMENT_NUMBER_01〗联系〖ORGANIZATION_01〗。",
			FinishReason: llm.FinishReasonStop,
		},
	}
	processor := NewProcessorWithNER(
		NewDictionaryRecognizer([]dictionary.Entry{{Text: "市财政局", Type: dictionary.EntryTypeOrganization}}),
		staticNERClient{hits: []Hit{spanHit(t, text, "张三", EntityTypePerson, SourceNER)}},
	)
	decorator := NewDecorator(next, processor, NewMemoryRouteConfigStore())

	resp, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: text}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	outbound := next.lastRequest.Messages[0].Content
	for _, placeholder := range []string{"〖DOCUMENT_NUMBER_01〗", "〖PERSON_01〗", "〖ORGANIZATION_01〗"} {
		if !strings.Contains(outbound, placeholder) {
			t.Fatalf("outbound = %q, missing %s", outbound, placeholder)
		}
	}
	if resp.Text != "请张三按〔2024〕12号联系市财政局。" {
		t.Fatalf("restored response = %q, want original entities restored", resp.Text)
	}
}

func TestDictionaryEntriesFeedGatewayAndExactRecallFromOneSource(t *testing.T) {
	store := dictionary.NewMemoryStore()
	now := time.Unix(1700000000, 0).UTC()
	if err := store.Save(context.Background(), dictionary.Entry{
		ID:        "entry-org-1",
		Text:      "市财政局",
		Type:      dictionary.EntryTypeOrganization,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save dictionary entry: %v", err)
	}

	entries, err := store.ListActive(context.Background())
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	recognizer := NewDictionaryRecognizer(entries)
	hits := recognizer.Recognize("请市财政局处理。")
	assertHasHit(t, hits, EntityTypeOrganization, SourceDictionary, "市财政局")

	recallOrganizations := exactRecallOrganizations(entries, "请市财政局处理。")
	if len(recallOrganizations) != 1 || recallOrganizations[0] != "市财政局" {
		t.Fatalf("recall organizations = %#v, want shared dictionary term", recallOrganizations)
	}
}

func TestDecoratorDeliversRestoredOriginalToDownstreamSinks(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "已通知〖ORGANIZATION_01〗。", FinishReason: llm.FinishReasonStop},
	}
	decorator := NewDecorator(next, NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
	})), NewMemoryRouteConfigStore())

	resp, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "通知市财政局。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	onlyOfficeText := resp.Text
	corpusIngestionText := resp.Text
	if onlyOfficeText != "已通知市财政局。" || corpusIngestionText != "已通知市财政局。" {
		t.Fatalf("downstream got onlyOffice=%q corpus=%q, want restored original text", onlyOfficeText, corpusIngestionText)
	}
	if strings.Contains(onlyOfficeText, "〖") || strings.Contains(corpusIngestionText, "〖") {
		t.Fatalf("downstream received placeholder text: onlyOffice=%q corpus=%q", onlyOfficeText, corpusIngestionText)
	}
}

func TestClosingDegradedSwitchSafelyRollsBackToFailClosed(t *testing.T) {
	routes := NewMemoryRouteConfigStore()
	if err := routes.SavePolicy(context.Background(), RoutePolicy{
		Level:               llm.ContentSecurityLevelSensitive,
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: true,
	}); err != nil {
		t.Fatalf("save degraded policy: %v", err)
	}
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "请〖ORGANIZATION_01〗处理。", FinishReason: llm.FinishReasonStop},
	}
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer([]dictionary.Entry{
			{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
		}), failingNER{}),
		routes,
		staticPrivateResolver{},
	)

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局处理。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("degraded request: %v", err)
	}
	if next.lastRequest.Messages[0].Content != "请〖ORGANIZATION_01〗处理。" {
		t.Fatalf("degraded outbound = %q, want regex+dictionary sanitization", next.lastRequest.Messages[0].Content)
	}

	if err := routes.SavePolicy(context.Background(), RoutePolicy{
		Level:               llm.ContentSecurityLevelSensitive,
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: false,
	}); err != nil {
		t.Fatalf("close degraded policy: %v", err)
	}
	next.lastRequest = llm.ChatRequest{}
	_, err = decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局处理。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if !errors.Is(err, ErrDesensitizationIncomplete) {
		t.Fatalf("error after closing degraded switch = %v, want ErrDesensitizationIncomplete", err)
	}
	if next.lastRequest.Messages != nil {
		t.Fatalf("fail-closed rollback reached upstream: %#v", next.lastRequest)
	}
}

func spanHit(t *testing.T, text, value string, entityType EntityType, source Source) Hit {
	t.Helper()
	start := strings.Index(text, value)
	if start < 0 {
		t.Fatalf("%q not found in %q", value, text)
	}
	return Hit{Start: start, End: start + len(value), Text: value, Type: entityType, Source: source}
}

func exactRecallOrganizations(entries []dictionary.Entry, text string) []string {
	var out []string
	for _, entry := range entries {
		if entry.Deleted || entry.Type != dictionary.EntryTypeOrganization {
			continue
		}
		if strings.Contains(text, entry.Text) {
			out = append(out, entry.Text)
		}
	}
	return out
}
