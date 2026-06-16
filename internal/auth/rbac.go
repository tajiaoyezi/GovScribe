package auth

import "context"

type RBACService struct {
	store RBACStore
}

func NewRBACService(store RBACStore) *RBACService {
	return &RBACService{store: store}
}

func (s *RBACService) Allowed(ctx context.Context, principal Principal, permission Permission) (bool, error) {
	if s == nil || s.store == nil || !principal.Authenticated {
		return false, nil
	}
	declared, err := s.store.PermissionDeclared(ctx, permission)
	if err != nil || !declared {
		return false, err
	}
	for _, role := range principal.Roles {
		allowed, err := s.store.RoleHasPermission(ctx, role, permission)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
}

type Authorizer struct {
	rbac *RBACService
}

func NewAuthorizer(rbac *RBACService) *Authorizer {
	return &Authorizer{rbac: rbac}
}

func (a *Authorizer) Authorize(ctx context.Context, principal Principal, permission Permission) error {
	allowed, err := a.rbac.Allowed(ctx, principal, permission)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrUnauthorized
	}
	return nil
}
