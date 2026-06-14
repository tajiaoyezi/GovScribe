package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
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

func TestDecoratorTreatsExplicitConfigIDAsPublicWhenNoResolverIsAvailable(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPrivate,
		response: llm.ChatResponse{Text: "请〖ORGANIZATION_01〗反馈。", FinishReason: llm.FinishReasonStop},
	}
	decorator := NewDecorator(next, staticProcessor{
		result: SanitizationResult{
			Text: "请〖ORGANIZATION_01〗反馈。",
			Mappings: []Mapping{
				{Placeholder: "〖ORGANIZATION_01〗", Original: "市财政局", Type: EntityTypeOrganization, Source: SourceDictionary},
			},
		},
	}, NewMemoryRouteConfigStore())

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局反馈。"}},
		Route:                llm.Route{ConfigID: "public-config"},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if next.lastRequest.Messages[0].Content != "请〖ORGANIZATION_01〗反馈。" {
		t.Fatalf("explicit config id request was not sanitized: %#v", next.lastRequest)
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

func TestDecoratorClearsPinnedConfigWhenForcingPrivate(t *testing.T) {
	next := &recordingClient{
		network: llm.NetworkPublic,
		err:     llm.ErrNoAvailablePrivateConfig,
	}
	decorator := NewDecorator(next, &countingProcessor{}, NewMemoryRouteConfigStore())

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "涉密内容"}},
		Route:                llm.Route{ConfigID: "public-config"},
		ContentSecurityLevel: llm.ContentSecurityLevelClassified,
	})
	if !errors.Is(err, llm.ErrNoAvailablePrivateConfig) {
		t.Fatalf("error = %v, want no private config", err)
	}
	if !next.lastRequest.Route.RequirePrivate || next.lastRequest.Route.ConfigID != "" {
		t.Fatalf("forced private route = %#v, want RequirePrivate with cleared ConfigID", next.lastRequest.Route)
	}
}

func TestDecoratorFailsClosedWhenProcessorMissingForPublicContent(t *testing.T) {
	next := &recordingClient{
		network: llm.NetworkPublic,
		err:     llm.ErrNoAvailablePrivateConfig,
	}
	decorator := NewDecorator(next, nil, NewMemoryRouteConfigStore())

	_, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局反馈。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if !errors.Is(err, llm.ErrNoAvailablePrivateConfig) {
		t.Fatalf("error = %v, want no private config", err)
	}
	if !next.lastRequest.Route.RequirePrivate {
		t.Fatalf("missing processor must force private route, got %#v", next.lastRequest.Route)
	}
}

func TestDecoratorUsesOnePlaceholderMappingAcrossAllMessages(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "由〖PERSON_01〗和〖PERSON_02〗负责。", FinishReason: llm.FinishReasonStop},
	}
	processor := NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "张三", Type: dictionary.EntryTypePerson},
		{Text: "李四", Type: dictionary.EntryTypePerson},
	}))
	decorator := NewDecorator(next, processor, NewMemoryRouteConfigStore())

	resp, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: "张三负责材料。"},
			{Role: llm.RoleUser, Content: "李四负责审核。"},
		},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if next.lastRequest.Messages[0].Content != "〖PERSON_01〗负责材料。" {
		t.Fatalf("first message = %q", next.lastRequest.Messages[0].Content)
	}
	if next.lastRequest.Messages[1].Content != "〖PERSON_02〗负责审核。" {
		t.Fatalf("second message = %q", next.lastRequest.Messages[1].Content)
	}
	if resp.Text != "由张三和李四负责。" {
		t.Fatalf("restored response = %q", resp.Text)
	}
}

func TestDecoratorProcessesUnclassifiedPublicContent(t *testing.T) {
	next := &recordingClient{
		network:  llm.NetworkPublic,
		response: llm.ChatResponse{Text: "请〖ORGANIZATION_01〗反馈。", FinishReason: llm.FinishReasonStop},
	}
	processor := NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
	}))
	decorator := NewDecorator(next, processor, NewMemoryRouteConfigStore())

	resp, err := decorator.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请市财政局反馈。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelUnclassified,
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if next.lastRequest.Messages[0].Content != "请〖ORGANIZATION_01〗反馈。" {
		t.Fatalf("unclassified public content skipped processing: %q", next.lastRequest.Messages[0].Content)
	}
	if resp.Text != "请市财政局反馈。" {
		t.Fatalf("restored response = %q", resp.Text)
	}
}

