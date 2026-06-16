package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

type DenyReason string

const (
	DenyUnauthenticated      DenyReason = "unauthenticated"
	DenyPermissionUndeclared DenyReason = "permission_undeclared"
	DenyRBAC                 DenyReason = "rbac_denied"
	DenySecurityContext      DenyReason = "security_context_incomplete"
	DenyDataACL              DenyReason = "data_acl_denied"
	DenyDataACLUnavailable   DenyReason = "data_acl_unavailable"
)

type DataSecurityContext struct {
	ResourceType   string
	ResourceID     string
	Classification string
	Department     string
	Action         string
	Complete       bool
}

type DecisionResult struct {
	Decision Decision
	Reason   DenyReason
}

func (r DecisionResult) Allowed() bool {
	return r.Decision == DecisionAllow
}

type DataACL interface {
	Allowed(context.Context, Principal, DataSecurityContext) (bool, error)
}

type AccessDecisionService struct {
	rbac    *RBACService
	dataACL DataACL
	audit   AuditRecorder
	now     func() time.Time
}

func NewAccessDecisionService(rbac *RBACService, dataACL DataACL) *AccessDecisionService {
	return &AccessDecisionService{rbac: rbac, dataACL: dataACL, now: time.Now}
}

func NewAccessDecisionServiceWithAudit(rbac *RBACService, dataACL DataACL, audit AuditRecorder) *AccessDecisionService {
	return &AccessDecisionService{rbac: rbac, dataACL: dataACL, audit: audit, now: time.Now}
}

func (s *AccessDecisionService) Decide(ctx context.Context, principal Principal, permission Permission, security DataSecurityContext) DecisionResult {
	if !principal.Authenticated || principal.UserID == "" {
		return s.deny(ctx, principal, permission, security, DenyUnauthenticated)
	}
	if s == nil || s.rbac == nil || s.rbac.store == nil || s.dataACL == nil {
		return s.deny(ctx, principal, permission, security, DenyDataACLUnavailable)
	}
	declared, err := s.rbac.store.PermissionDeclared(ctx, permission)
	if err != nil || !declared {
		return s.deny(ctx, principal, permission, security, DenyPermissionUndeclared)
	}
	rbacAllowed, err := s.rbac.Allowed(ctx, principal, permission)
	if err != nil || !rbacAllowed {
		return s.deny(ctx, principal, permission, security, DenyRBAC)
	}
	if !security.Complete {
		return s.deny(ctx, principal, permission, security, DenySecurityContext)
	}
	aclAllowed, err := s.dataACL.Allowed(ctx, principal, security)
	if err != nil {
		return s.deny(ctx, principal, permission, security, DenyDataACLUnavailable)
	}
	if !aclAllowed {
		return s.deny(ctx, principal, permission, security, DenyDataACL)
	}
	return DecisionResult{Decision: DecisionAllow}
}

func (s *AccessDecisionService) deny(ctx context.Context, principal Principal, permission Permission, security DataSecurityContext, reason DenyReason) DecisionResult {
	result := deny(reason)
	if s == nil || s.audit == nil || !principal.Authenticated || principal.UserID == "" {
		return result
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	_ = s.audit.AppendAudit(ctx, AuditEntry{
		ActorID:      principal.UserID,
		TargetUserID: principal.UserID,
		EventType:    AuditEventAuthorizationDenied,
		Details: map[string]string{
			"permission":    string(permission),
			"reason":        string(reason),
			"resource_type": security.ResourceType,
			"resource_id":   security.ResourceID,
			"action":        security.Action,
		},
		At: now,
	})
	return result
}

func deny(reason DenyReason) DecisionResult {
	return DecisionResult{Decision: DecisionDeny, Reason: reason}
}

type StaticDataACL struct {
	Allow bool
	Err   error
}

func (a StaticDataACL) Allowed(context.Context, Principal, DataSecurityContext) (bool, error) {
	if a.Err != nil {
		return false, a.Err
	}
	return a.Allow, nil
}

type PostgresDataACL struct {
	db *sql.DB
}

func NewPostgresDataACL(db *sql.DB) *PostgresDataACL {
	return &PostgresDataACL{db: db}
}

func (a *PostgresDataACL) Allowed(ctx context.Context, principal Principal, security DataSecurityContext) (bool, error) {
	if a == nil || a.db == nil || !security.Complete {
		return false, errors.New("data acl unavailable")
	}
	var ok bool
	err := a.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM data_security_acl_grants
	WHERE user_id = $1
	  AND resource_type = $2
	  AND resource_id = $3
	  AND classification = $4
	  AND action = $5
	  AND allow = TRUE
) OR EXISTS (
	SELECT 1
	FROM user_roles ur
	JOIN data_security_role_acl_grants rg ON rg.role_code = ur.role_code
	WHERE ur.user_id = $1
	  AND rg.resource_type = $2
	  AND rg.resource_id = $3
	  AND rg.classification = $4
	  AND rg.action = $5
	  AND rg.allow = TRUE
)`,
		principal.UserID,
		security.ResourceType,
		security.ResourceID,
		security.Classification,
		security.Action,
	).Scan(&ok)
	return ok, err
}

func AccountSecurityManageContext() DataSecurityContext {
	return DataSecurityContext{
		ResourceType:   "account_security",
		ResourceID:     "users",
		Classification: "internal",
		Action:         "manage",
		Complete:       true,
	}
}

func AccountSecurityReadContext() DataSecurityContext {
	return DataSecurityContext{
		ResourceType:   "account_security",
		ResourceID:     "users",
		Classification: "internal",
		Action:         "read",
		Complete:       true,
	}
}

func AccountSecurityContext() DataSecurityContext {
	return AccountSecurityManageContext()
}
