package auth

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu          sync.Mutex
	users       map[string]User
	byUsername  map[string]string
	userRoles   map[string][]RoleCode
	permissions map[Permission]struct{}
	rolePerms   map[RoleCode]map[Permission]struct{}
	audits      []AuditEntry
}

func NewMemoryStore() *MemoryStore {
	store := &MemoryStore{
		users:       map[string]User{},
		byUsername:  map[string]string{},
		userRoles:   map[string][]RoleCode{},
		permissions: map[Permission]struct{}{},
		rolePerms:   map[RoleCode]map[Permission]struct{}{},
	}
	for _, permission := range DeclaredPermissions() {
		store.permissions[permission] = struct{}{}
	}
	for role, permissions := range DefaultRolePermissions() {
		if store.rolePerms[role] == nil {
			store.rolePerms[role] = map[Permission]struct{}{}
		}
		for _, permission := range permissions {
			store.rolePerms[role][permission] = struct{}{}
		}
	}
	return store
}

func (s *MemoryStore) CreateUser(_ context.Context, user User, roles []RoleCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if user.ID == "" || user.Username == "" {
		return ErrUserNotFound
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now()
	}
	s.users[user.ID] = user
	s.byUsername[loginKey(user.Username)] = user.ID
	s.userRoles[user.ID] = append([]RoleCode(nil), roles...)
	return nil
}

func (s *MemoryStore) UserByUsername(_ context.Context, username string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.byUsername[loginKey(username)]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return s.userByIDLocked(id)
}

func (s *MemoryStore) UserByID(_ context.Context, id string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userByIDLocked(id)
}

func (s *MemoryStore) userByIDLocked(id string) (User, error) {
	user, ok := s.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return user, nil
}

func (s *MemoryStore) RolesForUser(_ context.Context, id string) ([]RoleCode, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return nil, ErrUserNotFound
	}
	return append([]RoleCode(nil), s.userRoles[id]...), nil
}

func (s *MemoryStore) ListUsers(context.Context) ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]User, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, user)
	}
	return users, nil
}

func (s *MemoryStore) HasAnyUserWithRole(_ context.Context, role RoleCode) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, roles := range s.userRoles {
		for _, current := range roles {
			if current == role {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *MemoryStore) SetUserActive(_ context.Context, id string, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return ErrUserNotFound
	}
	user.IsActive = active
	if !active {
		now := time.Now()
		user.DisabledAt = &now
	} else {
		user.DisabledAt = nil
	}
	s.users[id] = user
	return nil
}

func (s *MemoryStore) SetUserRoles(_ context.Context, id string, roles []RoleCode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return ErrUserNotFound
	}
	s.userRoles[id] = append([]RoleCode(nil), roles...)
	return nil
}

func (s *MemoryStore) UpdatePassword(_ context.Context, id, hash string, mustChange bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.users[id]
	if !ok {
		return ErrUserNotFound
	}
	user.PasswordHash = hash
	user.PasswordAlgorithm = PasswordHashAlgorithmBcrypt
	user.MustChangePassword = mustChange
	s.users[id] = user
	return nil
}

func (s *MemoryStore) AppendAudit(_ context.Context, entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append(s.audits, entry)
	return nil
}

func (s *MemoryStore) Audits() []AuditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AuditEntry(nil), s.audits...)
}

func (s *MemoryStore) ListAudits(context.Context) ([]AuditEntry, error) {
	return s.Audits(), nil
}

func (s *MemoryStore) PermissionDeclared(_ context.Context, permission Permission) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.permissions[permission]
	return ok, nil
}

func (s *MemoryStore) RoleHasPermission(_ context.Context, role RoleCode, permission Permission) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	perms, ok := s.rolePerms[role]
	if !ok {
		return false, nil
	}
	_, ok = perms[permission]
	return ok, nil
}

func (s *MemoryStore) SetRolePermissions(role RoleCode, permissions []Permission) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rolePerms[role] = map[Permission]struct{}{}
	for _, permission := range permissions {
		s.rolePerms[role][permission] = struct{}{}
	}
}
