package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

type UserManagementService struct {
	store   UserStore
	hasher  PasswordHasher
	decider *AccessDecisionService
	policy  PasswordPolicy
	now     func() time.Time
	newID   func() string
}

func NewUserManagementService(store UserStore, hasher PasswordHasher, decider *AccessDecisionService) *UserManagementService {
	return &UserManagementService{
		store:   store,
		hasher:  hasher,
		decider: decider,
		policy:  PasswordPolicy{},
		now:     time.Now,
		newID:   newID,
	}
}

type CreateUserRequest struct {
	Username   string
	Password   string
	Department string
	Role       RoleCode
}

func (s *UserManagementService) CreateUser(ctx context.Context, actor Principal, req CreateUserRequest) (User, error) {
	if err := s.requireUserManage(ctx, actor); err != nil {
		return User{}, err
	}
	if err := s.policy.Validate(req.Password); err != nil {
		return User{}, err
	}
	hash, err := s.hasher.Hash(req.Password)
	if err != nil {
		return User{}, err
	}
	now := s.nowTime()
	user := User{
		ID:                 s.newID(),
		Username:           req.Username,
		PasswordHash:       hash,
		PasswordAlgorithm:  s.hasher.Algorithm(),
		IsActive:           true,
		Department:         req.Department,
		MustChangePassword: true,
		CreatedAt:          now,
	}
	if err := s.store.CreateUser(ctx, user, []RoleCode{req.Role}); err != nil {
		return User{}, err
	}
	if err := s.store.AppendAudit(ctx, AuditEntry{
		ActorID:      actor.UserID,
		TargetUserID: user.ID,
		EventType:    AuditEventUserCreated,
		Details:      map[string]string{"username": req.Username, "role": string(req.Role)},
		At:           now,
	}); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *UserManagementService) DisableUser(ctx context.Context, actor Principal, userID string) error {
	if err := s.requireUserManage(ctx, actor); err != nil {
		return err
	}
	if err := s.store.SetUserActive(ctx, userID, false); err != nil {
		return err
	}
	return s.store.AppendAudit(ctx, AuditEntry{
		ActorID:      actor.UserID,
		TargetUserID: userID,
		EventType:    AuditEventUserDisabled,
		At:           s.nowTime(),
	})
}

func (s *UserManagementService) AssignRole(ctx context.Context, actor Principal, userID string, role RoleCode) error {
	if err := s.requireUserManage(ctx, actor); err != nil {
		return err
	}
	if err := s.store.SetUserRoles(ctx, userID, []RoleCode{role}); err != nil {
		return err
	}
	return s.store.AppendAudit(ctx, AuditEntry{
		ActorID:      actor.UserID,
		TargetUserID: userID,
		EventType:    AuditEventRoleAssigned,
		Details:      map[string]string{"role": string(role)},
		At:           s.nowTime(),
	})
}

func (s *UserManagementService) ResetPassword(ctx context.Context, actor Principal, userID, newPassword string) error {
	if err := s.requireUserManage(ctx, actor); err != nil {
		return err
	}
	if err := s.policy.Validate(newPassword); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePassword(ctx, userID, hash, true); err != nil {
		return err
	}
	return s.store.AppendAudit(ctx, AuditEntry{
		ActorID:      actor.UserID,
		TargetUserID: userID,
		EventType:    AuditEventPasswordReset,
		At:           s.nowTime(),
	})
}

func (s *UserManagementService) ChangeOwnPassword(ctx context.Context, principal Principal, newPassword string) error {
	if !principal.Authenticated || principal.UserID == "" {
		return ErrUnauthenticated
	}
	if err := s.policy.Validate(newPassword); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePassword(ctx, principal.UserID, hash, false); err != nil {
		return err
	}
	return s.store.AppendAudit(ctx, AuditEntry{
		ActorID:      principal.UserID,
		TargetUserID: principal.UserID,
		EventType:    AuditEventPasswordChanged,
		At:           s.nowTime(),
	})
}

func (s *UserManagementService) requireUserManage(ctx context.Context, actor Principal) error {
	if s == nil || s.decider == nil {
		return ErrUnauthorized
	}
	result := s.decider.Decide(ctx, actor, PermissionUserManage, AccountSecurityManageContext())
	if !result.Allowed() {
		return ErrUnauthorized
	}
	return nil
}

func (s *UserManagementService) nowTime() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
