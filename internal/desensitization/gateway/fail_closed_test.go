package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

func TestDecoratorTurnsPrivateWhenNERUnavailableAndPrivateExists(t *testing.T) {
	next := &recordingClient{network: llm.NetworkPublic, response: llm.ChatResponse{Text: "ok", FinishReason: llm.FinishReasonStop}}
	routes := NewMemoryRouteConfigStore()
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer(nil), failingNER{}),
		routes,
		staticPrivateResolver{route: llm.Route{ConfigID: "cfg-private", RequirePrivate: true}, ok: true},
	)

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "张三负责。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if next.lastRequest.Route.ConfigID != "cfg-private" || !next.lastRequest.Route.RequirePrivate {
		t.Fatalf("route = %#v, want private route after NER unavailable", next.lastRequest.Route)
	}
	if next.lastRequest.Messages[0].Content != "张三负责。" {
		t.Fatalf("private fallback should not send sanitized public content, got %q", next.lastRequest.Messages[0].Content)
	}
	assertLastDisposition(t, routes, DispositionEventRoutePrivate, DispositionReasonNERUnavailablePrivateAvailable)
}

func TestDecoratorDegradesWithRegexAndDictionaryWhenNERUnavailableAndAllowed(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "请〖ORGANIZATION_01〗支付〖AMOUNT_01〗。", FinishReason: llm.FinishReasonStop},
	}
	routes := NewMemoryRouteConfigStore()
	if err := routes.SavePolicy(context.Background(), RoutePolicy{
		Level:               llm.ContentSecurityLevelSensitive,
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: true,
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer([]dictionary.Entry{
			{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
		}), failingNER{}),
		routes,
		staticPrivateResolver{},
	)

	resp, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局支付1234元。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if next.lastRequest.Route.RequirePrivate {
		t.Fatalf("route = %#v, want degraded public route", next.lastRequest.Route)
	}
	if next.lastRequest.Messages[0].Content != "请〖ORGANIZATION_01〗支付〖AMOUNT_01〗。" {
		t.Fatalf("degraded outbound = %q, want regex+dictionary sanitized", next.lastRequest.Messages[0].Content)
	}
	if resp.Text != "请市财政局支付1234元。" {
		t.Fatalf("response text = %q, want restored", resp.Text)
	}
	assertLastDisposition(t, routes, DispositionEventDegradedPublic, DispositionReasonNERUnavailableDegradedPublic)
}

func TestDecoratorTreatsUnprobedPrivateConfigAsUnavailableForNERFallback(t *testing.T) {
	store := config.NewMemoryStore()
	if err := store.Save(context.Background(), config.ModelConfig{
		ID:          "cfg-public",
		Network:     llm.NetworkPublic,
		Enabled:     true,
		ProbePassed: true,
		IsCurrent:   true,
	}); err != nil {
		t.Fatalf("save public config: %v", err)
	}
	if err := store.Save(context.Background(), config.ModelConfig{
		ID:          "cfg-private-unprobed",
		Network:     llm.NetworkPrivate,
		Enabled:     true,
		ProbePassed: false,
	}); err != nil {
		t.Fatalf("save private config: %v", err)
	}
	routes := NewMemoryRouteConfigStore()
	if err := routes.SavePolicy(context.Background(), RoutePolicy{
		Level:               llm.ContentSecurityLevelSensitive,
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: true,
	}); err != nil {
		t.Fatalf("save policy: %v", err)
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
		NewConfigStoreRouteResolver(store),
	)

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局处理。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if next.lastRequest.Route.RequirePrivate {
		t.Fatalf("route = %#v, want degraded public when private config is not probe-passed", next.lastRequest.Route)
	}
	if next.lastRequest.Messages[0].Content != "请〖ORGANIZATION_01〗处理。" {
		t.Fatalf("degraded outbound = %q, want dictionary sanitized", next.lastRequest.Messages[0].Content)
	}
}

func TestDecoratorBlocksWhenNERUnavailableNoPrivateAndNoDegrade(t *testing.T) {
	next := &recordingClient{network: llm.NetworkPublic}
	routes := NewMemoryRouteConfigStore()
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer(nil), failingNER{}),
		routes,
		staticPrivateResolver{},
	)

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "张三负责。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if !errors.Is(err, ErrDesensitizationIncomplete) {
		t.Fatalf("error = %v, want ErrDesensitizationIncomplete", err)
	}
	if next.lastRequest.Messages != nil {
		t.Fatalf("blocked request reached upstream: %#v", next.lastRequest)
	}
	assertLastDisposition(t, routes, DispositionEventBlocked, DispositionReasonNERUnavailableNoPrivateNoDegrade)
}

