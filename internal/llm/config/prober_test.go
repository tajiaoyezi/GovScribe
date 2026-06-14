package config

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestBackendProberUsesMinimalChatRequest(t *testing.T) {
	backend := &recordingProbeBackend{}
	prober := NewBackendProber(backend)

	result := prober.Probe(context.Background(), ModelConfig{
		ID: "cfg-1", Provider: ProviderOpenAI, BaseURL: "https://api.example/v1",
		APIKey: "sk-test", Model: "model", Network: llm.NetworkPublic,
	})

	if !result.Available || result.ErrorReason != "" {
		t.Fatalf("probe result = %#v, want available", result)
	}
	if backend.calls != 1 {
		t.Fatalf("backend calls = %d, want 1", backend.calls)
	}
	if backend.request.Params.MaxTokens == nil || *backend.request.Params.MaxTokens != 1 {
		t.Fatalf("max tokens = %#v, want 1", backend.request.Params.MaxTokens)
	}
	if len(backend.request.Messages) != 1 || backend.request.Messages[0].Content == "" {
		t.Fatalf("probe messages = %#v, want one minimal message", backend.request.Messages)
	}
}

func TestBackendProberMapsProviderErrorReason(t *testing.T) {
	backend := &recordingProbeBackend{
		err: &llm.ProviderError{Reason: llm.ErrorReasonAuthenticationFailed},
	}
	prober := NewBackendProber(backend)

	result := prober.Probe(context.Background(), ModelConfig{ID: "cfg-1"})

	if result.Available || result.ErrorReason != llm.ErrorReasonAuthenticationFailed {
		t.Fatalf("probe result = %#v, want authentication failure", result)
	}
}

func TestBackendProberMapsUnknownErrorToUpstream(t *testing.T) {
	backend := &recordingProbeBackend{err: errors.New("boom")}
	prober := NewBackendProber(backend)

	result := prober.Probe(context.Background(), ModelConfig{ID: "cfg-1"})

	if result.Available || result.ErrorReason != llm.ErrorReasonUpstream {
		t.Fatalf("probe result = %#v, want upstream error", result)
	}
}

type recordingProbeBackend struct {
	calls   int
	request llm.ChatRequest
	err     error
}

func (b *recordingProbeBackend) Complete(_ context.Context, _ ModelConfig, req llm.ChatRequest) (llm.ChatResponse, error) {
	b.calls++
	b.request = req
	if b.err != nil {
		return llm.ChatResponse{}, b.err
	}
	return llm.ChatResponse{Text: "", FinishReason: llm.FinishReasonStop}, nil
}
