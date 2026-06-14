package config

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

type Service struct {
	store  Store
	auth   Authorizer
	probe  Prober
	syncer ModelConfigSyncer
	now    func() time.Time
}

func NewService(store Store, auth Authorizer, prober Prober) *Service {
	return NewServiceWithSyncer(store, auth, prober, nil)
}

func NewServiceWithSyncer(store Store, auth Authorizer, prober Prober, syncer ModelConfigSyncer) *Service {
	return &Service{
		store:  store,
		auth:   auth,
		probe:  prober,
		syncer: syncer,
		now:    time.Now,
	}
}

func (s *Service) Create(ctx context.Context, principal Principal, req CreateRequest) (PublicModelConfig, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return PublicModelConfig{}, err
	}
	now := s.now()
	cfg := ModelConfig{
		ID:        newID(),
		Provider:  req.Provider,
		BaseURL:   req.BaseURL,
		APIKey:    req.APIKey,
		Model:     req.Model,
		Network:   req.Network,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.SaveWithAudit(ctx, cfg, s.auditEntry(principal, cfg.ID, AuditActionCreate, map[string]string{
		"provider": string(cfg.Provider),
		"network":  string(cfg.Network),
	})); err != nil {
		return PublicModelConfig{}, err
	}
	return publicConfig(cfg), nil
}

func (s *Service) List(ctx context.Context, principal Principal) ([]PublicModelConfig, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return nil, err
	}
	configs, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(configs, func(i, j int) bool { return configs[i].ID < configs[j].ID })
	out := make([]PublicModelConfig, 0, len(configs))
	for _, cfg := range configs {
		out = append(out, publicConfig(cfg))
	}
	return out, nil
}

func (s *Service) Update(ctx context.Context, principal Principal, id string, req UpdateRequest) (UpdateResult, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return UpdateResult{}, err
	}
	cfg, err := s.store.Get(ctx, id)
	if err != nil {
		return UpdateResult{}, err
	}
	changed := false
	if req.BaseURL != "" {
		changed = changed || cfg.BaseURL != req.BaseURL
		cfg.BaseURL = req.BaseURL
	}
	if req.APIKey != "" {
		changed = changed || cfg.APIKey != req.APIKey
		cfg.APIKey = req.APIKey
	}
	if req.Model != "" {
		changed = changed || cfg.Model != req.Model
		cfg.Model = req.Model
	}
	if changed {
		cfg.ProbePassed = false
	}
	if cfg.IsCurrent && changed {
		if err := s.probeForCurrent(ctx, &cfg); err != nil {
			return UpdateResult{}, err
		}
		if err := s.syncModelConfig(ctx, cfg); err != nil {
			return UpdateResult{}, err
		}
	}
	cfg.UpdatedAt = s.now()
	if err := s.store.SaveWithAudit(ctx, cfg, s.auditEntry(principal, id, AuditActionUpdate, map[string]string{"current": boolString(cfg.IsCurrent)})); err != nil {
		return UpdateResult{}, err
	}
	result := UpdateResult{Config: publicConfig(cfg)}
	if cfg.IsCurrent && changed {
		switchResult := newSwitchResult(id)
		result.Switch = &switchResult
	}
	return result, nil
}

func (s *Service) UpdateWithNotice(ctx context.Context, principal Principal, id string, req UpdateRequest) (UpdateResult, error) {
	return s.Update(ctx, principal, id, req)
}

func (s *Service) Enable(ctx context.Context, principal Principal, id string) error {
	if err := s.authorize(ctx, principal); err != nil {
		return err
	}
	cfg, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	cfg.Enabled = true
	cfg.UpdatedAt = s.now()
	return s.store.SaveWithAudit(ctx, cfg, s.auditEntry(principal, id, AuditActionEnable, nil))
}

func (s *Service) Disable(ctx context.Context, principal Principal, id string) error {
	if err := s.authorize(ctx, principal); err != nil {
		return err
	}
	cfg, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if cfg.IsCurrent {
		return ErrCurrentConfigRequiresReplacement
	}
	cfg.Enabled = false
	cfg.UpdatedAt = s.now()
	return s.store.SaveWithAudit(ctx, cfg, s.auditEntry(principal, id, AuditActionDisable, nil))
}

func (s *Service) SwitchCurrent(ctx context.Context, principal Principal, id string) error {
	_, err := s.SwitchCurrentWithNotice(ctx, principal, id)
	return err
}

