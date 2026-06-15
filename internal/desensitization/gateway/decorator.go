package gateway

import (
	"context"
	"errors"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type Decorator struct {
	next          llm.Client
	processor     TextProcessor
	routes        RouteConfigStore
	routeResolver RouteNetworkResolver
	audits        DispositionAuditStore
}

type RouteNetworkResolver interface {
	NetworkForRoute(context.Context, llm.Route) (llm.Network, error)
}

type PrivateRouteResolver interface {
	PrivateRoute(context.Context) (llm.Route, bool, error)
}

func NewDecorator(next llm.Client, processor TextProcessor, routes RouteConfigStore) *Decorator {
	return NewDecoratorWithRouteResolver(next, processor, routes, nil)
}

func NewDecoratorWithRouteResolver(next llm.Client, processor TextProcessor, routes RouteConfigStore, resolver RouteNetworkResolver) *Decorator {
	return NewDecoratorWithRouteResolverAndAudit(next, processor, routes, resolver, nil)
}

func NewDecoratorWithRouteResolverAndAudit(
	next llm.Client,
	processor TextProcessor,
	routes RouteConfigStore,
	resolver RouteNetworkResolver,
	audits DispositionAuditStore,
) *Decorator {
	if routes == nil {
		routes = NewMemoryRouteConfigStore()
	}
	if audits == nil {
		if store, ok := routes.(DispositionAuditStore); ok {
			audits = store
		}
	}
	return &Decorator{next: next, processor: processor, routes: routes, routeResolver: resolver, audits: audits}
}

func (d *Decorator) Complete(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	target, err := d.targetNetwork(ctx, req)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	prepared, result, err := d.prepareRequest(ctx, req, target)
	if err != nil {
		return llm.ChatResponse{}, err
	}
	resp, err := d.next.Complete(ctx, prepared)
	if err != nil {
		if auditErr := d.auditPrivateRuntimeFailure(ctx, req, prepared, target, err); auditErr != nil {
			return llm.ChatResponse{}, auditErr
		}
		return llm.ChatResponse{}, err
	}
	resp.Text = result.Restore(resp.Text)
	return resp, nil
}

func (d *Decorator) Stream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	target, err := d.targetNetwork(ctx, req)
	if err != nil {
		return nil, err
	}
	prepared, result, err := d.prepareRequest(ctx, req, target)
	if err != nil {
		return nil, err
	}
	upstream, err := d.next.Stream(ctx, prepared)
	if err != nil {
		if auditErr := d.auditPrivateRuntimeFailure(ctx, req, prepared, target, err); auditErr != nil {
			return nil, auditErr
		}
		return nil, err
	}
	out := make(chan llm.StreamEvent)
	go func() {
		defer close(out)
		buffer := newPlaceholderTailBuffer(result)
		for event := range upstream {
			if event.Type == llm.StreamEventTypeDelta && event.Delta != "" {
				event.Delta = buffer.Push(event.Delta)
			}
			if event.Type == llm.StreamEventTypeError {
				if err := d.auditStreamPrivateRuntimeFailure(ctx, req, prepared, target, event); err != nil {
					out <- llm.StreamEvent{Type: llm.StreamEventTypeError, ErrorReason: llm.ErrorReasonDesensitizationIncomplete, Err: err}
					return
				}
			}
			if event.Type == llm.StreamEventTypeDone {
				if tail := buffer.Flush(); tail != "" {
					out <- llm.StreamEvent{Type: llm.StreamEventTypeDelta, Delta: tail}
				}
			}
			out <- event
		}
		if tail := buffer.Flush(); tail != "" {
			out <- llm.StreamEvent{Type: llm.StreamEventTypeDelta, Delta: tail}
		}
	}()
	return out, nil
}

func (d *Decorator) CurrentNetwork(ctx context.Context) (llm.Network, error) {
	return d.next.CurrentNetwork(ctx)
}

func (d *Decorator) targetNetwork(ctx context.Context, req llm.ChatRequest) (llm.Network, error) {
	if req.Route.RequirePrivate {
		return llm.NetworkPrivate, nil
	}
	if d.routeResolver != nil {
		return d.routeResolver.NetworkForRoute(ctx, req.Route)
	}
	if req.Route.ConfigID != "" {
		return llm.NetworkPublic, nil
	}
	return d.next.CurrentNetwork(ctx)
}

