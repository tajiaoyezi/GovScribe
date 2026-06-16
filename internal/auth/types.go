package auth

import (
	"errors"
	"time"
)

type RoleCode string

const (
	RoleSystemAdmin  RoleCode = "system_admin"
	RoleSecretary    RoleCode = "secretary"
	RoleBusinessUser RoleCode = "business_user"
	RoleAuditor      RoleCode = "auditor"
)

type Permission string

const (
	PermissionDocumentOpen   Permission = "document.open"
	PermissionDocumentEdit   Permission = "document.edit"
	PermissionDocumentExport Permission = "document.export"
	PermissionReviewOnline   Permission = "review.online"
	PermissionAdoptDecide    Permission = "adopt.decide"
	PermissionAuditRead      Permission = "audit.read"
	PermissionDictManage     Permission = "dict.manage"
	PermissionModelConfig    Permission = "model.config"
	PermissionTemplateSearch Permission = "template.search"
	PermissionTemplateIngest Permission = "template.ingest"
	PermissionDraftCreate    Permission = "draft.create"
	PermissionUserManage     Permission = "user.manage"
)

var ErrInvalidCredentials = errors.New("invalid username or password")
var ErrLoginTemporarilyBlocked = errors.New("login temporarily blocked")
var ErrUnauthenticated = errors.New("unauthenticated")
var ErrUnauthorized = errors.New("unauthorized")
var ErrUserNotFound = errors.New("user not found")
var ErrRoleNotFound = errors.New("role not found")
var ErrPermissionNotDeclared = errors.New("permission not declared")
var ErrMustChangePassword = errors.New("password change required")
var ErrPasswordPolicyViolation = errors.New("password does not satisfy policy")

type Principal struct {
	UserID             string
	Username           string
	Department         string
	Roles              []RoleCode
	MustChangePassword bool
	Provider           string
	Authenticated      bool
}

func (p Principal) HasRole(role RoleCode) bool {
	for _, current := range p.Roles {
		if current == role {
			return true
		}
	}
	return false
}

func (p Principal) RoleStrings() []string {
	roles := make([]string, 0, len(p.Roles))
	for _, role := range p.Roles {
		roles = append(roles, string(role))
	}
	return roles
}

type User struct {
	ID                 string
	Username           string
	PasswordHash       string
	PasswordAlgorithm  string
	IsActive           bool
	Department         string
	MustChangePassword bool
	CreatedAt          time.Time
	DisabledAt         *time.Time
}

type Role struct {
	Code        RoleCode
	DisplayName string
}

type AuditEventType string

const (
	AuditEventUserCreated         AuditEventType = "user_created"
	AuditEventUserDisabled        AuditEventType = "user_disabled"
	AuditEventRoleAssigned        AuditEventType = "role_assigned"
	AuditEventPasswordReset       AuditEventType = "password_reset"
	AuditEventPasswordChanged     AuditEventType = "password_changed"
	AuditEventLoginFailed         AuditEventType = "login_failed"
	AuditEventLoginBlocked        AuditEventType = "login_blocked"
	AuditEventAuthorizationDenied AuditEventType = "authorization_denied"
)

type AuditEntry struct {
	ActorID      string
	TargetUserID string
	EventType    AuditEventType
	Details      map[string]string
	At           time.Time
}

type PasswordPolicy struct {
	MinLength int
}

func (p PasswordPolicy) Validate(password string) error {
	min := p.MinLength
	if min == 0 {
		min = 10
	}
	if len(password) < min {
		return ErrPasswordPolicyViolation
	}
	return nil
}

func DeclaredPermissions() []Permission {
	return []Permission{
		PermissionDocumentOpen,
		PermissionDocumentEdit,
		PermissionDocumentExport,
		PermissionReviewOnline,
		PermissionAdoptDecide,
		PermissionAuditRead,
		PermissionDictManage,
		PermissionModelConfig,
		PermissionTemplateSearch,
		PermissionTemplateIngest,
		PermissionDraftCreate,
		PermissionUserManage,
	}
}

func IsDeclaredPermission(permission Permission) bool {
	for _, declared := range DeclaredPermissions() {
		if declared == permission {
			return true
		}
	}
	return false
}

func DefaultRolePermissions() map[RoleCode][]Permission {
	return map[RoleCode][]Permission{
		RoleSystemAdmin: {
			PermissionModelConfig,
			PermissionDictManage,
			PermissionAuditRead,
			PermissionUserManage,
		},
		RoleSecretary: {
			PermissionDocumentOpen,
			PermissionDocumentEdit,
			PermissionDocumentExport,
			PermissionReviewOnline,
			PermissionAdoptDecide,
			PermissionTemplateSearch,
			PermissionTemplateIngest,
			PermissionDraftCreate,
		},
		RoleBusinessUser: {
			PermissionDocumentOpen,
			PermissionDocumentEdit,
			PermissionDocumentExport,
			PermissionDraftCreate,
			PermissionTemplateSearch,
		},
		RoleAuditor: {
			PermissionAuditRead,
		},
	}
}
