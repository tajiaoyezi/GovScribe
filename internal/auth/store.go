package auth

import "context"

type UserReader interface {
	UserByUsername(context.Context, string) (User, error)
	UserByID(context.Context, string) (User, error)
	RolesForUser(context.Context, string) ([]RoleCode, error)
}

type UserWriter interface {
	CreateUser(context.Context, User, []RoleCode) error
	SetUserActive(context.Context, string, bool) error
	SetUserRoles(context.Context, string, []RoleCode) error
	UpdatePassword(context.Context, string, string, bool) error
}

type UserLister interface {
	ListUsers(context.Context) ([]User, error)
}

type RoleChecker interface {
	HasAnyUserWithRole(context.Context, RoleCode) (bool, error)
}

type AuditRecorder interface {
	AppendAudit(context.Context, AuditEntry) error
}

type AuditReader interface {
	ListAudits(context.Context) ([]AuditEntry, error)
}

type PermissionReader interface {
	PermissionDeclared(context.Context, Permission) (bool, error)
	RoleHasPermission(context.Context, RoleCode, Permission) (bool, error)
}

type UserStore interface {
	UserReader
	UserWriter
	UserLister
	RoleChecker
	AuditRecorder
	AuditReader
}

type RBACStore interface {
	PermissionReader
}
