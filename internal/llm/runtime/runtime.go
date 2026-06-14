package runtime

import (
	"context"
	"errors"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

type BackendKind string

const (
	BackendLiteLLM BackendKind = "litellm"
	BackendDirect  BackendKind = "direct"
)

type Config struct {
	Backend BackendKind
}

var (
	ErrBackendSelectionRequired = errors.New("backend selection is required")
	ErrUnsupportedBackend       = errors.New("unsupported backend selection")
)

type BackendFactory struct {
	store    config.Store
	backends map[BackendKind]ProviderBackend
}

func NewBackendFactory(store config.Store, backends map[BackendKind]ProviderBackend) BackendFactory {
	copied := make(map[BackendKind]ProviderBackend, len(backends))
	for kind, backend := range backends {
		copied[kind] = backend
	}
	return BackendFactory{store: store, backends: copied}
}

func (f BackendFactory) Select(cfg Config) (llm.Client, error) {
	if cfg.Backend == "" {
		return nil, ErrBackendSelectionRequired
	}
	backend, ok := f.backends[cfg.Backend]
	if !ok || backend == nil {
		return nil, ErrUnsupportedBackend
	}
	return NewRouter(f.store, backend), nil
}

type ProviderBackend interface {
	Complete(context.Context, config.ModelConfig, llm.ChatRequest) (llm.ChatResponse, error)
	Stream(context.Context, config.ModelConfig, llm.ChatRequest) (<-chan llm.StreamEvent, error)
}

type Router struct {
	store   config.Store
	backend ProviderBackend
}

func NewRouter(store config.Store, backend ProviderBackend) *Router {
	return &Router{store: store, backend: backend}
}

func (r *Router) Complete(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	cfg, err := r.resolveConfig(ctx, req.Route)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	return r.backend.Complete(ctx, cfg, req)
}

func (r *Router) Stream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	cfg, err := r.resolveConfig(ctx, req.Route)
	if err != nil {
		return nil, err
	}
	return r.backend.Stream(ctx, cfg, req)
}

func (r *Router) CurrentNetwork(ctx context.Context) (llm.Network, error) {
	cfg, err := r.store.Current(ctx)
	if err != nil {
		return "", err
	}
	if cfg.Network == "" {
		return "", &llm.ProviderError{Reason: llm.ErrorReasonUpstream}
	}
	return cfg.Network, nil
}

func (r *Router) resolveConfig(ctx context.Context, route llm.Route) (config.ModelConfig, error) {
	if route.ConfigID != "" {
		cfg, err := r.store.Get(ctx, route.ConfigID)
		if err != nil {
			return config.ModelConfig{}, err
		}
		if !cfg.Enabled {
			return config.ModelConfig{}, config.ErrConfigDisabled
		}
		if route.RequirePrivate && cfg.Network != llm.NetworkPrivate {
			return config.ModelConfig{}, &llm.ProviderError{Reason: llm.ErrorReasonNoAvailablePrivateConfig}
		}
		return cfg, nil
	}
	if route.RequirePrivate {
		configs, err := r.store.List(ctx)
		if err != nil {
			return config.ModelConfig{}, err
		}
		for _, cfg := range configs {
			if cfg.Enabled && cfg.Network == llm.NetworkPrivate {
				return cfg, nil
			}
		}
		return config.ModelConfig{}, &llm.ProviderError{Reason: llm.ErrorReasonNoAvailablePrivateConfig}
	}
	return r.store.Current(ctx)
}
