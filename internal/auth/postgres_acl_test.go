package auth

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresDataACLChecksUserAndRoleGrants(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	defer db.Close()
	acl := NewPostgresDataACL(db)
	mock.ExpectQuery(regexp.QuoteMeta(`
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
)`)).
		WithArgs("admin-1", "account_security", "users", "internal", "manage").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	allowed, err := acl.Allowed(context.Background(),
		Principal{UserID: "admin-1", Authenticated: true},
		AccountSecurityManageContext(),
	)
	if err != nil {
		t.Fatalf("acl allowed: %v", err)
	}
	if !allowed {
		t.Fatal("expected role-backed account security ACL to allow")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
