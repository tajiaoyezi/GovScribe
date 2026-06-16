package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestTokenManagerSignsParsesAndSeparatesOnlyOfficeAudience(t *testing.T) {
	manager, err := NewTokenManager(SessionConfig{
		Issuer:             "govscribe",
		Audience:           "govscribe-session",
		Secret:             []byte("session-secret"),
		TTL:                time.Hour,
		OnlyOfficeAudience: "onlyoffice-callback",
		OnlyOfficeSecret:   []byte("onlyoffice-secret"),
	})
	if err != nil {
		t.Fatalf("new token manager: %v", err)
	}
	token, err := manager.Sign(Principal{
		UserID:        "user-1",
		Username:      "alice",
		Roles:         []RoleCode{RoleSecretary},
		Authenticated: true,
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := manager.Parse(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Subject != "user-1" || len(claims.Roles) != 1 || claims.Roles[0] != string(RoleSecretary) {
		t.Fatalf("claims = %#v, want principal identity and role", claims)
	}
	if _, err := NewTokenManager(SessionConfig{
		Issuer:             "govscribe",
		Audience:           "same",
		Secret:             []byte("shared"),
		TTL:                time.Hour,
		OnlyOfficeAudience: "same",
		OnlyOfficeSecret:   []byte("shared"),
	}); !errors.Is(err, ErrTokenPurposeMismatch) {
		t.Fatalf("shared session/onlyoffice config error = %v, want ErrTokenPurposeMismatch", err)
	}
}

func TestTokenManagerRejectsExpiredAndTamperedTokens(t *testing.T) {
	manager, err := NewTokenManager(SessionConfig{
		Issuer:   "govscribe",
		Audience: "govscribe-session",
		Secret:   []byte("session-secret"),
		TTL:      time.Minute,
	})
	if err != nil {
		t.Fatalf("new token manager: %v", err)
	}
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	token, err := manager.Sign(Principal{UserID: "user-1", Username: "alice", Authenticated: true})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := manager.Parse(token + "x"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("tampered parse error = %v, want ErrInvalidToken", err)
	}
	manager.now = func() time.Time { return now.Add(2 * time.Minute) }
	if _, err := manager.Parse(token); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expired parse error = %v, want ErrExpiredToken", err)
	}
}

func TestAuthMiddlewareRejectsMissingTamperedAndInactiveToken(t *testing.T) {
	ctx := context.Background()
	store, hasher := seededAuthStore(t)
	manager := testTokenManager(t)
	token, err := manager.Sign(Principal{UserID: "user-1", Username: "alice", Authenticated: true})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	mw := AuthMiddleware{Tokens: manager, Resolver: StorePrincipalResolver{Users: store}}
	called := false
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := PrincipalFromContext(r.Context()); !ok {
			t.Fatal("principal missing from context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("missing token code=%d called=%v, want 401 and not called", rec.Code, called)
	}

	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token+"x")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("tampered token code=%d called=%v, want 401 and not called", rec.Code, called)
	}

	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || !called {
		t.Fatalf("valid token code=%d called=%v, want 204 and called", rec.Code, called)
	}

	called = false
	if err := store.SetUserActive(ctx, "user-1", false); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("inactive token code=%d called=%v, want 401 and not called", rec.Code, called)
	}

	_ = hasher
}

func TestAuthMiddlewareRequiresFirstPasswordChange(t *testing.T) {
	store := NewMemoryStore()
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	hash, err := hasher.Hash("secret")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := store.CreateUser(context.Background(), User{
		ID:                 "user-2",
		Username:           "bob",
		PasswordHash:       hash,
		PasswordAlgorithm:  hasher.Algorithm(),
		IsActive:           true,
		MustChangePassword: true,
		CreatedAt:          time.Now(),
	}, []RoleCode{RoleBusinessUser}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	manager := testTokenManager(t)
	token, err := manager.Sign(Principal{UserID: "user-2", Username: "bob", Authenticated: true})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	mw := AuthMiddleware{
		Tokens:                  manager,
		Resolver:                StorePrincipalResolver{Users: store},
		AllowMustChangePassword: func(r *http.Request) bool { return r.URL.Path == "/auth/change-password" },
	}
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("must-change protected code = %d, want 403", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/auth/change-password", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change-password code = %d, want 204", rec.Code)
	}
}

func testTokenManager(t *testing.T) *TokenManager {
	t.Helper()
	manager, err := NewTokenManager(SessionConfig{
		Issuer:   "govscribe",
		Audience: "govscribe-session",
		Secret:   []byte("session-secret"),
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatalf("new token manager: %v", err)
	}
	return manager
}
