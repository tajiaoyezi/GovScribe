package config

import (
	"context"
	"errors"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestCreateConfigMasksAPIKeyAndWritesSafeAudit(t *testing.T) {
	store := NewMemoryStore()
	auth := &recordingAuthorizer{}
	svc := NewService(store, auth, nil)

	created, err := svc.Create(context.Background(), Principal{ID: "admin-1"}, CreateRequest{
		Provider: ProviderOpenAI,
		BaseURL:  "https://api.openai.com/v1",
		APIKey:   "sk-secret-value",
		Model:    "gpt-test",
		Network:  llm.NetworkPublic,
	})
	if err != nil {
		t.Fatalf("create config failed: %v", err)
	}
	if created.APIKeyMasked == "" || created.APIKeyMasked == "sk-secret-value" {
		t.Fatalf("api key mask = %q, want masked non-empty value", created.APIKeyMasked)
	}
	if auth.lastPermission != PermissionModelConfig {
		t.Fatalf("permission = %q, want model.config", auth.lastPermission)
	}

	stored, err := store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("stored config missing: %v", err)
	}
	if stored.APIKey != "sk-secret-value" {
		t.Fatalf("stored api key = %q, want original secret kept for outbound use", stored.APIKey)
	}

	audits := store.Audits()
	if len(audits) != 1 {
		t.Fatalf("audit count = %d, want 1", len(audits))
	}
	if audits[0].ActorID != "admin-1" || audits[0].Action != AuditActionCreate {
		t.Fatalf("audit = %#v, want create by admin-1", audits[0])
	}
	if audits[0].ContainsSecret("sk-secret-value") {
		t.Fatalf("audit must not contain api key plaintext: %#v", audits[0])
	}
}

func TestDisableCurrentConfigIsRejected(t *testing.T) {
	store := NewMemoryStore()
	cfg := ModelConfig{
		ID:          "cfg-current",
		Provider:    ProviderOpenAI,
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      "sk-secret",
		Model:       "gpt-test",
		Network:     llm.NetworkPublic,
		Enabled:     true,
		ProbePassed: true,
		IsCurrent:   true,
	}
	if err := store.Save(context.Background(), cfg); err != nil {
		t.Fatalf("save seed config: %v", err)
	}

	svc := NewService(store, allowAuthorizer{}, nil)
	err := svc.Disable(context.Background(), Principal{ID: "admin-1"}, "cfg-current")
	if !errors.Is(err, ErrCurrentConfigRequiresReplacement) {
		t.Fatalf("disable error = %v, want ErrCurrentConfigRequiresReplacement", err)
	}
}

func TestSwitchRequiresEnabledAndPassingProbe(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-disabled", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
		APIKey: "sk-secret", Model: "gpt-test", Network: llm.NetworkPublic, Enabled: false,
	}); err != nil {
		t.Fatalf("save disabled config: %v", err)
	}
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-enabled", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
		APIKey: "sk-secret", Model: "gpt-test", Network: llm.NetworkPublic, Enabled: true,
	}); err != nil {
		t.Fatalf("save enabled config: %v", err)
	}

	svc := NewService(store, allowAuthorizer{}, staticProber{
		result: ProbeResult{Available: false, ErrorReason: llm.ErrorReasonAuthenticationFailed},
	})
	if err := svc.SwitchCurrent(context.Background(), Principal{ID: "admin-1"}, "cfg-disabled"); !errors.Is(err, ErrConfigDisabled) {
		t.Fatalf("disabled switch error = %v, want ErrConfigDisabled", err)
	}
	if err := svc.SwitchCurrent(context.Background(), Principal{ID: "admin-1"}, "cfg-enabled"); !errors.Is(err, ErrProbeFailed) {
		t.Fatalf("probe switch error = %v, want ErrProbeFailed", err)
	}

	svc = NewService(store, allowAuthorizer{}, staticProber{result: ProbeResult{Available: true}})
	if err := svc.SwitchCurrent(context.Background(), Principal{ID: "admin-1"}, "cfg-enabled"); err != nil {
		t.Fatalf("switch enabled config with passing probe: %v", err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("current config missing: %v", err)
	}
	if current.ID != "cfg-enabled" {
		t.Fatalf("current id = %q, want cfg-enabled", current.ID)
	}
}

