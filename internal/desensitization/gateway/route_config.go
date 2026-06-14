package gateway

import (
	"context"
	"sync"
	"time"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

type RoutePolicy struct {
	Level               llm.ContentSecurityLevel
	TargetNetwork       llm.Network
	ModelConfigID       string
	AllowDegradedPublic bool
	UpdatedBy           string
	UpdatedAt           time.Time
}

type RouteConfigStore interface {
	GetPolicy(context.Context, llm.ContentSecurityLevel) (RoutePolicy, error)
	SavePolicy(context.Context, RoutePolicy) error
}

type MemoryRouteConfigStore struct {
	mu       sync.RWMutex
	policies map[llm.ContentSecurityLevel]RoutePolicy
}

func NewMemoryRouteConfigStore() *MemoryRouteConfigStore {
	store := &MemoryRouteConfigStore{policies: make(map[llm.ContentSecurityLevel]RoutePolicy)}
	for _, policy := range defaultRoutePolicies() {
		store.policies[policy.Level] = policy
	}
	return store
}

func (s *MemoryRouteConfigStore) GetPolicy(_ context.Context, level llm.ContentSecurityLevel) (RoutePolicy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	policy, ok := s.policies[normalizeLevel(level)]
	if !ok {
		policy = s.policies[llm.ContentSecurityLevelUnknown]
	}
	return hardenPolicy(policy), nil
}

func (s *MemoryRouteConfigStore) SavePolicy(_ context.Context, policy RoutePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy = hardenPolicy(policy)
	s.policies[policy.Level] = policy
	return nil
}

func defaultRoutePolicies() []RoutePolicy {
	return []RoutePolicy{
		{Level: llm.ContentSecurityLevelUnknown, TargetNetwork: llm.NetworkPrivate},
		{Level: llm.ContentSecurityLevelUnclassified, TargetNetwork: llm.NetworkPublic},
		{Level: llm.ContentSecurityLevelSensitive, TargetNetwork: llm.NetworkPublic},
		{Level: llm.ContentSecurityLevelClassified, TargetNetwork: llm.NetworkPrivate},
	}
}

func normalizeLevel(level llm.ContentSecurityLevel) llm.ContentSecurityLevel {
	if level == "" {
		return llm.ContentSecurityLevelUnknown
	}
	return level
}
