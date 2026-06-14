package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestDecoratorSanitizesSensitivePublicCompletionAndRestoresResponse(t *testing.T) {
	next := &recordingClient{
		network: llm.NetworkPublic,
		response: llm.ChatResponse{
			Text:         "请〖ORGANIZATION_01〗反馈。",
			FinishReason: llm.FinishReasonStop,
		},
	}
	decorator := NewDecorator(next, staticProcessor{
		result: SanitizationResult{
			Text: "请〖ORGANIZATION_01〗反馈。",
			Mappings: []Mapping{
				{Placeholder: "〖ORGANIZATION_01〗", Original: "市财政局", Type: EntityTypeOrganization, Source: SourceDictionary},
			},
		},
	}, NewMemoryRouteConfigStore())

	resp, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局反馈。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if next.lastRequest.Messages[0].Content != "请〖ORGANIZATION_01〗反馈。" {
		t.Fatalf("outbound content = %q, want sanitized", next.lastRequest.Messages[0].Content)
	}
	if resp.Text != "请市财政局反馈。" {
		t.Fatalf("response text = %q, want restored", resp.Text)
	}
}

func TestDecoratorDoesNotForceSanitizePrivateTarget(t *testing.T) {
	next := &recordingClient{network: llm.NetworkPrivate, response: llm.ChatResponse{Text: "ok", FinishReason: llm.FinishReasonStop}}
	processor := &countingProcessor{}
	decorator := NewDecorator(next, processor, NewMemoryRouteConfigStore())

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局反馈。"}},
		Route:                llm.Route{RequirePrivate: true},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if processor.calls != 0 {
		t.Fatalf("processor calls = %d, want private target bypass", processor.calls)
	}
	if next.lastRequest.Messages[0].Content != "请市财政局反馈。" {
		t.Fatalf("private target content changed: %q", next.lastRequest.Messages[0].Content)
	}
}

func TestDecoratorRoutesClassifiedAndUnknownToPrivateFailClosed(t *testing.T) {
	for _, level := range []llm.ContentSecurityLevel{
		llm.ContentSecurityLevelClassified,
		llm.ContentSecurityLevelUnknown,
	} {
		t.Run(string(level), func(t *testing.T) {
			next := &recordingClient{
				network: llm.NetworkPublic,
				err:     llm.ErrNoAvailablePrivateConfig,
			}
			decorator := NewDecorator(next, &countingProcessor{}, NewMemoryRouteConfigStore())

			_, err := decorator.Complete(context.Background(), llm.ChatRequest{
				Messages:             []llm.Message{{Role: llm.RoleUser, Content: "涉密内容"}},
				ContentSecurityLevel: level,
			})
			if !errors.Is(err, llm.ErrNoAvailablePrivateConfig) {
				t.Fatalf("error = %v, want no private config", err)
			}
			if !next.lastRequest.Route.RequirePrivate {
				t.Fatalf("classified or unknown level must require private route: %#v", next.lastRequest.Route)
			}
		})
	}
}

type recordingClient struct {
	network     llm.Network
	response    llm.ChatResponse
	err         error
	lastRequest llm.ChatRequest
}

func (c *recordingClient) Complete(_ context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	c.lastRequest = req
	if c.err != nil {
		return llm.ChatResponse{}, c.err
	}
	return c.response, nil
}

func (c *recordingClient) Stream(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	c.lastRequest = req
	ch := make(chan llm.StreamEvent)
	close(ch)
	return ch, c.err
}

func (c *recordingClient) CurrentNetwork(context.Context) (llm.Network, error) {
	return c.network, nil
}

type staticProcessor struct {
	result SanitizationResult
}

func (p staticProcessor) Sanitize(string) SanitizationResult {
	return p.result
}

type countingProcessor struct {
	calls int
}

func (p *countingProcessor) Sanitize(text string) SanitizationResult {
	p.calls++
	return SanitizationResult{Text: text}
}
