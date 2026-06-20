package draft

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
	"github.com/tajiaoyezi/GovScribe/internal/llm/direct"
	llmruntime "github.com/tajiaoyezi/GovScribe/internal/llm/runtime"
)

func TestHighFreqDraftOrchestratorCompleteRunsThroughC01DirectBackend(t *testing.T) {
	server := newC05DirectOpenAIServer(t)
	defer server.Close()

	client := selectC05DirectClient(t, server.URL)
	orchestrator := NewHighFreqDraftOrchestrator(
		singleExampleSearcher(),
		singleContractReader(t, "通知"),
		client,
		allowDraftConfig(2),
	)

	result, err := orchestrator.GenerateDraft(context.Background(), authorizedDraftPrincipal("sec-1"), highFreqDirectBackendInput())
	if err != nil {
		server.assertNoFailure(t)
		t.Fatalf("generate through direct backend: %v", err)
	}
	if result.ModelResponse.Text != "直连后端初稿" {
		t.Fatalf("direct backend response = %q, want 直连后端初稿", result.ModelResponse.Text)
	}
	server.assertSaw(t, "complete")
}

func TestHighFreqDraftOrchestratorStreamRunsThroughC01DirectBackend(t *testing.T) {
	server := newC05DirectOpenAIServer(t)
	defer server.Close()

	client := selectC05DirectClient(t, server.URL)
	orchestrator := NewHighFreqDraftOrchestrator(
		singleExampleSearcher(),
		singleContractReader(t, "通知"),
		client,
		allowDraftConfig(2),
	)

	result, err := orchestrator.StreamDraft(context.Background(), authorizedDraftPrincipal("sec-1"), highFreqDirectBackendInput())
	if err != nil {
		server.assertNoFailure(t)
		t.Fatalf("stream through direct backend: %v", err)
	}
	events := collectHighFreqStreamEvents(result.Events)
	if got := joinedHighFreqDeltas(events); got != "直连流式初稿" {
		t.Fatalf("stream deltas = %q, want 直连流式初稿", got)
	}
	if len(events) < 3 {
		t.Fatalf("stream events = %#v, want delta/delta/done", events)
	}
	if events[0].Metadata == nil || events[len(events)-1].Metadata == nil {
		t.Fatalf("c05 metadata must stay on stream head and tail: %#v", events)
	}
	if tail := events[len(events)-1]; tail.Type != llm.StreamEventTypeDone {
		t.Fatalf("tail event = %#v, want done", tail)
	}
	server.assertSaw(t, "stream")
}

func selectC05DirectClient(t *testing.T, baseURL string) llm.Client {
	t.Helper()
	store := config.NewMemoryStore()
	if err := store.Save(context.Background(), config.ModelConfig{
		ID:        "cfg-c05-direct",
		Provider:  config.ProviderOpenAICompatible,
		BaseURL:   baseURL,
		APIKey:    "sk-c05-direct",
		Model:     "gpt-c05-direct",
		Network:   llm.NetworkPrivate,
		Enabled:   true,
		IsCurrent: true,
	}); err != nil {
		t.Fatalf("save direct model config: %v", err)
	}
	factory := llmruntime.NewBackendFactory(store, map[llmruntime.BackendKind]llmruntime.ProviderBackend{
		llmruntime.BackendDirect: direct.NewClient(),
	})
	client, err := factory.Select(llmruntime.Config{Backend: llmruntime.BackendDirect})
	if err != nil {
		t.Fatalf("select direct backend: %v", err)
	}
	return client
}

func highFreqDirectBackendInput() HighFreqDraftRequestInput {
	return HighFreqDraftRequestInput{
		Scenario: doctype.ScenarioContext{
			TargetCapability:     doctype.CapabilityC05,
			Doctype:              "通知",
			Subtype:              "召开会议",
			Direction:            doctype.DirectionDownward,
			Confidence:           0.91,
			SceneDescription:     "通知各部门召开年度会议",
			ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
		},
		ActorID:   "actor-direct",
		RequestID: "req-direct",
	}
}

type c05DirectOpenAIServer struct {
	*httptest.Server
	mu       sync.Mutex
	seen     map[string]bool
	failures []string
}

func newC05DirectOpenAIServer(t *testing.T) *c05DirectOpenAIServer {
	t.Helper()
	fake := &c05DirectOpenAIServer{seen: map[string]bool{}}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			fake.failf(w, "path = %q, want /chat/completions", r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-c05-direct" {
			fake.failf(w, "authorization = %q, want Bearer sk-c05-direct", got)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			fake.failf(w, "decode direct request: %v", err)
			return
		}
		if body["model"] != "gpt-c05-direct" {
			fake.failf(w, "model = %q, want gpt-c05-direct", body["model"])
			return
		}
		prompt := flattenOpenAIMessages(body["messages"])
		for _, want := range []string{"目标文种：通知", "代表子类：召开会议", "通知范文片段"} {
			if !strings.Contains(prompt, want) {
				fake.failf(w, "direct backend prompt missing %q:\n%s", want, prompt)
				return
			}
		}
		if body["stream"] == true {
			fake.mark("stream")
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"chunk\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-c05-direct\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"直连\"},\"finish_reason\":\"\"}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"id\":\"chunk\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-c05-direct\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"流式初稿\"},\"finish_reason\":\"stop\"}]}\n\n"))
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		fake.mark("complete")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-c05-direct",
			"object":"chat.completion",
			"created":1,
			"model":"gpt-c05-direct",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"直连后端初稿"}}]
		}`))
	}))
	return fake
}

func (s *c05DirectOpenAIServer) mark(kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[kind] = true
}

func (s *c05DirectOpenAIServer) assertSaw(t *testing.T, kind string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failures) > 0 {
		t.Fatalf("direct fake server failures: %s", strings.Join(s.failures, "; "))
	}
	if !s.seen[kind] {
		t.Fatalf("direct fake server did not see %s request", kind)
	}
}

func (s *c05DirectOpenAIServer) assertNoFailure(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.failures) > 0 {
		t.Fatalf("direct fake server failures: %s", strings.Join(s.failures, "; "))
	}
}

func (s *c05DirectOpenAIServer) failf(w http.ResponseWriter, format string, args ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	s.mu.Lock()
	s.failures = append(s.failures, message)
	s.mu.Unlock()
	http.Error(w, message, http.StatusInternalServerError)
}

func flattenOpenAIMessages(raw any) string {
	messages, _ := raw.([]any)
	var b strings.Builder
	for _, message := range messages {
		obj, ok := message.(map[string]any)
		if !ok {
			continue
		}
		switch content := obj["content"].(type) {
		case string:
			b.WriteString(content)
			b.WriteByte('\n')
		case []any:
			for _, part := range content {
				partObj, ok := part.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := partObj["text"].(string); ok {
					b.WriteString(text)
					b.WriteByte('\n')
				}
			}
		}
	}
	return b.String()
}
