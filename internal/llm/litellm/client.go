package litellm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

type Client struct {
	proxyBaseURL string
	httpClient   *http.Client
}

func NewClient(proxyBaseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{proxyBaseURL: proxyBaseURL, httpClient: httpClient}
}

func (c *Client) Complete(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) (llm.ChatResponse, error) {
	body, err := json.Marshal(chatRequestBody(cfg, req, false))
	if err != nil {
		return llm.ChatResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL(c.proxyBaseURL), bytes.NewReader(body))
	if err != nil {
		return llm.ChatResponse{}, err
	}
	setHeaders(httpReq, cfg)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return llm.ChatResponse{}, mapTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.ChatResponse{}, statusError(resp.StatusCode)
	}

	var decoded chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return llm.ChatResponse{}, &llm.ProviderError{Reason: llm.ErrorReasonUpstream, Err: err}
	}
	if len(decoded.Choices) == 0 {
		return llm.ChatResponse{}, &llm.ProviderError{Reason: llm.ErrorReasonUpstream}
	}
	choice := decoded.Choices[0]
	return llm.ChatResponse{
		Text:         choice.Message.Content,
		FinishReason: mapFinishReason(choice.FinishReason),
	}, nil
}

func (c *Client) Stream(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	body, err := json.Marshal(chatRequestBody(cfg, req, true))
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL(c.proxyBaseURL), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setHeaders(httpReq, cfg)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return singleEvent(llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: errorReasonFromError(mapTransportError(err)), Err: err}), nil
	}
	if resp.StatusCode >= 400 {
		_ = resp.Body.Close()
		return singleEvent(llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: errorReasonFromError(statusError(resp.StatusCode))}), nil
	}

	ch := make(chan llm.StreamEvent)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		parseStream(resp.Body, ch)
	}()
	return ch, nil
}

func chatRequestBody(cfg config.ModelConfig, req llm.ChatRequest, stream bool) map[string]any {
	body := map[string]any{
		"model":    proxyModelName(cfg),
		"messages": req.Messages,
		"stream":   stream,
	}
	if req.Params.Temperature != nil {
		body["temperature"] = *req.Params.Temperature
	}
	if req.Params.MaxTokens != nil {
		body["max_tokens"] = *req.Params.MaxTokens
	}
	for key, value := range req.Params.Extra {
		if reservedChatRequestField(key) {
			continue
		}
		body[key] = value
	}
	return body
}

func reservedChatRequestField(key string) bool {
	switch key {
	case "model", "messages", "stream", "temperature", "max_tokens":
		return true
	default:
		return false
	}
}

func proxyModelName(cfg config.ModelConfig) string {
	if cfg.ID != "" {
		return cfg.ID
	}
	return cfg.Model
}

func chatCompletionsURL(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + "/chat/completions"
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/chat/completions"
	return parsed.String()
}

func setHeaders(req *http.Request, cfg config.ModelConfig) {
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
}

func parseStream(reader io.Reader, ch chan<- llm.StreamEvent) {
	scanner := bufio.NewScanner(reader)
	diagnostics := map[string]int{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop, Diagnostics: diagnostics}
			return
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			diagnostics["invalid_json"]++
			continue
		}
		if len(chunk.Choices) == 0 {
			diagnostics["missing_choices"]++
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			ch <- llm.StreamEvent{Type: llm.StreamEventTypeDelta, Delta: choice.Delta.Content, Diagnostics: diagnostics}
		}
		if choice.FinishReason != "" {
			ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: mapFinishReason(choice.FinishReason), Diagnostics: diagnostics}
			return
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonUpstream, Err: err, Diagnostics: diagnostics}
		return
	}
	ch <- llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonUpstream, Err: io.ErrUnexpectedEOF, Diagnostics: diagnostics}
}

func singleEvent(event llm.StreamEvent) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent, 1)
	ch <- event
	close(ch)
	return ch
}

func mapFinishReason(reason string) llm.FinishReason {
	switch reason {
	case "", "stop":
		return llm.FinishReasonStop
	case "length":
		return llm.FinishReasonLength
	case "content_filter":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonStop
	}
}

func mapTransportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &llm.ProviderError{Reason: llm.ErrorReasonTimeout, Err: err}
	}
	return &llm.ProviderError{Reason: llm.ErrorReasonEndpointUnavailable, Err: err}
}

func statusError(status int) error {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return &llm.ProviderError{Reason: llm.ErrorReasonAuthenticationFailed}
	case http.StatusNotFound:
		return &llm.ProviderError{Reason: llm.ErrorReasonModelNotFound}
	default:
		return &llm.ProviderError{Reason: llm.ErrorReasonUpstream}
	}
}

func errorReasonFromError(err error) llm.ErrorReason {
	var providerErr *llm.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Reason
	}
	return llm.ErrorReasonUpstream
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}
