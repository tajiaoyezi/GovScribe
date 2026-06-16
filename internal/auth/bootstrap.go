package auth

import (
	"context"
	"time"
)

type BootstrapConfig struct {
	Username   string
	Password   string
	Department string
}

func BootstrapInitialAdmin(ctx context.Context, store UserStore, hasher PasswordHasher, cfg BootstrapConfig) (bool, error) {
	if store == nil || hasher == nil {
		return false, ErrUnauthorized
	}
	exists, err := store.HasAnyUserWithRole(ctx, RoleSystemAdmin)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if _, err := store.UserByUsername(ctx, cfg.Username); err == nil {
		return false, nil
	}
	if err := (PasswordPolicy{}).Validate(cfg.Password); err != nil {
		return false, err
	}
	hash, err := hasher.Hash(cfg.Password)
	if err != nil {
		return false, err
	}
	now := time.Now()
	user := User{
		ID:                 newID(),
		Username:           cfg.Username,
		PasswordHash:       hash,
		PasswordAlgorithm:  hasher.Algorithm(),
		IsActive:           true,
		Department:         cfg.Department,
		MustChangePassword: true,
		CreatedAt:          now,
	}
	if err := store.CreateUser(ctx, user, []RoleCode{RoleSystemAdmin}); err != nil {
		return false, err
	}
	if err := store.AppendAudit(ctx, AuditEntry{
		ActorID:      "bootstrap",
		TargetUserID: user.ID,
		EventType:    AuditEventUserCreated,
		Details:      map[string]string{"username": cfg.Username, "role": string(RoleSystemAdmin)},
		At:           now,
	}); err != nil {
		return false, err
	}
	return true, nil
}
