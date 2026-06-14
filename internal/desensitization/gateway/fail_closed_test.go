package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestDecoratorTurnsPrivateWhenNERUnavailableAndPrivateExists(t *testing.T) {
	next := &recordingClient{network: llm.NetworkPublic, response: llm.ChatResponse{Text: "ok", FinishReason: llm.FinishReasonStop}}
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer(nil), failingNER{}),
		NewMemoryRouteConfigStore(),
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
}

func TestDecoratorBlocksWhenNERUnavailableNoPrivateAndNoDegrade(t *testing.T) {
	next := &recordingClient{network: llm.NetworkPublic}
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer(nil), failingNER{}),
		NewMemoryRouteConfigStore(),
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
}

func TestDecoratorDoesNotRetryPublicWhenPrivateRuntimeFails(t *testing.T) {
	next := &recordingClient{network: llm.NetworkPublic, err: &llm.ProviderError{Reason: llm.ErrorReasonEndpointUnavailable}}
	decorator := NewDecoratorWithRouteResolver(
		next,
		NewProcessorWithNER(NewDictionaryRecognizer(nil), failingNER{}),
		NewMemoryRouteConfigStore(),
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
