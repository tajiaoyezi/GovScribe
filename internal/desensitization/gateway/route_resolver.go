package gateway

import (
	"context"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

type ConfigStoreRouteResolver struct {
	store config.Store
}

func NewConfigStoreRouteResolver(store config.Store) ConfigStoreRouteResolver {
	return ConfigStoreRouteResolver{store: store}
}

func (r ConfigStoreRouteResolver) NetworkForRoute(ctx context.Context, route llm.Route) (llm.Network, error) {
	if route.RequirePrivate {
		return llm.NetworkPrivate, nil
	}
	if route.ConfigID != "" {
		cfg, err := r.store.Get(ctx, route.ConfigID)
		if err != nil {
			return "", err
		}
		return cfg.Network, nil
	}
	cfg, err := r.store.Current(ctx)
	if err != nil {
		return "", err
	}
	return cfg.Network, nil
}

func (r ConfigStoreRouteResolver) PrivateRoute(ctx context.Context) (llm.Route, bool, error) {
	configs, err := r.store.List(ctx)
	if err != nil {
		return llm.Route{}, false, err
	}
	for _, cfg := range configs {
		if cfg.Enabled && cfg.Network == llm.NetworkPrivate {
			return llm.Route{ConfigID: cfg.ID, RequirePrivate: true}, true, nil
		}
	}
	return llm.Route{}, false, nil
}
