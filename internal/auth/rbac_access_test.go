package auth

import (
	"context"
	"errors"
	"testing"
)

func TestRBACRoleBoundariesAndRoleChangeArePostgresBackedByStore(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	rbac := NewRBACService(store)
	auditor := Principal{UserID: "auditor-1", Roles: []RoleCode{RoleAuditor}, Authenticated: true}
	business := Principal{UserID: "business-1", Roles: []RoleCode{RoleBusinessUser}, Authenticated: true}
	secretary := Principal{UserID: "sec-1", Roles: []RoleCode{RoleSecretary}, Authenticated: true}
	admin := Principal{UserID: "admin-1", Roles: []RoleCode{RoleSystemAdmin}, Authenticated: true}

	assertAllowed(t, ctx, rbac, auditor, PermissionAuditRead, true)
	assertAllowed(t, ctx, rbac, auditor, PermissionDraftCreate, false)
	assertAllowed(t, ctx, rbac, business, PermissionTemplateSearch, true)
	assertAllowed(t, ctx, rbac, business, PermissionTemplateIngest, false)
	assertAllowed(t, ctx, rbac, secretary, PermissionAdoptDecide, true)
	assertAllowed(t, ctx, rbac, admin, PermissionUserManage, true)

	store.SetRolePermissions(RoleBusinessUser, []Permission{PermissionTemplateIngest})
	assertAllowed(t, ctx, rbac, business, PermissionTemplateSearch, false)
	assertAllowed(t, ctx, rbac, business, PermissionTemplateIngest, true)
}

func TestRBACRejectsUndeclaredPermission(t *testing.T) {
	store := NewMemoryStore()
	rbac := NewRBACService(store)
	allowed, err := rbac.Allowed(context.Background(),
		Principal{UserID: "admin-1", Roles: []RoleCode{RoleSystemAdmin}, Authenticated: true},
		Permission("unknown.permission"),
	)
	if err != nil {
		t.Fatalf("rbac error: %v", err)
	}
	if allowed {
		t.Fatal("undeclared permission must be rejected")
	}
}

func TestAccessDecisionFailClosedAndRequiresBothLayers(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	rbac := NewRBACService(store)
	principal := Principal{UserID: "admin-1", Roles: []RoleCode{RoleSystemAdmin}, Authenticated: true}
	service := NewAccessDecisionService(rbac, StaticDataACL{Allow: true})

	if got := service.Decide(ctx, Principal{}, PermissionUserManage, AccountSecurityContext()); got.Allowed() || got.Reason != DenyUnauthenticated {
		t.Fatalf("unauthenticated decision = %#v, want deny unauthenticated", got)
	}
	if got := service.Decide(ctx, principal, Permission("missing"), AccountSecurityContext()); got.Allowed() || got.Reason != DenyPermissionUndeclared {
		t.Fatalf("undeclared decision = %#v, want deny undeclared", got)
	}
	if got := service.Decide(ctx, principal, PermissionDraftCreate, AccountSecurityContext()); got.Allowed() || got.Reason != DenyRBAC {
		t.Fatalf("rbac denied decision = %#v, want deny rbac", got)
	}
	if got := service.Decide(ctx, principal, PermissionUserManage, DataSecurityContext{}); got.Allowed() || got.Reason != DenySecurityContext {
		t.Fatalf("incomplete security context decision = %#v, want deny security context", got)
	}
	service = NewAccessDecisionService(rbac, StaticDataACL{Allow: false})
	if got := service.Decide(ctx, principal, PermissionUserManage, AccountSecurityContext()); got.Allowed() || got.Reason != DenyDataACL {
		t.Fatalf("data ACL denied decision = %#v, want deny data ACL", got)
	}
	service = NewAccessDecisionService(rbac, StaticDataACL{Err: errors.New("db unavailable")})
	if got := service.Decide(ctx, principal, PermissionUserManage, AccountSecurityContext()); got.Allowed() || got.Reason != DenyDataACLUnavailable {
		t.Fatalf("data ACL unavailable decision = %#v, want deny unavailable", got)
	}
	service = NewAccessDecisionService(rbac, StaticDataACL{Allow: true})
	if got := service.Decide(ctx, principal, PermissionUserManage, AccountSecurityContext()); !got.Allowed() {
		t.Fatalf("allow decision = %#v, want allow", got)
	}
}

func TestAccessDecisionAuditsAuthorizationDenied(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewAccessDecisionServiceWithAudit(NewRBACService(store), StaticDataACL{Allow: true}, store)
	business := Principal{UserID: "business-1", Roles: []RoleCode{RoleBusinessUser}, Authenticated: true}
	result := service.Decide(ctx, business, PermissionUserManage, AccountSecurityManageContext())
	if result.Allowed() {
		t.Fatal("business user must not be allowed to manage users")
	}
	audits := store.Audits()
	if len(audits) != 1 || audits[0].EventType != AuditEventAuthorizationDenied {
		t.Fatalf("audits = %#v, want one authorization_denied event", audits)
	}
	if audits[0].Details["permission"] != string(PermissionUserManage) || audits[0].Details["reason"] != string(DenyRBAC) {
		t.Fatalf("audit details = %#v, want permission and deny reason", audits[0].Details)
	}
}

func TestAccountSecurityReadAndManageContextsAreDistinct(t *testing.T) {
	if AccountSecurityReadContext().Action != "read" {
		t.Fatalf("read context action = %q, want read", AccountSecurityReadContext().Action)
	}
	if AccountSecurityManageContext().Action != "manage" {
		t.Fatalf("manage context action = %q, want manage", AccountSecurityManageContext().Action)
	}
}

func assertAllowed(t *testing.T, ctx context.Context, rbac *RBACService, principal Principal, permission Permission, want bool) {
	t.Helper()
	got, err := rbac.Allowed(ctx, principal, permission)
	if err != nil {
		t.Fatalf("rbac allowed(%q): %v", permission, err)
	}
	if got != want {
		t.Fatalf("rbac allowed(%q) = %v, want %v", permission, got, want)
	}
}