func (s *Service) SwitchCurrentWithNotice(ctx context.Context, principal Principal, id string) (SwitchResult, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return SwitchResult{}, err
	}
	cfg, err := s.store.Get(ctx, id)
	if err != nil {
		return SwitchResult{}, err
	}
	if !cfg.Enabled {
		return SwitchResult{}, ErrConfigDisabled
	}
	if err := s.probeForCurrent(ctx, &cfg); err != nil {
		return SwitchResult{}, err
	}
	if err := s.syncModelConfig(ctx, cfg); err != nil {
		return SwitchResult{}, err
	}
	cfg.UpdatedAt = s.now()
	if err := s.store.SaveAndSetCurrentWithAudit(ctx, cfg, s.auditEntry(principal, id, AuditActionSwitch, map[string]string{"effect": "new_requests"})); err != nil {
		return SwitchResult{}, err
	}
	return newSwitchResult(id), nil
}

func (s *Service) Probe(ctx context.Context, principal Principal, id string) (ProbeResult, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return ProbeResult{}, err
	}
	cfg, err := s.store.Get(ctx, id)
	if err != nil {
		return ProbeResult{}, err
	}
	if s.probe == nil {
		return ProbeResult{}, ErrProbeFailed
	}
	result := s.probe.Probe(ctx, cfg)
	cfg.ProbePassed = result.Available
	cfg.UpdatedAt = s.now()
	details := map[string]string{"available": boolString(result.Available)}
	if result.ErrorReason != "" {
		details["error_reason"] = string(result.ErrorReason)
	}
	if err := s.store.SaveWithAudit(ctx, cfg, s.auditEntry(principal, id, AuditActionProbe, details)); err != nil {
		return ProbeResult{}, err
	}
	return result, nil
}

func (s *Service) probeForCurrent(ctx context.Context, cfg *ModelConfig) error {
	if s.probe == nil {
		if cfg.ProbePassed {
			return nil
		}
		return ErrProbeFailed
	}
	result := s.probe.Probe(ctx, *cfg)
	if !result.Available {
		return errors.Join(ErrProbeFailed, &llmProbeError{reason: result.ErrorReason})
	}
	cfg.ProbePassed = true
	return nil
}

func newSwitchResult(id string) SwitchResult {
	return SwitchResult{
		ConfigID:                       id,
		AppliesTo:                      SwitchAppliesToNewRequests,
		LiteLLMPropagationDelaySeconds: 30,
		Notice:                         "切换仅确认新请求将使用新配置；在途流不受影响，LiteLLM 多 worker 传播可能约 30 秒且缓存可能短暂陈旧。",
	}
}

func (s *Service) authorize(ctx context.Context, principal Principal) error {
	if s.auth == nil {
		return ErrUnauthorized
	}
	return s.auth.Authorize(ctx, principal, PermissionModelConfig)
}

func (s *Service) auditEntry(principal Principal, configID string, action AuditAction, details map[string]string) AuditEntry {
	return AuditEntry{
		ActorID:  principal.ID,
		ConfigID: configID,
		Action:   action,
		At:       s.now(),
		Details:  details,
	}
}

func (s *Service) syncModelConfig(ctx context.Context, cfg ModelConfig) error {
	if s.syncer == nil {
		return nil
	}
	return s.syncer.SyncModelConfig(ctx, cfg)
}

func publicConfig(cfg ModelConfig) PublicModelConfig {
	return PublicModelConfig{
		ID:           cfg.ID,
		Provider:     cfg.Provider,
		BaseURL:      cfg.BaseURL,
		APIKeyMasked: maskAPIKey(cfg.APIKey),
		Model:        cfg.Model,
		Network:      cfg.Network,
		Enabled:      cfg.Enabled,
		ProbePassed:  cfg.ProbePassed,
		IsCurrent:    cfg.IsCurrent,
		CreatedAt:    cfg.CreatedAt,
		UpdatedAt:    cfg.UpdatedAt,
	}
}

func maskAPIKey(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	if len(apiKey) <= 6 {
		return "******"
	}
	return apiKey[:3] + strings.Repeat("*", len(apiKey)-6) + apiKey[len(apiKey)-3:]
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

type llmProbeError struct {
	reason any
}

func (e *llmProbeError) Error() string {
	return "probe failed"
}
