package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestAuthenticatorRejectsMissingAndWrongPasswordWithSameError(t *testing.T) {
	ctx := context.Background()
	store, hasher := seededAuthStore(t)
	authenticator := NewAuthenticator(store, hasher, NewWindowLoginFailureLimiter(5, time.Minute), store)

	_, missingErr := authenticator.Authenticate(ctx, "missing", "bad")
	_, wrongErr := authenticator.Authenticate(ctx, "alice", "bad")
	if !errors.Is(missingErr, ErrInvalidCredentials) || !errors.Is(wrongErr, ErrInvalidCredentials) {
		t.Fatalf("errors = %v / %v, want ErrInvalidCredentials for both", missingErr, wrongErr)
	}
}

func TestAuthenticatorRejectsInactiveAccountEvenWithCorrectPassword(t *testing.T) {
	ctx := context.Background()
	store, hasher := seededAuthStore(t)
	if err := store.SetUserActive(ctx, "user-1", false); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	authenticator := NewAuthenticator(store, hasher, NewWindowLoginFailureLimiter(5, time.Minute), store)

	_, err := authenticator.Authenticate(ctx, "alice", "secret")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("inactive login error = %v, want ErrInvalidCredentials", err)
	}
}

func TestAuthenticatorBlocksAfterFailureThresholdAndAuditsWithoutSecrets(t *testing.T) {
	ctx := context.Background()
	store, hasher := seededAuthStore(t)
	limiter := NewWindowLoginFailureLimiter(2, time.Minute)
	authenticator := NewAuthenticator(store, hasher, limiter, store)

	for i := 0; i < 2; i++ {
		_, _ = authenticator.Authenticate(ctx, "alice", "wrong")
	}
	_, err := authenticator.Authenticate(ctx, "alice", "secret")
	if !errors.Is(err, ErrLoginTemporarilyBlocked) {
		t.Fatalf("blocked login error = %v, want ErrLoginTemporarilyBlocked", err)
	}
	for _, entry := range store.Audits() {
		for key, value := range entry.Details {
			if key == "password" || key == "password_hash" || value == "secret" {
				t.Fatalf("audit leaks secret: %#v", entry)
			}
		}
	}
}

func TestLocalPasswordProviderReturnsUnifiedPrincipal(t *testing.T) {
	ctx := context.Background()
	store, hasher := seededAuthStore(t)
	provider := NewLocalPasswordProvider(NewAuthenticator(store, hasher, nil, store))

	principal, err := provider.Authenticate(ctx, Credentials{Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !principal.Authenticated || principal.UserID != "user-1" || !principal.HasRole(RoleBusinessUser) {
		t.Fatalf("principal = %#v, want unified internal identity", principal)
	}
}

func seededAuthStore(t *testing.T) (*MemoryStore, BcryptHasher) {
	t.Helper()
	store := NewMemoryStore()
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	hash, err := hasher.Hash("secret")
	if err != nil {
		t.Fatalf("hash seed password: %v", err)
	}
	if err := store.CreateUser(context.Background(), User{
		ID:                 "user-1",
		Username:           "alice",
		PasswordHash:       hash,
		PasswordAlgorithm:  hasher.Algorithm(),
		IsActive:           true,
		Department:         "office",
		MustChangePassword: false,
		CreatedAt:          time.Now(),
	}, []RoleCode{RoleBusinessUser}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return store, hasher
}
