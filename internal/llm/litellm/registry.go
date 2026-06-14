package litellm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

type ModelRegistry struct {
	proxyBaseURL string
	adminAPIKey  string
	httpClient   *http.Client
}

func NewModelRegistry(proxyBaseURL string, adminAPIKey string, httpClient *http.Client) *ModelRegistry {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ModelRegistry{proxyBaseURL: proxyBaseURL, adminAPIKey: adminAPIKey, httpClient: httpClient}
}

func (r *ModelRegistry) SyncModelConfig(ctx context.Context, cfg config.ModelConfig) error {
	body, err := json.Marshal(map[string]any{
		"model_name": proxyModelName(cfg),
		"litellm_params": map[string]any{
			"model":               cfg.Model,
			"api_base":            cfg.BaseURL,
			"api_key":             cfg.APIKey,
			"custom_llm_provider": customLLMProvider(cfg.Provider),
		},
		"model_info": map[string]any{
			"id":       cfg.ID,
			"provider": string(cfg.Provider),
			"network":  string(cfg.Network),
		},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, modelNewURL(r.proxyBaseURL), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.adminAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+r.adminAPIKey)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return mapTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return statusError(resp.StatusCode)
	}
	return nil
}

func customLLMProvider(provider config.Provider) string {
	switch provider {
	case config.ProviderAnthropic:
		return "anthropic"
	case config.ProviderOpenAI, config.ProviderOpenAICompatible:
		return "openai"
	default:
		return string(provider)
	}
}

func modelNewURL(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + "/model/new"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/model/new"
	return parsed.String()
}
