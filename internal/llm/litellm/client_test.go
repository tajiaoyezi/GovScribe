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
		if body["model"] != "cfg-test" || body["stream"] == true {
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

func TestCompleteDoesNotAllowExtraToOverrideRoutingFields(t *testing.T) {
	temperature := 0.2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model"] != "cfg-test" {
			t.Fatalf("model = %q, want selected config alias cfg-test", body["model"])
		}
		if body["stream"] != false {
			t.Fatalf("stream = %#v, want false", body["stream"])
		}
		if body["temperature"] != temperature {
			t.Fatalf("temperature = %#v, want typed parameter %v", body["temperature"], temperature)
		}
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("messages = %#v, want original request messages", body["messages"])
		}
		first := messages[0].(map[string]any)
		if first["content"] != "原始消息" {
			t.Fatalf("message content = %#v, want original message", first["content"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, http.DefaultClient)
	_, err := client.Complete(t.Context(), modelConfig("https://provider.example"), llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "原始消息"}},
		Params: llm.GenerationParams{
			Temperature: &temperature,
			Extra: map[string]any{
				"model":       "bypass-model",
				"messages":    []map[string]string{{"role": "user", "content": "改写消息"}},
				"stream":      true,
				"temperature": 1.0,
			},
		},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestModelRegistrySyncsProviderConfigToLiteLLMModelNew(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/model/new" {
			t.Fatalf("request = %s %s, want POST /model/new", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer proxy-admin" {
			t.Fatalf("authorization = %q, want proxy admin bearer token", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["model_name"] != "cfg-test" {
			t.Fatalf("model_name = %#v, want config id alias", body["model_name"])
		}
		params, ok := body["litellm_params"].(map[string]any)
		if !ok {
			t.Fatalf("litellm_params = %#v, want object", body["litellm_params"])
		}
		if params["model"] != "claude-test" || params["api_base"] != "https://anthropic.example/v1" || params["api_key"] != "sk-provider" {
			t.Fatalf("litellm_params = %#v, want provider model/base/key", params)
		}
		if params["custom_llm_provider"] != "anthropic" {
			t.Fatalf("custom_llm_provider = %#v, want anthropic", params["custom_llm_provider"])
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"success"}`))
	}))
	defer server.Close()

	registry := NewModelRegistry(server.URL, "proxy-admin", http.DefaultClient)
	cfg := modelConfig("https://anthropic.example/v1")
	cfg.Provider = config.ProviderAnthropic
	cfg.APIKey = "sk-provider"
	cfg.Model = "claude-test"
	if err := registry.SyncModelConfig(t.Context(), cfg); err != nil {
		t.Fatalf("sync model config: %v", err)
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
