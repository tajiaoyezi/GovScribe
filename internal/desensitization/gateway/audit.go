package gateway

import (
	"context"
	"time"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type DispositionEvent string

const (
	DispositionEventRoutePrivate   DispositionEvent = "route_private"
	DispositionEventDegradedPublic DispositionEvent = "degraded_public"
	DispositionEventBlocked        DispositionEvent = "blocked"
)

type DispositionReason string

const (
	DispositionReasonClassificationPrivate            DispositionReason = "classification_private"
	DispositionReasonProcessorMissingPrivate          DispositionReason = "processor_missing_private"
	DispositionReasonNERUnavailablePrivateAvailable   DispositionReason = "ner_unavailable_private_available"
	DispositionReasonNERUnavailableDegradedPublic     DispositionReason = "ner_unavailable_degraded_public"
	DispositionReasonNERUnavailableNoPrivateNoDegrade DispositionReason = "ner_unavailable_no_private_no_degrade"
	DispositionReasonPrivateRuntimeFailure            DispositionReason = "private_runtime_failure"
	DispositionReasonNoAvailablePrivateConfig         DispositionReason = "no_available_private_config"
	DispositionReasonDesensitizationIncomplete        DispositionReason = "desensitization_incomplete"
)

type DispositionAuditEntry struct {
	ActorID               string
	RequestID             string
	ContentClassification llm.ContentSecurityLevel
	OriginalDiff          string
	MatchDetails          string
	DispositionEvent      DispositionEvent
	DispositionReason     DispositionReason
	At                    time.Time
}

type DispositionAuditStore interface {
	AppendDispositionAudit(context.Context, DispositionAuditEntry) error
}

func normalizeDispositionAuditEntry(entry DispositionAuditEntry) DispositionAuditEntry {
	if entry.ActorID == "" {
		entry.ActorID = "system"
	}
	if entry.RequestID == "" {
		entry.RequestID = "unknown"
	}
	entry.ContentClassification = normalizeLevel(entry.ContentClassification)
	if entry.OriginalDiff == "" {
		entry.OriginalDiff = "{}"
	}
	if entry.MatchDetails == "" {
		entry.MatchDetails = "[]"
	}
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	return entry
}
