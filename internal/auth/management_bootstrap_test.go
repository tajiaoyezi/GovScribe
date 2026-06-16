package auth

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestUserManagementRequiresAdminDecisionAndAuditsWithoutPassword(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	decider := NewAccessDecisionService(NewRBACService(store), StaticDataACL{Allow: true})
	svc := NewUserManagementService(store, hasher, decider)
	svc.newID = func() string { return "user-new" }

	admin := Principal{UserID: "admin-1", Roles: []RoleCode{RoleSystemAdmin}, Authenticated: true}
	user, err := svc.CreateUser(ctx, admin, CreateUserRequest{
		Username:   "newuser",
		Password:   "initial-secret",
		Department: "office",
		Role:       RoleBusinessUser,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if user.PasswordHash == "initial-secret" || !hasher.Verify(user.PasswordHash, "initial-secret") || !user.MustChangePassword {
		t.Fatalf("created user password state = %#v, want salted hash and forced change", user)
	}
	if err := svc.AssignRole(ctx, admin, user.ID, RoleSecretary); err != nil {
		t.Fatalf("assign role: %v", err)
	}
	roles, err := store.RolesForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("roles: %v", err)
	}
	if len(roles) != 1 || roles[0] != RoleSecretary {
		t.Fatalf("roles = %#v, want secretary", roles)
	}
	if err := svc.ResetPassword(ctx, admin, user.ID, "reset-secret"); err != nil {
		t.Fatalf("reset password: %v", err)
	}
	resetUser, err := store.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("user after reset: %v", err)
	}
	if resetUser.PasswordHash == "reset-secret" || !resetUser.MustChangePassword {
		t.Fatalf("reset user = %#v, want hash and forced change", resetUser)
	}
	if err := svc.DisableUser(ctx, admin, user.ID); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	disabled, err := store.UserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("disabled user: %v", err)
	}
	if disabled.IsActive {
		t.Fatal("disabled user remained active")
	}
	for _, entry := range store.Audits() {
		for key, value := range entry.Details {
			if key == "password" || key == "password_hash" || key == "new_password" || value == "initial-secret" || value == "reset-secret" {
				t.Fatalf("audit leaks password material: %#v", entry)
			}
		}
	}
}

func TestUserManagementRejectsNonAdmin(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := NewUserManagementService(store, BcryptHasher{Cost: bcrypt.MinCost}, NewAccessDecisionService(NewRBACService(store), StaticDataACL{Allow: true}))
	_, err := svc.CreateUser(ctx,
		Principal{UserID: "sec-1", Roles: []RoleCode{RoleSecretary}, Authenticated: true},
		CreateUserRequest{Username: "blocked", Password: "secret", Role: RoleBusinessUser},
	)
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("non-admin create error = %v, want ErrUnauthorized", err)
	}
}

func TestAuditReadIsConstrainedByAuditReadPermission(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if err := store.AppendAudit(ctx, AuditEntry{ActorID: "admin-1", EventType: AuditEventUserCreated}); err != nil {
		t.Fatalf("seed audit: %v", err)
	}
	svc := NewAuditLogService(store, NewAccessDecisionService(NewRBACService(store), StaticDataACL{Allow: true}))
	if _, err := svc.ListAccountSecurityAudits(ctx, Principal{UserID: "business-1", Roles: []RoleCode{RoleBusinessUser}, Authenticated: true}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("business audit read error = %v, want ErrUnauthorized", err)
	}
	entries, err := svc.ListAccountSecurityAudits(ctx, Principal{UserID: "auditor-1", Roles: []RoleCode{RoleAuditor}, Authenticated: true})
	if err != nil {
		t.Fatalf("auditor read: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1", len(entries))
	}
}

func TestChangeOwnPasswordClearsMustChangePassword(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	hash, err := hasher.Hash("old")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := store.CreateUser(ctx, User{
		ID:                 "user-1",
		Username:           "alice",
		PasswordHash:       hash,
		PasswordAlgorithm:  hasher.Algorithm(),
		IsActive:           true,
		MustChangePassword: true,
	}, []RoleCode{RoleBusinessUser}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	svc := NewUserManagementService(store, hasher, NewAccessDecisionService(NewRBACService(store), StaticDataACL{Allow: true}))
	if err := svc.ChangeOwnPassword(ctx, Principal{UserID: "user-1", Authenticated: true}, "new-secret-1"); err != nil {
		t.Fatalf("change own password: %v", err)
	}
	user, err := store.UserByID(ctx, "user-1")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	if user.MustChangePassword || !hasher.Verify(user.PasswordHash, "new-secret-1") {
		t.Fatalf("user password state = %#v, want changed and must_change=false", user)
	}
}

func TestUserManagementRejectsShortPasswords(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := NewUserManagementService(store, BcryptHasher{Cost: bcrypt.MinCost}, NewAccessDecisionService(NewRBACService(store), StaticDataACL{Allow: true}))
	_, err := svc.CreateUser(ctx,
		Principal{UserID: "admin-1", Roles: []RoleCode{RoleSystemAdmin}, Authenticated: true},
		CreateUserRequest{Username: "short", Password: "short", Role: RoleBusinessUser},
	)
	if !errors.Is(err, ErrPasswordPolicyViolation) {
		t.Fatalf("short create password error = %v, want ErrPasswordPolicyViolation", err)
	}
}

func TestBootstrapInitialAdminCreatesOnceAndDoesNotExposePassword(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	created, err := BootstrapInitialAdmin(ctx, store, hasher, BootstrapConfig{
		Username:   "admin",
		Password:   "bootstrap-secret",
		Department: "office",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !created {
		t.Fatal("first bootstrap did not create admin")
	}
	created, err = BootstrapInitialAdmin(ctx, store, hasher, BootstrapConfig{
		Username: "admin",
		Password: "different-secret",
	})
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if created {
		t.Fatal("second bootstrap must be idempotent")
	}
	admin, err := store.UserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	if admin.PasswordHash == "bootstrap-secret" || !admin.MustChangePassword {
		t.Fatalf("admin = %#v, want hashed initial password and forced change", admin)
	}
	roles, err := store.RolesForUser(ctx, admin.ID)
	if err != nil {
		t.Fatalf("admin roles: %v", err)
	}
	if len(roles) != 1 || roles[0] != RoleSystemAdmin {
		t.Fatalf("admin roles = %#v, want system_admin", roles)
	}
	for _, entry := range store.Audits() {
		for _, value := range entry.Details {
			if value == "bootstrap-secret" {
				t.Fatalf("bootstrap audit leaks password: %#v", entry)
			}
		}
	}
}

func TestBootstrapInitialAdminDoesNotCreateSecondAdminWithDifferentUsername(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	if created, err := BootstrapInitialAdmin(ctx, store, hasher, BootstrapConfig{Username: "admin", Password: "bootstrap-secret"}); err != nil || !created {
		t.Fatalf("first bootstrap created=%v err=%v, want created", created, err)
	}
	created, err := BootstrapInitialAdmin(ctx, store, hasher, BootstrapConfig{Username: "other-admin", Password: "bootstrap-secret"})
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if created {
		t.Fatal("bootstrap must not create a second system_admin with a different username")
	}
}