func TestSwitchWithoutProberRequiresStoredProbePassed(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-unprobed", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
		APIKey: "sk-secret", Model: "gpt-test", Network: llm.NetworkPublic, Enabled: true,
	}); err != nil {
		t.Fatalf("save unprobed config: %v", err)
	}
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-probed", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
		APIKey: "sk-secret", Model: "gpt-test", Network: llm.NetworkPublic, Enabled: true, ProbePassed: true,
	}); err != nil {
		t.Fatalf("save probed config: %v", err)
	}

	svc := NewService(store, allowAuthorizer{}, nil)
	if err := svc.SwitchCurrent(context.Background(), Principal{ID: "admin-1"}, "cfg-unprobed"); !errors.Is(err, ErrProbeFailed) {
		t.Fatalf("unprobed switch error = %v, want ErrProbeFailed", err)
	}
	if err := svc.SwitchCurrent(context.Background(), Principal{ID: "admin-1"}, "cfg-probed"); err != nil {
		t.Fatalf("probed switch without prober: %v", err)
	}
}

func TestUpdateCurrentConfigRequiresPassingProbeBeforeSaving(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-current", Provider: ProviderOpenAI, BaseURL: "https://old.example/v1",
		APIKey: "sk-old", Model: "gpt-old", Network: llm.NetworkPublic, Enabled: true, ProbePassed: true, IsCurrent: true,
	}); err != nil {
		t.Fatalf("save current config: %v", err)
	}

	svc := NewService(store, allowAuthorizer{}, staticProber{
		result: ProbeResult{Available: false, ErrorReason: llm.ErrorReasonEndpointUnavailable},
	})
	_, err := svc.Update(context.Background(), Principal{ID: "admin-1"}, "cfg-current", UpdateRequest{
		BaseURL: "https://new.example/v1",
		APIKey:  "sk-new",
		Model:   "gpt-new",
	})
	if !errors.Is(err, ErrProbeFailed) {
		t.Fatalf("update current error = %v, want ErrProbeFailed", err)
	}
	stored, err := store.Get(context.Background(), "cfg-current")
	if err != nil {
		t.Fatalf("get current config: %v", err)
	}
	if stored.BaseURL != "https://old.example/v1" || stored.APIKey != "sk-old" || stored.Model != "gpt-old" {
		t.Fatalf("failed current update must not save candidate config: %#v", stored)
	}

	svc = NewService(store, allowAuthorizer{}, staticProber{result: ProbeResult{Available: true}})
	updated, err := svc.Update(context.Background(), Principal{ID: "admin-1"}, "cfg-current", UpdateRequest{
		BaseURL: "https://new.example/v1",
		APIKey:  "sk-new",
		Model:   "gpt-new",
	})
	if err != nil {
		t.Fatalf("update current with passing probe: %v", err)
	}
	if updated.BaseURL != "https://new.example/v1" || !updated.ProbePassed {
		t.Fatalf("updated current public view = %#v, want new base URL and probe passed", updated)
	}
}

func TestUpdateCurrentConfigReturnsSwitchNotice(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-current", Provider: ProviderOpenAI, BaseURL: "https://old.example/v1",
		APIKey: "sk-old", Model: "gpt-old", Network: llm.NetworkPublic, Enabled: true, ProbePassed: true, IsCurrent: true,
	}); err != nil {
		t.Fatalf("save current config: %v", err)
	}
	svc := NewService(store, allowAuthorizer{}, staticProber{result: ProbeResult{Available: true}})

	result, err := svc.UpdateWithNotice(context.Background(), Principal{ID: "admin-1"}, "cfg-current", UpdateRequest{
		Model: "gpt-new",
	})
	if err != nil {
		t.Fatalf("update current with notice: %v", err)
	}
	if result.Switch == nil {
		t.Fatal("current config update must return switch semantics notice")
	}
	if result.Switch.AppliesTo != SwitchAppliesToNewRequests || result.Switch.PromisesImmediateGlobalEffect() {
		t.Fatalf("switch notice = %#v, want new-request-only non-immediate notice", result.Switch)
	}
}

