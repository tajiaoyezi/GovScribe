package litellm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

func TestCompleteMapsOpenAICompatibleResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "gpt-test" || body["stream"] == true {
			t.Fatalf("request body = %#v", body)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v, want one OpenAI-compatible message", body["messages"])
		}
		first, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("message = %#v, want object", messages[0])
		}
		if first["role"] != "user" || first["content"] != "写通知" {
			t.Fatalf("message = %#v, want lowercase role/content keys", first)
		}
		if _, ok := first["Role"]; ok {
			t.Fatalf("message must not use Go field key Role: %#v", first)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"生成结果"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, http.DefaultClient)
	resp, err := client.Complete(t.Context(), modelConfig(server.URL), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "写通知"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != "生成结果" || resp.FinishReason != llm.FinishReasonStop {
		t.Fatalf("response = %#v", resp)
	}
}

func TestCompleteUsesConfiguredProxyBaseURLNotModelProviderURL(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"proxy result"},"finish_reason":"stop"}]}`))
	}))
	defer proxy.Close()

	cfg := modelConfig("https://anthropic.example")
	cfg.Provider = config.ProviderAnthropic
	client := NewClient(proxy.URL, http.DefaultClient)

	resp, err := client.Complete(t.Context(), cfg, llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "写通知"}},
	})
	if err != nil {
		t.Fatalf("complete through proxy: %v", err)
	}
	if resp.Text != "proxy result" {
		t.Fatalf("response text = %q, want proxy result", resp.Text)
	}
}

func TestCompleteInvalidJSONMapsToProviderUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid`))
	}))
	defer server.Close()

	client := NewClient(server.URL, http.DefaultClient)
	_, err := client.Complete(t.Context(), modelConfig("https://provider.example"), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "写通知"}},
	})
	if errorReasonFromError(err) != llm.ErrorReasonUpstream {
		t.Fatalf("error = %v, want upstream provider error", err)
	}
}

func TestStreamIgnoresUnknownFieldsAndEmitsDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"unknown\":true,\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, http.DefaultClient)
	events, err := client.Stream(t.Context(), modelConfig(server.URL), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "流式"}},
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	first := <-events
	second := <-events
	done := <-events
	if first.Type != llm.StreamEventTypeDelta || first.Delta != "你" {
		t.Fatalf("first = %#v", first)
	}
	if second.Type != llm.StreamEventTypeDelta || second.Delta != "好" {
		t.Fatalf("second = %#v", second)
	}
	if done.Type != llm.StreamEventTypeDone || done.FinishReason != llm.FinishReasonStop {
		t.Fatalf("done = %#v", done)
	}
}

func TestStreamHTTPErrorEmitsErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(server.URL, http.DefaultClient)
	events, err := client.Stream(t.Context(), modelConfig(server.URL), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("stream must return event channel for upstream error, got error: %v", err)
	}
	event := <-events
	if event.Type != llm.StreamEventTypeError || event.ErrorReason != llm.ErrorReasonUpstream {
		t.Fatalf("event = %#v, want upstream error event", event)
	}
}

func TestStreamUnexpectedEOFEmitsErrorAfterDeliveredDelta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"半截\"}}]}\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, http.DefaultClient)
	events, err := client.Stream(t.Context(), modelConfig(server.URL), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	first := <-events
	if first.Type != llm.StreamEventTypeDelta || first.Delta != "半截" {
		t.Fatalf("first = %#v, want delivered delta", first)
	}
	second := <-events
	if second.Type != llm.StreamEventTypeError || second.ErrorReason != llm.ErrorReasonUpstream {
		t.Fatalf("second = %#v, want upstream error for incomplete stream", second)
	}
}

func modelConfig(baseURL string) config.ModelConfig {
	return config.ModelConfig{
		ID: "cfg-test", Provider: config.ProviderOpenAI, BaseURL: baseURL,
		APIKey: "sk-test", Model: "gpt-test", Network: llm.NetworkPublic, Enabled: true,
	}
}
