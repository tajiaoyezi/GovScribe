package config

import (
	"context"
	"sync"
)

type Store interface {
	Save(context.Context, ModelConfig) error
	Get(context.Context, string) (ModelConfig, error)
	List(context.Context) ([]ModelConfig, error)
	Current(context.Context) (ModelConfig, error)
	SetCurrent(context.Context, string) error
	AppendAudit(context.Context, AuditEntry) error
	SaveWithAudit(context.Context, ModelConfig, AuditEntry) error
	SaveAndSetCurrentWithAudit(context.Context, ModelConfig, AuditEntry) error
}

type MemoryStore struct {
	mu      sync.RWMutex
	configs map[string]ModelConfig
	audits  []AuditEntry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{configs: make(map[string]ModelConfig)}
}

func (s *MemoryStore) Save(_ context.Context, cfg ModelConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[cfg.ID] = cfg
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (ModelConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.configs[id]
	if !ok {
		return ModelConfig{}, ErrConfigNotFound
	}
	return cfg, nil
}

func (s *MemoryStore) List(_ context.Context) ([]ModelConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ModelConfig, 0, len(s.configs))
	for _, cfg := range s.configs {
		out = append(out, cfg)
	}
	return out, nil
}

func (s *MemoryStore) Current(_ context.Context) (ModelConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, cfg := range s.configs {
		if cfg.IsCurrent {
			return cfg, nil
		}
	}
	return ModelConfig{}, ErrConfigNotFound
}

func (s *MemoryStore) SetCurrent(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.configs[id]; !ok {
		return ErrConfigNotFound
	}
	for cfgID, cfg := range s.configs {
		cfg.IsCurrent = cfgID == id
		s.configs[cfgID] = cfg
	}
	return nil
}

func (s *MemoryStore) AppendAudit(_ context.Context, entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, entry)
	return nil
}

func (s *MemoryStore) SaveWithAudit(_ context.Context, cfg ModelConfig, entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs[cfg.ID] = cfg
	s.audits = append(s.audits, entry)
	return nil
}

func (s *MemoryStore) SaveAndSetCurrentWithAudit(_ context.Context, cfg ModelConfig, entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.configs[cfg.ID]; !ok {
		return ErrConfigNotFound
	}
	s.configs[cfg.ID] = cfg
	for cfgID, existing := range s.configs {
		existing.IsCurrent = cfgID == cfg.ID
		s.configs[cfgID] = existing
	}
	s.audits = append(s.audits, entry)
	return nil
}

func (s *MemoryStore) Audits() []AuditEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]AuditEntry, len(s.audits))
	copy(out, s.audits)
	return out
}