func TestSwitchNoticeDoesNotPromiseImmediateGlobalEffect(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-enabled", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
		APIKey: "sk-secret", Model: "gpt-test", Network: llm.NetworkPublic, Enabled: true,
	}); err != nil {
		t.Fatalf("save enabled config: %v", err)
	}
	svc := NewService(store, allowAuthorizer{}, staticProber{result: ProbeResult{Available: true}})

	result, err := svc.SwitchCurrentWithNotice(context.Background(), Principal{ID: "admin-1"}, "cfg-enabled")
	if err != nil {
		t.Fatalf("switch current with notice: %v", err)
	}
	if result.AppliesTo != SwitchAppliesToNewRequests {
		t.Fatalf("applies to = %q, want new requests", result.AppliesTo)
	}
	if result.LiteLLMPropagationDelaySeconds != 30 {
		t.Fatalf("propagation delay = %d, want 30", result.LiteLLMPropagationDelaySeconds)
	}
	if result.Notice == "" || result.PromisesImmediateGlobalEffect() {
		t.Fatalf("notice = %#v, want explicit non-immediate behavior", result)
	}
}

func TestListUpdateAndEnableUseMaskedPublicViews(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, allowAuthorizer{}, nil)
	created, err := svc.Create(context.Background(), Principal{ID: "admin-1"}, CreateRequest{
		Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1", APIKey: "sk-original",
		Model: "gpt-old", Network: llm.NetworkPublic,
	})
	if err != nil {
		t.Fatalf("create config: %v", err)
	}

	updated, err := svc.Update(context.Background(), Principal{ID: "admin-1"}, created.ID, UpdateRequest{
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-updated",
		Model:   "gpt-new",
	})
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if updated.APIKeyMasked == "sk-updated" || updated.Model != "gpt-new" {
		t.Fatalf("updated public view = %#v, want masked key and new model", updated)
	}

	if err := svc.Disable(context.Background(), Principal{ID: "admin-1"}, created.ID); err != nil {
		t.Fatalf("disable config: %v", err)
	}
	if err := svc.Enable(context.Background(), Principal{ID: "admin-1"}, created.ID); err != nil {
		t.Fatalf("enable config: %v", err)
	}

	configs, err := svc.List(context.Background(), Principal{ID: "admin-1"})
	if err != nil {
		t.Fatalf("list configs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("config count = %d, want 1", len(configs))
	}
	if configs[0].APIKeyMasked == "sk-updated" || !configs[0].Enabled {
		t.Fatalf("listed public config = %#v, want masked enabled config", configs[0])
	}
}

func TestProbeReturnsAvailabilityAndReason(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Save(context.Background(), ModelConfig{
		ID: "cfg-probe", Provider: ProviderOpenAI, BaseURL: "https://api.openai.com/v1",
		APIKey: "sk-secret", Model: "missing-model", Network: llm.NetworkPublic, Enabled: true,
	}); err != nil {
		t.Fatalf("save probe config: %v", err)
	}

	svc := NewService(store, allowAuthorizer{}, staticProber{
		result: ProbeResult{Available: false, ErrorReason: llm.ErrorReasonModelNotFound},
	})
	result, err := svc.Probe(context.Background(), Principal{ID: "admin-1"}, "cfg-probe")
	if err != nil {
		t.Fatalf("probe config: %v", err)
	}
	if result.Available || result.ErrorReason != llm.ErrorReasonModelNotFound {
		t.Fatalf("probe result = %#v, want unavailable model_not_found", result)
	}
	stored, err := store.Get(context.Background(), "cfg-probe")
	if err != nil {
		t.Fatalf("get probed config: %v", err)
	}
	if stored.ProbePassed {
		t.Fatal("failed probe must not mark config as probe-passed")
	}
}

type recordingAuthorizer struct {
	lastPermission Permission
}

func (a *recordingAuthorizer) Authorize(_ context.Context, _ Principal, permission Permission) error {
	a.lastPermission = permission
	return nil
}

type allowAuthorizer struct{}

func (allowAuthorizer) Authorize(context.Context, Principal, Permission) error {
	return nil
}

type staticProber struct {
	result ProbeResult
}

func (s staticProber) Probe(context.Context, ModelConfig) ProbeResult {
	return s.result
}
