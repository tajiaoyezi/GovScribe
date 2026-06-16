package auth

import (
	"context"
	"time"
)

type Authenticator struct {
	users   UserReader
	hasher  PasswordHasher
	limiter LoginFailureLimiter
	audit   AuditRecorder
	now     func() time.Time
}

func NewAuthenticator(users UserReader, hasher PasswordHasher, limiter LoginFailureLimiter, audit AuditRecorder) *Authenticator {
	return &Authenticator{
		users:   users,
		hasher:  hasher,
		limiter: limiter,
		audit:   audit,
		now:     time.Now,
	}
}

func (a *Authenticator) Authenticate(ctx context.Context, username, password string) (Principal, error) {
	if a.limiter != nil && a.limiter.Blocked(username) {
		a.appendAudit(ctx, AuditEntry{
			ActorID:   loginKey(username),
			EventType: AuditEventLoginBlocked,
			Details:   map[string]string{"username": loginKey(username)},
			At:        a.nowTime(),
		})
		return Principal{}, ErrLoginTemporarilyBlocked
	}
	user, err := a.users.UserByUsername(ctx, username)
	if err != nil {
		a.recordFailure(ctx, username, "")
		return Principal{}, ErrInvalidCredentials
	}
	if !user.IsActive {
		a.recordFailure(ctx, username, user.ID)
		return Principal{}, ErrInvalidCredentials
	}
	if a.hasher == nil || !a.hasher.Verify(user.PasswordHash, password) {
		a.recordFailure(ctx, username, user.ID)
		return Principal{}, ErrInvalidCredentials
	}
	if a.limiter != nil {
		a.limiter.RecordSuccess(username)
	}
	roles, err := a.users.RolesForUser(ctx, user.ID)
	if err != nil {
		return Principal{}, err
	}
	return principalFromUser(user, roles, ProviderLocalPassword), nil
}

func (a *Authenticator) recordFailure(ctx context.Context, username, userID string) {
	blocked := false
	if a.limiter != nil {
		blocked = a.limiter.RecordFailure(username)
	}
	a.appendAudit(ctx, AuditEntry{
		ActorID:      loginKey(username),
		TargetUserID: userID,
		EventType:    AuditEventLoginFailed,
		Details:      map[string]string{"username": loginKey(username)},
		At:           a.nowTime(),
	})
	if blocked {
		a.appendAudit(ctx, AuditEntry{
			ActorID:      loginKey(username),
			TargetUserID: userID,
			EventType:    AuditEventLoginBlocked,
			Details:      map[string]string{"username": loginKey(username)},
			At:           a.nowTime(),
		})
	}
}

func (a *Authenticator) appendAudit(ctx context.Context, entry AuditEntry) {
	if a.audit == nil {
		return
	}
	_ = a.audit.AppendAudit(ctx, entry)
}

func (a *Authenticator) nowTime() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

func principalFromUser(user User, roles []RoleCode, provider string) Principal {
	return Principal{
		UserID:             user.ID,
		Username:           user.Username,
		Department:         user.Department,
		Roles:              append([]RoleCode(nil), roles...),
		MustChangePassword: user.MustChangePassword,
		Provider:           provider,
		Authenticated:      true,
	}
}
