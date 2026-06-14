package gateway

import (
	"context"
	"errors"
	"strings"
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

type Permission string

const PermissionRoutePolicyManage Permission = "route.policy.manage"

type Principal struct {
	ID string
}

type Authorizer interface {
	Authorize(context.Context, Principal, Permission) error
}

var ErrUnauthorizedRoutePolicy = errors.New("unauthorized route policy access")

type RoutePolicyService struct {
	store RouteConfigStore
	auth  Authorizer
	now   func() time.Time
}

func NewRoutePolicyService(store RouteConfigStore, auth Authorizer) *RoutePolicyService {
	if store == nil {
		store = NewMemoryRouteConfigStore()
	}
	return &RoutePolicyService{store: store, auth: auth, now: time.Now}
}

func (s *RoutePolicyService) GetPolicy(ctx context.Context, principal Principal, level llm.ContentSecurityLevel) (RoutePolicy, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return RoutePolicy{}, err
	}
	return s.store.GetPolicy(ctx, level)
}

func (s *RoutePolicyService) SavePolicy(ctx context.Context, principal Principal, policy RoutePolicy) (RoutePolicy, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return RoutePolicy{}, err
	}
	policy.UpdatedBy = principal.ID
	policy.UpdatedAt = s.now()
	policy = hardenPolicy(policy)
	if err := s.store.SavePolicy(ctx, policy); err != nil {
		return RoutePolicy{}, err
	}
	return policy, nil
}

func (s *RoutePolicyService) authorize(ctx context.Context, principal Principal) error {
	if s.auth == nil {
		return ErrUnauthorizedRoutePolicy
	}
	if err := s.auth.Authorize(ctx, principal, PermissionRoutePolicyManage); err != nil {
		return ErrUnauthorizedRoutePolicy
	}
	return nil
}

type MemoryRouteConfigStore struct {
	mu       sync.RWMutex
	policies map[llm.ContentSecurityLevel]RoutePolicy
	audits   []DispositionAuditEntry
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

func (s *MemoryRouteConfigStore) AppendDispositionAudit(_ context.Context, entry DispositionAuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, normalizeDispositionAuditEntry(entry))
	return nil
}

func (s *MemoryRouteConfigStore) Audits() []DispositionAuditEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DispositionAuditEntry, len(s.audits))
	copy(out, s.audits)
	return out
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
	if level == "" || strings.EqualFold(string(level), "unknown") {
		return llm.ContentSecurityLevelUnknown
	}
	return level
}

func routePolicyDBLevel(level llm.ContentSecurityLevel) string {
	if normalizeLevel(level) == llm.ContentSecurityLevelUnknown {
		return "unknown"
	}
	return string(level)
}
