package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

func TestSelectBackendRejectsMissingOrInvalidKind(t *testing.T) {
	factory := NewBackendFactory(config.NewMemoryStore(), map[BackendKind]ProviderBackend{
		BackendLiteLLM: recordingBackend{},
	})

	if _, err := factory.Select(Config{}); !errors.Is(err, ErrBackendSelectionRequired) {
		t.Fatalf("missing backend error = %v, want ErrBackendSelectionRequired", err)
	}
	if _, err := factory.Select(Config{Backend: BackendKind("unknown")}); !errors.Is(err, ErrUnsupportedBackend) {
		t.Fatalf("invalid backend error = %v, want ErrUnsupportedBackend", err)
	}
}

func TestSelectBackendWrapsProviderBackendWithCurrentConfigRouter(t *testing.T) {
	store := config.NewMemoryStore()
	mustSave(t, store, config.ModelConfig{
		ID: "cfg-current", Provider: config.ProviderOpenAI, BaseURL: "https://api.example/v1",
		APIKey: "sk-test", Model: "model", Network: llm.NetworkPrivate, Enabled: true, IsCurrent: true,
	})
	factory := NewBackendFactory(store, map[BackendKind]ProviderBackend{
		BackendLiteLLM: recordingBackend{},
	})

	client, err := factory.Select(Config{Backend: BackendLiteLLM})
	if err != nil {
		t.Fatalf("select backend: %v", err)
	}
	resp, err := client.Complete(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("complete through selected backend: %v", err)
	}
	if resp.Text != "cfg-current" {
		t.Fatalf("response text = %q, want current config id", resp.Text)
	}
	network, err := client.CurrentNetwork(context.Background())
	if err != nil {
		t.Fatalf("current network: %v", err)
	}
	if network != llm.NetworkPrivate {
		t.Fatalf("network = %q, want private", network)
	}
}

func TestSelectBackendRoutesPublicRequestsThroughDesensitizationGateway(t *testing.T) {
	store := config.NewMemoryStore()
	mustSave(t, store, config.ModelConfig{
		ID: "cfg-public", Provider: config.ProviderOpenAI, BaseURL: "https://api.example/v1",
		APIKey: "sk-test", Model: "model", Network: llm.NetworkPublic, Enabled: true, IsCurrent: true,
	})
	backend := &gatewayRecordingBackend{
		response: llm.ChatResponse{Text: "已支付〖AMOUNT_01〗。", FinishReason: llm.FinishReasonStop},
	}
	factory := NewBackendFactory(store, map[BackendKind]ProviderBackend{
		BackendLiteLLM: backend,
	})

	client, err := factory.Select(Config{Backend: BackendLiteLLM})
	if err != nil {
		t.Fatalf("select backend: %v", err)
	}
	resp, err := client.Complete(context.Background(), llm.ChatRequest{
		Messages:             []llm.Message{{Role: llm.RoleUser, Content: "请支付12345.67元。"}},
		ContentSecurityLevel: llm.ContentSecurityLevelSensitive,
	})
	if err != nil {
		t.Fatalf("complete through selected backend: %v", err)
	}
	if backend.lastRequest.Messages[0].Content != "请支付〖AMOUNT_01〗。" {
		t.Fatalf("outbound content = %q, want sanitized amount", backend.lastRequest.Messages[0].Content)
	}
	if resp.Text != "已支付12345.67元。" {
		t.Fatalf("response text = %q, want restored amount", resp.Text)
	}
}

func TestRouterSnapshotsConfigForInFlightStream(t *testing.T) {
	store := config.NewMemoryStore()
	mustSave(t, store, config.ModelConfig{
		ID: "cfg-a", Provider: config.ProviderOpenAI, BaseURL: "https://a.example",
		APIKey: "sk-a", Model: "model-a", Network: llm.NetworkPublic, Enabled: true, IsCurrent: true,
	})
	mustSave(t, store, config.ModelConfig{
		ID: "cfg-b", Provider: config.ProviderOpenAI, BaseURL: "https://b.example",
		APIKey: "sk-b", Model: "model-b", Network: llm.NetworkPrivate, Enabled: true,
	})
	router := NewRouter(store, recordingBackend{})

	firstStream, err := router.Stream(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("start first stream: %v", err)
	}
	if err := store.SetCurrent(context.Background(), "cfg-b"); err != nil {
		t.Fatalf("switch current config: %v", err)
	}
	first := <-firstStream
	if first.Delta != "cfg-a" {
		t.Fatalf("in-flight stream used config %q, want cfg-a snapshot", first.Delta)
	}

	secondStream, err := router.Stream(context.Background(), llm.ChatRequest{})
	if err != nil {
		t.Fatalf("start second stream: %v", err)
	}
	second := <-secondStream
	if second.Delta != "cfg-b" {
		t.Fatalf("new stream used config %q, want cfg-b", second.Delta)
	}
}

func TestRouterCurrentNetworkReadsCurrentConfig(t *testing.T) {
	store := config.NewMemoryStore()
	mustSave(t, store, config.ModelConfig{
		ID: "cfg-private", Provider: config.ProviderOpenAI, BaseURL: "https://private.example",
		APIKey: "sk-private", Model: "model", Network: llm.NetworkPrivate, Enabled: true, IsCurrent: true,
	})
	router := NewRouter(store, recordingBackend{})

	network, err := router.CurrentNetwork(context.Background())
	if err != nil {
		t.Fatalf("current network: %v", err)
	}
	if network != llm.NetworkPrivate {
		t.Fatalf("network = %q, want private", network)
	}
}

func mustSave(t *testing.T, store *config.MemoryStore, cfg config.ModelConfig) {
	t.Helper()
	if err := store.Save(context.Background(), cfg); err != nil {
		t.Fatalf("save config %q: %v", cfg.ID, err)
	}
}

type recordingBackend struct{}

func (recordingBackend) Complete(_ context.Context, cfg config.ModelConfig, _ llm.ChatRequest) (llm.ChatResponse, error) {
	return llm.ChatResponse{Text: cfg.ID, FinishReason: llm.FinishReasonStop}, nil
}

func (recordingBackend) Stream(_ context.Context, cfg config.ModelConfig, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 2)
	ch <- llm.StreamEvent{Type: llm.StreamEventTypeDelta, Delta: cfg.ID}
	ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop}
	close(ch)
	return ch, nil
}

type gatewayRecordingBackend struct {
	response    llm.ChatResponse
	lastRequest llm.ChatRequest
}

func (b *gatewayRecordingBackend) Complete(_ context.Context, _ config.ModelConfig, req llm.ChatRequest) (llm.ChatResponse, error) {
	b.lastRequest = req
	return b.response, nil
}

func (b *gatewayRecordingBackend) Stream(_ context.Context, _ config.ModelConfig, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	b.lastRequest = req
	ch := make(chan llm.StreamEvent)
	close(ch)
	return ch, nil
}
