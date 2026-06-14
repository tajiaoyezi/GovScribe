package direct

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

func TestOpenAICompatibleCompleteUsesV3SDKAndMapsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-direct" {
			t.Fatalf("authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "gpt-direct" {
			t.Fatalf("model = %q", body["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-test",
			"object":"chat.completion",
			"created":1,
			"model":"gpt-direct",
			"choices":[{"index":0,"finish_reason":"length","message":{"role":"assistant","content":"direct result"}}]
		}`))
	}))
	defer server.Close()

	resp, err := NewClient().Complete(t.Context(), modelConfig(config.ProviderOpenAICompatible, server.URL, "gpt-direct"), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "写通知"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != "direct result" || resp.FinishReason != llm.FinishReasonLength {
		t.Fatalf("response = %#v", resp)
	}
}

func TestAnthropicCompleteUsesBaseURLAndMapsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("X-Api-Key"); got != "sk-direct" {
			t.Fatalf("x-api-key = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "claude-direct" || body["max_tokens"].(float64) != 128 {
			t.Fatalf("request body = %#v", body)
		}
		if system, ok := body["system"].([]any); !ok || len(system) != 1 {
			t.Fatalf("system = %#v, want one top-level system block", body["system"])
		}
		if messages, ok := body["messages"].([]any); !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v, want one non-system message", body["messages"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-test",
			"type":"message",
			"role":"assistant",
			"model":"claude-direct",
			"content":[{"type":"text","text":"anthropic result"},{"type":"thinking","thinking":"ignored"}],
			"stop_reason":"refusal",
			"stop_sequence":"",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer server.Close()

	maxTokens := 128
	resp, err := NewClient().Complete(t.Context(), modelConfig(config.ProviderAnthropic, server.URL, "claude-direct"), llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "系统约束"},
			{Role: llm.RoleUser, Content: "写通知"},
		},
		Params: llm.GenerationParams{MaxTokens: &maxTokens},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != "anthropic result" || resp.FinishReason != llm.FinishReasonContentFilter {
		t.Fatalf("response = %#v", resp)
	}
}

func TestOpenAICompatibleStreamMapsDeltaDoneAndErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chunk\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-direct\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"},\"finish_reason\":\"\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chunk\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-direct\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	events, err := NewClient().Stream(t.Context(), modelConfig(config.ProviderOpenAICompatible, server.URL, "gpt-direct"), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	assertStreamEvents(t, events, []llm.StreamEvent{
		{Type: llm.StreamEventTypeDelta, Delta: "你"},
		{Type: llm.StreamEventTypeDelta, Delta: "好"},
		{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop},
	})
}

func TestOpenAICompatibleStreamUnexpectedEOFEmitsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chunk\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-direct\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"完成\"},\"finish_reason\":\"\"}]}\n\n"))
	}))
	defer server.Close()

	events, err := NewClient().Stream(t.Context(), modelConfig(config.ProviderOpenAICompatible, server.URL, "gpt-direct"), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	assertStreamEvents(t, events, []llm.StreamEvent{
		{Type: llm.StreamEventTypeDelta, Delta: "完成"},
		{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonUpstream},
	})
}

func TestAnthropicStreamMapsTextDeltaAndMessageStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\",\"stop_sequence\":\"\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	events, err := NewClient().Stream(t.Context(), modelConfig(config.ProviderAnthropic, server.URL, "claude-direct"), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	assertStreamEvents(t, events, []llm.StreamEvent{
		{Type: llm.StreamEventTypeDelta, Delta: "你"},
		{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonLength},
	})
}

func TestAnthropicStreamUnexpectedEOFEmitsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: content_block_delta\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"半截\"}}\n\n"))
	}))
	defer server.Close()

	events, err := NewClient().Stream(t.Context(), modelConfig(config.ProviderAnthropic, server.URL, "claude-direct"), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	assertStreamEvents(t, events, []llm.StreamEvent{
		{Type: llm.StreamEventTypeDelta, Delta: "半截"},
		{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonUpstream},
	})
}

func TestAnthropicThinkingRejectsMaxTokensAtOrBelowMinimum(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Fatal("invalid thinking request must be rejected before outbound call")
	}))
	defer server.Close()

	maxTokens := 1024
	thinking := true
	_, err := NewClient().Complete(t.Context(), modelConfig(config.ProviderAnthropic, server.URL, "claude-direct"), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "写通知"}},
		Params:   llm.GenerationParams{MaxTokens: &maxTokens, Thinking: &thinking},
	})
	if err == nil {
		t.Fatal("complete must reject thinking when max_tokens is not greater than Anthropic thinking budget")
	}
	if called {
		t.Fatal("invalid thinking request reached upstream server")
	}
}

func TestAnthropicThinkingUsesValidDefaultsWhenMaxTokensMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		thinking, ok := body["thinking"].(map[string]any)
		if !ok {
			t.Fatalf("thinking = %#v, want object", body["thinking"])
		}
		if thinking["budget_tokens"].(float64) >= body["max_tokens"].(float64) {
			t.Fatalf("thinking budget %v must be less than max_tokens %v", thinking["budget_tokens"], body["max_tokens"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg-test",
			"type":"message",
			"role":"assistant",
			"model":"claude-direct",
			"content":[{"type":"text","text":"ok"}],
			"stop_reason":"end_turn",
			"stop_sequence":"",
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer server.Close()

	thinking := true
	_, err := NewClient().Complete(t.Context(), modelConfig(config.ProviderAnthropic, server.URL, "claude-direct"), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "写通知"}},
		Params:   llm.GenerationParams{Thinking: &thinking},
	})
	if err != nil {
		t.Fatalf("complete with thinking defaults: %v", err)
	}
}

func TestDirectBackendsKeepLiteLLMShapeEquivalent(t *testing.T) {
	finishReasons := []llm.FinishReason{
		mapOpenAIFinishReason("stop"),
		mapOpenAIFinishReason("length"),
		mapOpenAIFinishReason("content_filter"),
		mapAnthropicStopReason("end_turn"),
		mapAnthropicStopReason("max_tokens"),
		mapAnthropicStopReason("refusal"),
	}
	for _, reason := range finishReasons {
		switch reason {
		case llm.FinishReasonStop, llm.FinishReasonLength, llm.FinishReasonContentFilter:
		default:
			t.Fatalf("unexpected finish reason %q", reason)
		}
	}
}

func modelConfig(provider config.Provider, baseURL string, model string) config.ModelConfig {
	return config.ModelConfig{
		ID: "cfg-direct", Provider: provider, BaseURL: baseURL, APIKey: "sk-direct",
		Model: model, Network: llm.NetworkPrivate, Enabled: true,
	}
}

func assertStreamEvents(t *testing.T, events <-chan llm.StreamEvent, want []llm.StreamEvent) {
	t.Helper()
	for _, expected := range want {
		got, ok := <-events
		if !ok {
			t.Fatalf("stream closed before %#v", expected)
		}
		if got.Type != expected.Type || got.Delta != expected.Delta || got.FinishReason != expected.FinishReason || got.ErrorReason != expected.ErrorReason {
			t.Fatalf("event = %#v, want %#v", got, expected)
		}
	}
	if got, ok := <-events; ok {
		t.Fatalf("unexpected extra event %#v", got)
	}
}
