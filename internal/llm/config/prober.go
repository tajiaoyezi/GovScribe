package config

import (
	"context"
	"errors"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type ProbeBackend interface {
	Complete(context.Context, ModelConfig, llm.ChatRequest) (llm.ChatResponse, error)
}

type BackendProber struct {
	backend ProbeBackend
}

func NewBackendProber(backend ProbeBackend) *BackendProber {
	return &BackendProber{backend: backend}
}

func (p *BackendProber) Probe(ctx context.Context, cfg ModelConfig) ProbeResult {
	maxTokens := 1
	_, err := p.backend.Complete(ctx, cfg, llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "ping"}},
		Params:   llm.GenerationParams{MaxTokens: &maxTokens},
	})
	if err != nil {
		return ProbeResult{Available: false, ErrorReason: probeErrorReason(err), Message: err.Error()}
	}
	return ProbeResult{Available: true}
}

func probeErrorReason(err error) llm.ErrorReason {
	var providerErr *llm.ProviderError
	if errors.As(err, &providerErr) && providerErr.Reason != "" {
		return providerErr.Reason
	}
	return llm.ErrorReasonUpstream
}