func TestDecoratorNeverDegradesClassifiedWhenNERUnavailable(t *testing.T) {
	routes := NewMemoryRouteConfigStore()
	if err := routes.SavePolicy(context.Background(), RoutePolicy{
		Level:               llm.ContentSecurityLevelClassified,
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: true,
	}); err != nil {
		t.Fatalf("save policy: %v", err)
	}
	next := &recordingClient{network: llm.NetworkPublic, err: llm.ErrNoAvailablePrivateConfig}
	decorator := NewDecoratorWithRouteResolver(next, NewProcessorWithNER(nil, failingNER{}), routes, staticPrivateResolver{})

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "涉密内容"}},
		ContentSecurityLevel: llm.ContentSecurityLevelClassified,
	})
	if !errors.Is(err, llm.ErrNoAvailablePrivateConfig) {
		t.Fatalf("error = %v, want no private config", err)
	}
	if !next.lastRequest.Route.RequirePrivate {
		t.Fatalf("classified request must force private route, got %#v", next.lastRequest.Route)
	}
	assertLastDisposition(t, routes, DispositionEventBlocked, DispositionReasonNoAvailablePrivateConfig)
}

func TestDecoratorDoesNotRetryPublicWhenPrivateRuntimeFails(t *testing.T) {
	next := &recordingClient{network: llm.NetworkPublic, err: &llm.ProviderError{Reason: llm.ErrorReasonEndpointUnavailable}}
	routes := NewMemoryRouteConfigStore()
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer(nil), failingNER{}),
		routes,
		staticPrivateResolver{route: llm.Route{ConfigID: "cfg-private", RequirePrivate: true}, ok: true},
	)

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "张三负责。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err == nil {
		t.Fatal("expected private runtime failure")
	}
	if next.lastRequest.Route.ConfigID != "cfg-private" || !next.lastRequest.Route.RequirePrivate {
		t.Fatalf("request should only try private route, got %#v", next.lastRequest.Route)
	}
	audits := routes.Audits()
	if len(audits) != 2 {
		t.Fatalf("audit count = %d, want route_private and blocked events: %#v", len(audits), audits)
	}
	if audits[0].DispositionEvent != DispositionEventRoutePrivate ||
		audits[0].DispositionReason != DispositionReasonNERUnavailablePrivateAvailable {
		t.Fatalf("first audit = %#v, want route_private due NER unavailable", audits[0])
	}
	if audits[1].DispositionEvent != DispositionEventBlocked ||
		audits[1].DispositionReason != DispositionReasonPrivateRuntimeFailure {
		t.Fatalf("second audit = %#v, want blocked private runtime failure", audits[1])
	}
}

func TestDecoratorAuditsStreamPrivateRuntimeFailureEvent(t *testing.T) {
	next := &recordingClient{
		network: llm.NetworkPublic,
		streamEvents: []llm.StreamEvent{
			{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonEndpointUnavailable},
		},
	}
	routes := NewMemoryRouteConfigStore()
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer(nil), failingNER{}),
		routes,
		staticPrivateResolver{route: llm.Route{ConfigID: "cfg-private", RequirePrivate: true}, ok: true},
	)

	stream, err := decorator.Stream(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "张三负责。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	events := collectStreamEvents(stream)
	if len(events) != 1 || events[0].Type != llm.StreamEventTypeError {
		t.Fatalf("events = %#v, want one error event", events)
	}
	audits := routes.Audits()
	if len(audits) != 2 {
		t.Fatalf("audit count = %d, want route_private and blocked events: %#v", len(audits), audits)
	}
	if audits[0].DispositionEvent != DispositionEventRoutePrivate ||
		audits[0].DispositionReason != DispositionReasonNERUnavailablePrivateAvailable {
		t.Fatalf("first audit = %#v, want route_private due NER unavailable", audits[0])
	}
	if audits[1].DispositionEvent != DispositionEventBlocked ||
		audits[1].DispositionReason != DispositionReasonPrivateRuntimeFailure {
		t.Fatalf("second audit = %#v, want blocked private runtime failure", audits[1])
	}
}

type failingNER struct{}

func (failingNER) Recognize(context.Context, string) ([]Hit, error) {
	return nil, ErrNERUnavailable
}

type staticPrivateResolver struct {
	route llm.Route
	ok    bool
}

func (r staticPrivateResolver) NetworkForRoute(context.Context, llm.Route) (llm.Network, error) {
	return llm.NetworkPublic, nil
}

func (r staticPrivateResolver) PrivateRoute(context.Context) (llm.Route, bool, error) {
	return r.route, r.ok, nil
}

func assertLastDisposition(t *testing.T, routes *MemoryRouteConfigStore, event DispositionEvent, reason DispositionReason) {
	t.Helper()
	audits := routes.Audits()
	if len(audits) == 0 {
		t.Fatalf("no disposition audit recorded, want %s/%s", event, reason)
	}
	last := audits[len(audits)-1]
	if last.DispositionEvent != event || last.DispositionReason != reason {
		t.Fatalf("last audit = %#v, want %s/%s", last, event, reason)
	}
}

func collectStreamEvents(stream <-chan llm.StreamEvent) []llm.StreamEvent {
	var events []llm.StreamEvent
	for event := range stream {
		events = append(events, event)
	}
	return events
}