func (d *Decorator) prepareRequest(ctx context.Context, req llm.ChatRequest, target llm.Network) (llm.ChatRequest, SanitizationResult, error) {
	if target == llm.NetworkPrivate {
		return req, SanitizationResult{Text: joinedMessages(req.Messages)}, nil
	}
	policy, err := d.routes.GetPolicy(ctx, normalizeLevel(req.ContentSecurityLevel))
	if err != nil {
		return llm.ChatRequest{}, SanitizationResult{}, err
	}
	if policy.TargetNetwork == llm.NetworkPrivate {
		req.Route.RequirePrivate = true
		if policy.ModelConfigID != "" {
			req.Route.ConfigID = policy.ModelConfigID
		} else {
			req.Route.ConfigID = ""
		}
		if err := d.auditDisposition(ctx, req, DispositionEventRoutePrivate, DispositionReasonClassificationPrivate); err != nil {
			return llm.ChatRequest{}, SanitizationResult{}, err
		}
		return req, SanitizationResult{Text: joinedMessages(req.Messages)}, nil
	}
	if d.processor == nil {
		req.Route.RequirePrivate = true
		req.Route.ConfigID = ""
		if err := d.auditDisposition(ctx, req, DispositionEventRoutePrivate, DispositionReasonProcessorMissingPrivate); err != nil {
			return llm.ChatRequest{}, SanitizationResult{}, err
		}
		return req, SanitizationResult{Text: joinedMessages(req.Messages)}, nil
	}
	if policy.ModelConfigID != "" {
		req.Route.RequirePrivate = false
		req.Route.ConfigID = policy.ModelConfigID
	}

	messages, result, err := d.sanitizeMessages(ctx, req.Messages)
	if isNERUnavailable(err) {
		return d.prepareNERUnavailable(ctx, req, policy)
	}
	if err != nil {
		return llm.ChatRequest{}, SanitizationResult{}, err
	}
	if err := d.auditPublicSanitization(ctx, req, result); err != nil {
		return llm.ChatRequest{}, SanitizationResult{}, err
	}
	req.Messages = messages
	return req, result, nil
}

func (d *Decorator) sanitizeMessages(ctx context.Context, messages []llm.Message) ([]llm.Message, SanitizationResult, error) {
	if processor, ok := d.processor.(ContextTextProcessor); ok {
		return processor.SanitizeMessagesContext(ctx, messages)
	}
	messages, result := d.processor.SanitizeMessages(messages)
	return messages, result, nil
}

func (d *Decorator) prepareNERUnavailable(ctx context.Context, req llm.ChatRequest, policy RoutePolicy) (llm.ChatRequest, SanitizationResult, error) {
	auditResult := d.fallbackAuditResult(req)
	if route, ok, err := d.privateRoute(ctx); err != nil {
		return llm.ChatRequest{}, SanitizationResult{}, err
	} else if ok {
		req.Route = route
		req.Route.RequirePrivate = true
		if err := d.auditDispositionWithResult(ctx, req, DispositionEventRoutePrivate, DispositionReasonNERUnavailablePrivateAvailable, auditResult); err != nil {
			return llm.ChatRequest{}, SanitizationResult{}, err
		}
		return req, SanitizationResult{Text: joinedMessages(req.Messages)}, nil
	}

	if policy.AllowDegradedPublic && canDegrade(policy.Level) {
		if policy.ModelConfigID != "" {
			req.Route.ConfigID = policy.ModelConfigID
		}
		req.Route.RequirePrivate = false
		original := req
		messages, result := d.processor.SanitizeMessages(req.Messages)
		req.Messages = messages
		if err := d.auditDispositionWithResult(ctx, original, DispositionEventDegradedPublic, DispositionReasonNERUnavailableDegradedPublic, result); err != nil {
			return llm.ChatRequest{}, SanitizationResult{}, err
		}
		return req, result, nil
	}
	if err := d.auditDispositionWithResult(ctx, req, DispositionEventBlocked, DispositionReasonNERUnavailableNoPrivateNoDegrade, auditResult); err != nil {
		return llm.ChatRequest{}, SanitizationResult{}, err
	}
	return llm.ChatRequest{}, SanitizationResult{}, &llm.ProviderError{
		Reason: llm.ErrorReasonDesensitizationIncomplete,
		Err:    ErrDesensitizationIncomplete,
	}
}

