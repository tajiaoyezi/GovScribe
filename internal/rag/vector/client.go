package vector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
)

const defaultHTTPTimeout = 10 * time.Second

var (
	ErrEmbeddingUnavailable       = errors.New("embedding unavailable")
	ErrEmbeddingDimensionMismatch = errors.New("embedding dimension mismatch")
	ErrEmbeddingProfileMismatch   = errors.New("embedding profile mismatch")
	ErrInvalidClientConfig        = errors.New("invalid vector client config")
	ErrRerankUnavailable          = errors.New("rerank unavailable")
)

type ClientConfig struct {
	BaseURL             string
	APIKey              string
	EmbeddingModel      string
	EmbeddingDimensions int
	RerankModel         string
	HTTPClient          *http.Client
	Timeout             time.Duration
}

type EmbeddingProfile struct {
	Model      string
	Dimensions int
}

type Embedding struct {
	Model  string
	Values []float64
}

type Embedder interface {
	Embed(context.Context, string) (Embedding, error)
	Profile() EmbeddingProfile
}

type RerankRequest struct {
	Query     string
	Documents []string
}

type RerankResult struct {
	Results []RerankScore
}

type RerankScore struct {
	Index int
	Score float64
}

type Reranker interface {
	Rerank(context.Context, RerankRequest) (RerankResult, error)
}

type OpenAIEmbeddingClient struct {
	client  openai.Client
	profile EmbeddingProfile
}

func NewOpenAIEmbeddingClient(cfg ClientConfig) (*OpenAIEmbeddingClient, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	model := strings.TrimSpace(cfg.EmbeddingModel)
	if baseURL == "" || model == "" || cfg.EmbeddingDimensions <= 0 {
		return nil, ErrInvalidClientConfig
	}
	opts := []openaioption.RequestOption{
		openaioption.WithBaseURL(baseURL),
		openaioption.WithAPIKey(cfg.APIKey),
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, openaioption.WithHTTPClient(cfg.HTTPClient))
	} else if cfg.Timeout > 0 {
		opts = append(opts, openaioption.WithHTTPClient(&http.Client{Timeout: cfg.Timeout}))
	}
	return &OpenAIEmbeddingClient{
		client: openai.NewClient(opts...),
		profile: EmbeddingProfile{
			Model:      model,
			Dimensions: cfg.EmbeddingDimensions,
		},
	}, nil
}

func (c *OpenAIEmbeddingClient) Embed(ctx context.Context, text string) (Embedding, error) {
	resp, err := c.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: param.NewOpt(text),
		},
		Model:          openai.EmbeddingModel(c.profile.Model),
		EncodingFormat: openai.EmbeddingNewParamsEncodingFormatFloat,
	})
	if err != nil {
		return Embedding{}, fmt.Errorf("%w: %v", ErrEmbeddingUnavailable, err)
	}
	if resp == nil || len(resp.Data) == 0 {
		return Embedding{}, fmt.Errorf("%w: missing embedding data", ErrEmbeddingUnavailable)
	}
	values := append([]float64(nil), resp.Data[0].Embedding...)
	if len(values) != c.profile.Dimensions {
		return Embedding{}, fmt.Errorf("%w: got %d want %d", ErrEmbeddingDimensionMismatch, len(values), c.profile.Dimensions)
	}
	return Embedding{Model: c.profile.Model, Values: values}, nil
}

func (c *OpenAIEmbeddingClient) Profile() EmbeddingProfile {
	return c.profile
}

func ValidateEmbeddingProfileCompatibility(indexing, query EmbeddingProfile) error {
	if strings.TrimSpace(indexing.Model) == "" || strings.TrimSpace(query.Model) == "" ||
		indexing.Dimensions <= 0 || query.Dimensions <= 0 {
		return ErrEmbeddingProfileMismatch
	}
	if indexing.Model != query.Model || indexing.Dimensions != query.Dimensions {
		return ErrEmbeddingProfileMismatch
	}
	return nil
}

type HTTPRerankClient struct {
	endpoint   string
	model      string
	apiKey     string
	httpClient *http.Client
}

func NewHTTPRerankClient(cfg ClientConfig) (*HTTPRerankClient, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	model := strings.TrimSpace(cfg.RerankModel)
	if baseURL == "" || model == "" {
		return nil, ErrInvalidClientConfig
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultHTTPTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	} else if cfg.Timeout > 0 && httpClient.Timeout <= 0 {
		copied := *httpClient
		copied.Timeout = cfg.Timeout
		httpClient = &copied
	}
	return &HTTPRerankClient{
		endpoint:   baseURL + "/v1/rerank",
		model:      model,
		apiKey:     cfg.APIKey,
		httpClient: httpClient,
	}, nil
}

func (c *HTTPRerankClient) Rerank(ctx context.Context, req RerankRequest) (RerankResult, error) {
	body, err := json.Marshal(rerankHTTPRequest{
		Model:     c.model,
		Query:     req.Query,
		Documents: req.Documents,
	})
	if err != nil {
		return RerankResult{}, fmt.Errorf("%w: encode request: %v", ErrRerankUnavailable, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return RerankResult{}, fmt.Errorf("%w: build request: %v", ErrRerankUnavailable, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return RerankResult{}, fmt.Errorf("%w: %v", ErrRerankUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return RerankResult{}, fmt.Errorf("%w: status %d", ErrRerankUnavailable, resp.StatusCode)
	}
	var decoded rerankHTTPResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return RerankResult{}, fmt.Errorf("%w: decode response: %v", ErrRerankUnavailable, err)
	}
	if len(decoded.Results) == 0 {
		return RerankResult{}, fmt.Errorf("%w: missing results", ErrRerankUnavailable)
	}
	out := make([]RerankScore, 0, len(decoded.Results))
	for _, item := range decoded.Results {
		if item.Index == nil || item.Score == nil {
			return RerankResult{}, fmt.Errorf("%w: incomplete result", ErrRerankUnavailable)
		}
		if *item.Index < 0 || *item.Index >= len(req.Documents) {
			return RerankResult{}, fmt.Errorf("%w: result index %d out of range", ErrRerankUnavailable, *item.Index)
		}
		out = append(out, RerankScore{Index: *item.Index, Score: *item.Score})
	}
	return RerankResult{Results: out}, nil
}

type rerankHTTPRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type rerankHTTPResponse struct {
	Results []rerankHTTPResult `json:"results"`
}

type rerankHTTPResult struct {
	Index *int     `json:"index"`
	Score *float64 `json:"relevance_score"`
}
