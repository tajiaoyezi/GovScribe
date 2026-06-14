package litellm

import (
	"net/http"

	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

func NewModelConfigService(store config.Store, auth config.Authorizer, proxyBaseURL string, adminAPIKey string, httpClient *http.Client) *config.Service {
	client := NewClient(proxyBaseURL, httpClient)
	prober := config.NewBackendProber(client)
	registry := NewModelRegistry(proxyBaseURL, adminAPIKey, httpClient)
	return config.NewServiceWithSyncer(store, auth, prober, registry)
}
