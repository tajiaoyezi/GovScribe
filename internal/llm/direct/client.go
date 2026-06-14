package direct

import (
	"context"
	"errors"
	"net/http"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	openai "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
	"github.com/tajiaoyezi/GovScribe/internal/llm/config"
)

const defaultAnthropicMaxTokens = 1024

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Complete(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) (llm.ChatResponse, error) {
	switch cfg.Provider {
	case config.ProviderOpenAI, config.ProviderOpenAICompatible:
		return c.completeOpenAI(ctx, cfg, req)
	case config.ProviderAnthropic:
		return c.completeAnthropic(ctx, cfg, req)
	default:
		return llm.ChatResponse{}, &llm.ProviderError{Reason: llm.ErrorReasonInvalidBackendSelection}
	}
}

func (c *Client) Stream(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	switch cfg.Provider {
	case config.ProviderOpenAI, config.ProviderOpenAICompatible:
		return c.streamOpenAI(ctx, cfg, req), nil
	case config.ProviderAnthropic:
		return c.streamAnthropic(ctx, cfg, req), nil
	default:
		return singleEvent(llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonInvalidBackendSelection}), nil
	}
}

func (c *Client) completeOpenAI(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) (llm.ChatResponse, error) {
	client := openai.NewClient(
		openaioption.WithAPIKey(cfg.APIKey),
		openaioption.WithBaseURL(cfg.BaseURL),
	)
	resp, err := client.Chat.Completions.New(ctx, openAIParams(cfg, req))
	if err != nil {
		return llm.ChatResponse{}, mapSDKError(err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return llm.ChatResponse{}, &llm.ProviderError{Reason: llm.ErrorReasonUpstream}
	}
	choice := resp.Choices[0]
	return llm.ChatResponse{
		Text:         choice.Message.Content,
		FinishReason: mapOpenAIFinishReason(choice.FinishReason),
	}, nil
}

func (c *Client) streamOpenAI(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent)
	go func() {
		defer close(ch)
		client := openai.NewClient(
			openaioption.WithAPIKey(cfg.APIKey),
			openaioption.WithBaseURL(cfg.BaseURL),
		)
		stream := client.Chat.Completions.NewStreaming(ctx, openAIParams(cfg, req))
		defer stream.Close()
		done := false
		for stream.Next() {
			chunk := stream.Current()
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if choice.Delta.Content != "" {
				ch <- llm.StreamEvent{Type: llm.StreamEventTypeDelta, Delta: choice.Delta.Content}
			}
			if choice.FinishReason != "" {
				done = true
				ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: mapOpenAIFinishReason(choice.FinishReason)}
				return
			}
		}
		if err := stream.Err(); err != nil {
			ch <- llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: errorReasonFromError(mapSDKError(err)), Err: err}
			return
		}
		if !done {
			ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop}
		}
	}()
	return ch
}

func openAIParams(cfg config.ModelConfig, req llm.ChatRequest) openai.ChatCompletionNewParams {
	params := openai.ChatCompletionNewParams{
		Model:    cfg.Model,
		Messages: openAIMessages(req.Messages),
	}
	if req.Params.Temperature != nil {
		params.Temperature = openai.Float(*req.Params.Temperature)
	}
	if req.Params.MaxTokens != nil {
		params.MaxTokens = openai.Int(int64(*req.Params.MaxTokens))
	}
	return params
}

func openAIMessages(messages []llm.Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case llm.RoleSystem:
			out = append(out, openai.SystemMessage(msg.Content))
		case llm.RoleAssistant:
			out = append(out, openai.AssistantMessage(msg.Content))
		default:
			out = append(out, openai.UserMessage(msg.Content))
		}
	}
	return out
}

func (c *Client) completeAnthropic(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) (llm.ChatResponse, error) {
	client := anthropic.NewClient(
		anthropicoption.WithoutEnvironmentDefaults(),
		anthropicoption.WithAPIKey(cfg.APIKey),
		anthropicoption.WithBaseURL(cfg.BaseURL),
	)
	resp, err := client.Messages.New(ctx, anthropicParams(cfg, req))
	if err != nil {
		return llm.ChatResponse{}, mapSDKError(err)
	}
	if resp == nil {
		return llm.ChatResponse{}, &llm.ProviderError{Reason: llm.ErrorReasonUpstream}
	}
	return llm.ChatResponse{
		Text:         anthropicText(resp.Content),
		FinishReason: mapAnthropicStopReason(string(resp.StopReason)),
	}, nil
}

