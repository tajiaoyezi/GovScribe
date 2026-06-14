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