func (d *Decorator) fallbackAuditResult(req llm.ChatRequest) SanitizationResult {
	if d.processor == nil {
		return SanitizationResult{Text: joinedMessages(req.Messages)}
	}
	_, result := d.processor.SanitizeMessages(req.Messages)
	return result
}

func (d *Decorator) privateRoute(ctx context.Context) (llm.Route, bool, error) {
	resolver, ok := d.routeResolver.(PrivateRouteResolver)
	if !ok || resolver == nil {
		return llm.Route{}, false, nil
	}
	return resolver.PrivateRoute(ctx)
}

func canDegrade(level llm.ContentSecurityLevel) bool {
	level = normalizeLevel(level)
	return level != llm.ContentSecurityLevelClassified && level != llm.ContentSecurityLevelUnknown
}

func (d *Decorator) auditPrivateRuntimeFailure(
	ctx context.Context,
	original llm.ChatRequest,
	prepared llm.ChatRequest,
	target llm.Network,
	err error,
) error {
	if target == llm.NetworkPrivate || !prepared.Route.RequirePrivate {
		return nil
	}
	reason := DispositionReasonPrivateRuntimeFailure
	if errors.Is(err, llm.ErrNoAvailablePrivateConfig) {
		reason = DispositionReasonNoAvailablePrivateConfig
	}
	if errors.Is(err, ErrDesensitizationIncomplete) {
		reason = DispositionReasonDesensitizationIncomplete
	}
	return d.auditDispositionWithResult(ctx, original, DispositionEventBlocked, reason, d.fallbackAuditResult(original))
}

func (d *Decorator) auditStreamPrivateRuntimeFailure(
	ctx context.Context,
	original llm.ChatRequest,
	prepared llm.ChatRequest,
	target llm.Network,
	event llm.StreamEvent,
) error {
	if event.Err != nil {
		return d.auditPrivateRuntimeFailure(ctx, original, prepared, target, event.Err)
	}
	if target == llm.NetworkPrivate || !prepared.Route.RequirePrivate {
		return nil
	}
	reason := DispositionReasonPrivateRuntimeFailure
	if event.ErrorReason == llm.ErrorReasonNoAvailablePrivateConfig {
		reason = DispositionReasonNoAvailablePrivateConfig
	}
	if event.ErrorReason == llm.ErrorReasonDesensitizationIncomplete {
		reason = DispositionReasonDesensitizationIncomplete
	}
	return d.auditDispositionWithResult(ctx, original, DispositionEventBlocked, reason, d.fallbackAuditResult(original))
}

func (d *Decorator) auditDisposition(ctx context.Context, req llm.ChatRequest, event DispositionEvent, reason DispositionReason) error {
	return d.auditDispositionWithResult(ctx, req, event, reason, SanitizationResult{})
}

func (d *Decorator) auditPublicSanitization(ctx context.Context, req llm.ChatRequest, result SanitizationResult) error {
	return d.auditDispositionWithResult(ctx, req, DispositionEventPublicSanitized, DispositionReasonNormalPublicCall, result)
}

func (d *Decorator) auditDispositionWithResult(ctx context.Context, req llm.ChatRequest, event DispositionEvent, reason DispositionReason, result SanitizationResult) error {
	if d.audits == nil {
		return ErrDispositionAuditRequired
	}
	diff := "{}"
	matches := "[]"
	if result.Text != "" || len(result.Matches) > 0 {
		diff = auditDiffWithMessagesJSON(joinedMessages(req.Messages), result.Text, result.MessageDiffs)
		matches = matchDetailsJSON(result.Matches)
	}
	return d.audits.AppendDispositionAudit(ctx, DispositionAuditEntry{
		ActorID:               req.ActorID,
		RequestID:             req.RequestID,
		ContentClassification: normalizeLevel(req.ContentSecurityLevel),
		OriginalDiff:          diff,
		MatchDetails:          matches,
		DispositionEvent:      event,
		DispositionReason:     reason,
	})
}

func joinedMessages(messages []llm.Message) string {
	var out string
	for _, msg := range messages {
		out += msg.Content
	}
	return out
}
