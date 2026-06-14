package config

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type Provider string

const (
	ProviderOpenAI           Provider = "openai"
	ProviderAnthropic        Provider = "anthropic"
	ProviderOpenAICompatible Provider = "openai_compatible"
)

type Permission string

const PermissionModelConfig Permission = "model.config"

type Principal struct {
	ID string
}

type Authorizer interface {
	Authorize(context.Context, Principal, Permission) error
}

type Prober interface {
	Probe(context.Context, ModelConfig) ProbeResult
}

type ProbeResult struct {
	Available   bool
	ErrorReason llm.ErrorReason
	Message     string
}

type SwitchAppliesTo string

const SwitchAppliesToNewRequests SwitchAppliesTo = "new_requests"

type SwitchResult struct {
	ConfigID                       string
	AppliesTo                      SwitchAppliesTo
	LiteLLMPropagationDelaySeconds int
	Notice                         string
}

func (r SwitchResult) PromisesImmediateGlobalEffect() bool {
	return strings.Contains(r.Notice, "立即全局生效") || strings.Contains(r.Notice, "immediate global")
}

type ModelConfig struct {
	ID          string
	Provider    Provider
	BaseURL     string
	APIKey      string
	Model       string
	Network     llm.Network
	Enabled     bool
	ProbePassed bool
	IsCurrent   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type PublicModelConfig struct {
	ID           string
	Provider     Provider
	BaseURL      string
	APIKeyMasked string
	Model        string
	Network      llm.Network
	Enabled      bool
	ProbePassed  bool
	IsCurrent    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateRequest struct {
	Provider Provider
	BaseURL  string
	APIKey   string
	Model    string
	Network  llm.Network
}

type UpdateRequest struct {
	BaseURL string
	APIKey  string
	Model   string
}

type AuditAction string

const (
	AuditActionCreate  AuditAction = "create"
	AuditActionUpdate  AuditAction = "update"
	AuditActionEnable  AuditAction = "enable"
	AuditActionDisable AuditAction = "disable"
	AuditActionSwitch  AuditAction = "switch"
	AuditActionProbe   AuditAction = "probe"
)

type AuditEntry struct {
	ActorID  string
	ConfigID string
	Action   AuditAction
	At       time.Time
	Details  map[string]string
}

func (a AuditEntry) ContainsSecret(secret string) bool {
	if secret == "" {
		return false
	}
	if strings.Contains(a.ActorID, secret) || strings.Contains(a.ConfigID, secret) || strings.Contains(string(a.Action), secret) {
		return true
	}
	for k, v := range a.Details {
		if strings.Contains(k, secret) || strings.Contains(v, secret) {
			return true
		}
	}
	return false
}

var (
	ErrConfigNotFound                   = errors.New("model config not found")
	ErrConfigDisabled                   = errors.New("model config is disabled")
	ErrCurrentConfigRequiresReplacement = errors.New("current config must be replaced before disabling")
	ErrProbeFailed                      = errors.New("model config probe failed")
	ErrUnauthorized                     = errors.New("unauthorized model config access")
)