func TestDecoratorStreamBuffersPlaceholderAcrossChunks(t *testing.T) {
	next := &recordingClient{
		network: llm.NetworkPublic,
		streamEvents: []llm.StreamEvent{
			{Type: llm.StreamEventTypeDelta, Delta: "由〖PERSON_"},
			{Type: llm.StreamEventTypeDelta, Delta: "01〗负责。"},
			{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop},
		},
	}
	processor := NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "张三", Type: dictionary.EntryTypePerson},
	}))
	decorator := NewDecorator(next, processor, NewMemoryRouteConfigStore())

	stream, err := decorator.Stream(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "张三负责。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	deltas := collectStreamDeltas(t, stream)
	if got := joinStrings(deltas); got != "由张三负责。" {
		t.Fatalf("stream output = %q, want restored placeholder across chunks; deltas=%#v", got, deltas)
	}
	for _, delta := range deltas {
		if delta == "由〖PERSON_" || delta == "01〗负责。" {
			t.Fatalf("stream emitted split placeholder delta: %#v", deltas)
		}
	}
}

func TestDecoratorStreamFlushesBufferedTailAtEnd(t *testing.T) {
	next := &recordingClient{
		network: llm.NetworkPublic,
		streamEvents: []llm.StreamEvent{
			{Type: llm.StreamEventTypeDelta, Delta: "正文末尾〖PERSON_"},
		},
	}
	processor := NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "张三", Type: dictionary.EntryTypePerson},
	}))
	decorator := NewDecorator(next, processor, NewMemoryRouteConfigStore())

	stream, err := decorator.Stream(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "张三负责。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if got := joinStrings(collectStreamDeltas(t, stream)); got != "正文末尾〖PERSON_" {
		t.Fatalf("stream output = %q, want buffered tail flushed at end", got)
	}
}

func TestDecoratorStreamDoesNotBufferOverlongPlaceholderTail(t *testing.T) {
	next := &recordingClient{
		network: llm.NetworkPublic,
		streamEvents: []llm.StreamEvent{
			{Type: llm.StreamEventTypeDelta, Delta: "异常〖PERSON_01_过长内容"},
			{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop},
		},
	}
	processor := NewProcessor(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "张三", Type: dictionary.EntryTypePerson},
	}))
	decorator := NewDecorator(next, processor, NewMemoryRouteConfigStore())

	stream, err := decorator.Stream(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "张三负责。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	deltas := collectStreamDeltas(t, stream)
	if len(deltas) == 0 || deltas[0] != "异常〖PERSON_01_过长内容" {
		t.Fatalf("overlong placeholder tail should be emitted without buffering, deltas=%#v", deltas)
	}
}

type recordingClient struct {
	network      llm.Network
	response     llm.ChatResponse
	streamEvents []llm.StreamEvent
	err          error
	lastRequest  llm.ChatRequest
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
	ch := make(chan llm.StreamEvent, len(c.streamEvents))
	for _, event := range c.streamEvents {
		ch <- event
	}
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

func (p staticProcessor) SanitizeMessages(messages []llm.Message) ([]llm.Message, SanitizationResult) {
	out := append([]llm.Message(nil), messages...)
	for i := range out {
		out[i].Content = p.result.Text
	}
	return out, p.result
}

type countingProcessor struct {
	calls int
}

func (p *countingProcessor) Sanitize(text string) SanitizationResult {
	p.calls++
	return SanitizationResult{Text: text}
}

func (p *countingProcessor) SanitizeMessages(messages []llm.Message) ([]llm.Message, SanitizationResult) {
	out := append([]llm.Message(nil), messages...)
	for range out {
		p.calls++
	}
	return out, SanitizationResult{Text: joinedMessages(out)}
}

func collectStreamDeltas(t *testing.T, stream <-chan llm.StreamEvent) []string {
	t.Helper()
	var deltas []string
	for event := range stream {
		if event.Type == llm.StreamEventTypeDelta {
			deltas = append(deltas, event.Delta)
		}
	}
	return deltas
}

func joinStrings(values []string) string {
	var out string
	for _, value := range values {
		out += value
	}
	return out
}
