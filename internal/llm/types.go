package llm

import (
	"context"
	"errors"
	"fmt"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

type ContentSecurityLevel string

const (
	ContentSecurityLevelUnknown      ContentSecurityLevel = ""
	ContentSecurityLevelUnclassified ContentSecurityLevel = "unclassified"
	ContentSecurityLevelSensitive    ContentSecurityLevel = "sensitive"
	ContentSecurityLevelClassified   ContentSecurityLevel = "classified"
)

type Network string

const (
	NetworkPublic  Network = "public"
	NetworkPrivate Network = "private"
)

type FinishReason string

const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonContentFilter FinishReason = "content_filter"
)

type ErrorReason string

const (
	ErrorReasonAuthenticationFailed      ErrorReason = "authentication_failed"
	ErrorReasonEndpointUnavailable       ErrorReason = "endpoint_unavailable"
	ErrorReasonModelNotFound             ErrorReason = "model_not_found"
	ErrorReasonTimeout                   ErrorReason = "timeout"
	ErrorReasonUpstream                  ErrorReason = "upstream_error"
	ErrorReasonNoAvailablePrivateConfig  ErrorReason = "no_available_private_config"
	ErrorReasonInvalidBackendSelection   ErrorReason = "invalid_backend_selection"
	ErrorReasonDesensitizationIncomplete ErrorReason = "desensitization_incomplete"
)

type GenerationParams struct {
	Temperature *float64
	MaxTokens   *int
	Thinking    *bool
	Extra       map[string]any
}

type Route struct {
	ConfigID       string
	RequirePrivate bool
}

type ChatRequest struct {
	Messages             []Message
	Params               GenerationParams
	Route                Route
	ContentSecurityLevel ContentSecurityLevel
	ActorID              string
	RequestID            string
}

func (r ChatRequest) HasContentSecurityLevel() bool {
	return r.ContentSecurityLevel != ContentSecurityLevelUnknown
}

type ChatResponse struct {
	Text         string
	FinishReason FinishReason
}

type StreamEventType string

const (
	StreamEventTypeDelta StreamEventType = "delta"
	StreamEventTypeDone  StreamEventType = "done"
	StreamEventTypeError StreamEventType = "error"
)

type StreamEvent struct {
	Type         StreamEventType
	Delta        string
	FinishReason FinishReason
	ErrorReason  ErrorReason
	Err          error
	Diagnostics  map[string]int
}

type Client interface {
	Complete(context.Context, ChatRequest) (ChatResponse, error)
	Stream(context.Context, ChatRequest) (<-chan StreamEvent, error)
	CurrentNetwork(context.Context) (Network, error)
}

var ErrNoAvailablePrivateConfig = errors.New("no available private model configuration")

type ProviderError struct {
	Reason ErrorReason
	Err    error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Reason, e.Err)
	}
	return string(e.Reason)
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *ProviderError) Is(target error) bool {
	return target == ErrNoAvailablePrivateConfig && e != nil && e.Reason == ErrorReasonNoAvailablePrivateConfig
}
