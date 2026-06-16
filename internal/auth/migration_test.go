package auth

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationDefinesAuthRBACAuthorityTablesAndSeeds(t *testing.T) {
	content, err := os.ReadFile("../../backend/migrations/000004_auth_rbac.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(content)
	required := []string{
		"CREATE TABLE IF NOT EXISTS users",
		"username TEXT NOT NULL UNIQUE",
		"password_hash TEXT NOT NULL",
		"password_hash_algorithm TEXT NOT NULL DEFAULT 'bcrypt'",
		"is_active BOOLEAN NOT NULL DEFAULT TRUE",
		"must_change_password BOOLEAN NOT NULL DEFAULT TRUE",
		"CREATE TABLE IF NOT EXISTS roles",
		"CREATE TABLE IF NOT EXISTS permissions",
		"CREATE TABLE IF NOT EXISTS role_permissions",
		"ON DELETE CASCADE",
		"CREATE TABLE IF NOT EXISTS user_roles",
		"CREATE TABLE IF NOT EXISTS account_security_audit_logs",
		"account_security_audit_no_password_chk",
		"CREATE TABLE IF NOT EXISTS data_security_acl_grants",
		"CREATE TABLE IF NOT EXISTS data_security_role_acl_grants",
		"DELETE FROM role_permissions",
		"('system_admin', 'account_security', 'users', 'internal', 'manage', TRUE)",
		"('system_admin', 'account_security', 'users', 'internal', 'read', TRUE)",
		"('auditor', 'account_security', 'users', 'internal', 'read', TRUE)",
		"('system_admin', 'System Administrator')",
		"('secretary', 'Secretary')",
		"('business_user', 'Business User')",
		"('auditor', 'Auditor')",
		"('document.open', 'Open document')",
		"('document.edit', 'Edit document')",
		"('document.export', 'Export document')",
		"('review.online', 'Online review')",
		"('adopt.decide', 'Adoption decision')",
		"('audit.read', 'Read audit logs')",
		"('dict.manage', 'Manage desensitization dictionary')",
		"('model.config', 'Manage model provider configuration')",
		"('template.search', 'Search corpus templates')",
		"('template.ingest', 'Ingest corpus templates')",
		"('draft.create', 'Create draft')",
		"('user.manage', 'Manage users and roles')",
	}
	for _, want := range required {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	if strings.Contains(sql, "password TEXT") || strings.Contains(sql, "plain_password") {
		t.Fatal("migration must not define plaintext password columns")
	}
}

func TestDefaultRolePermissionMappingMatchesD10Boundaries(t *testing.T) {
	mapping := DefaultRolePermissions()
	wantAuditor := []Permission{PermissionAuditRead}
	if !samePermissions(mapping[RoleAuditor], wantAuditor) {
		t.Fatalf("auditor permissions = %#v, want only audit.read", mapping[RoleAuditor])
	}
	for _, forbidden := range []Permission{
		PermissionModelConfig,
		PermissionDictManage,
		PermissionUserManage,
		PermissionAuditRead,
		PermissionReviewOnline,
		PermissionAdoptDecide,
		PermissionTemplateIngest,
	} {
		if containsPermission(mapping[RoleBusinessUser], forbidden) {
			t.Fatalf("business_user must not include %q", forbidden)
		}
	}
	if len(mapping) != 4 {
		t.Fatalf("role count = %d, want 4", len(mapping))
	}
}

func samePermissions(got, want []Permission) bool {
	if len(got) != len(want) {
		return false
	}
	for _, permission := range want {
		if !containsPermission(got, permission) {
			return false
		}
	}
	return true
}

func containsPermission(permissions []Permission, want Permission) bool {
	for _, permission := range permissions {
		if permission == want {
			return true
		}
	}
	return false
}