func (c *Client) streamAnthropic(ctx context.Context, cfg config.ModelConfig, req llm.ChatRequest) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent)
	go func() {
		defer close(ch)
		client := anthropic.NewClient(
			anthropicoption.WithoutEnvironmentDefaults(),
			anthropicoption.WithAPIKey(cfg.APIKey),
			anthropicoption.WithBaseURL(cfg.BaseURL),
		)
		stream := client.Messages.NewStreaming(ctx, anthropicParams(cfg, req))
		defer stream.Close()
		done := false
		for stream.Next() {
			event := stream.Current()
			if event.Type == "content_block_delta" && event.Delta.Text != "" {
				ch <- llm.StreamEvent{Type: llm.StreamEventTypeDelta, Delta: event.Delta.Text}
			}
			if event.Type == "message_delta" && event.Delta.StopReason != "" {
				done = true
				ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: mapAnthropicStopReason(string(event.Delta.StopReason))}
				return
			}
			if event.Type == "message_stop" && !done {
				ch <- llm.StreamEvent{Type: llm.StreamEventTypeDone, FinishReason: llm.FinishReasonStop}
				return
			}
		}
		if err := stream.Err(); err != nil {
			ch <- llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: errorReasonFromError(mapSDKError(err)), Err: err}
		}
	}()
	return ch
}

func anthropicParams(cfg config.ModelConfig, req llm.ChatRequest) anthropic.MessageNewParams {
	maxTokens := defaultAnthropicMaxTokens
	if req.Params.MaxTokens != nil {
		maxTokens = *req.Params.MaxTokens
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(cfg.Model),
		MaxTokens: int64(maxTokens),
	}
	params.System, params.Messages = anthropicMessages(req.Messages)
	if req.Params.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Params.Temperature)
	}
	if req.Params.Thinking != nil && *req.Params.Thinking {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(1024)
	}
	return params
}

func anthropicMessages(messages []llm.Message) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	system := make([]anthropic.TextBlockParam, 0)
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == llm.RoleSystem {
			system = append(system, anthropic.TextBlockParam{Text: msg.Content})
			continue
		}
		block := anthropic.NewTextBlock(msg.Content)
		if msg.Role == llm.RoleAssistant {
			out = append(out, anthropic.NewAssistantMessage(block))
			continue
		}
		out = append(out, anthropic.NewUserMessage(block))
	}
	return system, out
}

func anthropicText(blocks []anthropic.ContentBlockUnion) string {
	var builder strings.Builder
	for _, block := range blocks {
		if block.Type == "text" && block.Text != "" {
			builder.WriteString(block.Text)
		}
	}
	return builder.String()
}

func mapOpenAIFinishReason(reason string) llm.FinishReason {
	switch reason {
	case "", "stop", "tool_calls", "function_call":
		return llm.FinishReasonStop
	case "length":
		return llm.FinishReasonLength
	case "content_filter":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonStop
	}
}

func mapAnthropicStopReason(reason string) llm.FinishReason {
	switch reason {
	case "max_tokens":
		return llm.FinishReasonLength
	case "refusal":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonStop
	}
}

func mapSDKError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &llm.ProviderError{Reason: llm.ErrorReasonTimeout, Err: err}
	}
	var openAIError *openai.Error
	if errors.As(err, &openAIError) {
		return &llm.ProviderError{Reason: reasonFromStatus(openAIError.StatusCode), Err: err}
	}
	var anthropicError *anthropic.Error
	if errors.As(err, &anthropicError) {
		return &llm.ProviderError{Reason: reasonFromStatus(anthropicError.StatusCode), Err: err}
	}
	return &llm.ProviderError{Reason: llm.ErrorReasonEndpointUnavailable, Err: err}
}

func reasonFromStatus(status int) llm.ErrorReason {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return llm.ErrorReasonAuthenticationFailed
	case http.StatusNotFound:
		return llm.ErrorReasonModelNotFound
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return llm.ErrorReasonTimeout
	default:
		return llm.ErrorReasonUpstream
	}
}

func errorReasonFromError(err error) llm.ErrorReason {
	var providerErr *llm.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Reason
	}
	return llm.ErrorReasonUpstream
}

func singleEvent(event llm.StreamEvent) <-chan llm.StreamEvent {
	ch := make(chan llm.StreamEvent, 1)
	ch <- event
	close(ch)
	return ch
}
