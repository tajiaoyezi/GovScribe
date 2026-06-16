package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHTTPLoginChangePasswordAndAdminUsers(t *testing.T) {
	ctx := context.Background()
	handler, store, tokens := newTestHTTPHandler(t)

	loginResp := postJSON(t, handler, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-secret",
	}, "")
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResp.Code, loginResp.Body.String())
	}
	var loginBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if loginBody.Token == "" {
		t.Fatal("login did not return token")
	}

	createResp := postJSON(t, handler, "/api/admin/users", map[string]any{
		"username":   "writer",
		"password":   "writer-secret",
		"department": "office",
		"role":       RoleBusinessUser,
	}, loginBody.Token)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", createResp.Code, createResp.Body.String())
	}
	var created managedUserResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created user: %v", err)
	}
	if created.Username != "writer" || len(created.Roles) != 1 || created.Roles[0] != RoleBusinessUser {
		t.Fatalf("created = %#v, want writer business_user", created)
	}

	disableResp := postJSON(t, handler, "/api/admin/users/"+created.ID+"/disable", map[string]string{}, loginBody.Token)
	if disableResp.Code != http.StatusNoContent {
		t.Fatalf("disable status = %d, body=%s", disableResp.Code, disableResp.Body.String())
	}
	disabled, err := store.UserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("disabled user: %v", err)
	}
	if disabled.IsActive {
		t.Fatal("disable endpoint did not deactivate user")
	}

	roleResp := requestJSON(t, handler, http.MethodPut, "/api/admin/users/"+created.ID+"/role", map[string]RoleCode{"role": RoleSecretary}, loginBody.Token)
	if roleResp.Code != http.StatusNoContent {
		t.Fatalf("role status = %d, body=%s", roleResp.Code, roleResp.Body.String())
	}
	roles, err := store.RolesForUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("roles after assign: %v", err)
	}
	if len(roles) != 1 || roles[0] != RoleSecretary {
		t.Fatalf("roles = %#v, want secretary", roles)
	}
	resetResp := postJSON(t, handler, "/api/admin/users/"+created.ID+"/password", map[string]string{"password": "reset-secret"}, loginBody.Token)
	if resetResp.Code != http.StatusNoContent {
		t.Fatalf("reset status = %d, body=%s", resetResp.Code, resetResp.Body.String())
	}
	resetUser, err := store.UserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("reset user: %v", err)
	}
	if !resetUser.MustChangePassword {
		t.Fatal("reset password endpoint must force password change")
	}

	admin, err := store.UserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	if err := store.UpdatePassword(ctx, admin.ID, admin.PasswordHash, true); err != nil {
		t.Fatalf("force password change: %v", err)
	}
	forcedToken, err := tokens.Sign(Principal{UserID: admin.ID, Username: admin.Username, Roles: []RoleCode{RoleSystemAdmin}, MustChangePassword: true, Authenticated: true})
	if err != nil {
		t.Fatalf("sign forced token: %v", err)
	}
	blockedResp := getJSON(t, handler, "/api/admin/users", forcedToken)
	if blockedResp.Code != http.StatusForbidden {
		t.Fatalf("must-change admin list status = %d, want 403", blockedResp.Code)
	}
	changeResp := postJSON(t, handler, "/api/auth/change-password", map[string]string{"newPassword": "changed-secret"}, forcedToken)
	if changeResp.Code != http.StatusNoContent {
		t.Fatalf("change password status = %d, body=%s", changeResp.Code, changeResp.Body.String())
	}
	changed, err := store.UserByID(ctx, admin.ID)
	if err != nil {
		t.Fatalf("changed admin: %v", err)
	}
	if changed.MustChangePassword {
		t.Fatal("change-password endpoint did not clear must_change_password")
	}
}

func TestHTTPRejectsNonAdminUserManagement(t *testing.T) {
	handler, _, tokens := newTestHTTPHandler(t)
	token, err := tokens.Sign(Principal{UserID: "writer-1", Username: "writer", Roles: []RoleCode{RoleBusinessUser}, Authenticated: true})
	if err != nil {
		t.Fatalf("sign writer token: %v", err)
	}
	resp := postJSON(t, handler, "/api/admin/users", map[string]any{
		"username": "blocked",
		"password": "blocked-secret",
		"role":     RoleBusinessUser,
	}, token)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("non-admin create status = %d, body=%s", resp.Code, resp.Body.String())
	}
}

func newTestHTTPHandler(t *testing.T) (*HTTPHandler, *MemoryStore, *TokenManager) {
	t.Helper()
	store := NewMemoryStore()
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	adminHash, err := hasher.Hash("admin-secret")
	if err != nil {
		t.Fatalf("hash admin: %v", err)
	}
	if err := store.CreateUser(context.Background(), User{
		ID:                 "admin-1",
		Username:           "admin",
		PasswordHash:       adminHash,
		PasswordAlgorithm:  hasher.Algorithm(),
		IsActive:           true,
		MustChangePassword: false,
		CreatedAt:          time.Now(),
	}, []RoleCode{RoleSystemAdmin}); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	writerHash, err := hasher.Hash("writer-secret")
	if err != nil {
		t.Fatalf("hash writer: %v", err)
	}
	if err := store.CreateUser(context.Background(), User{
		ID:                 "writer-1",
		Username:           "writer",
		PasswordHash:       writerHash,
		PasswordAlgorithm:  hasher.Algorithm(),
		IsActive:           true,
		MustChangePassword: false,
		CreatedAt:          time.Now(),
	}, []RoleCode{RoleBusinessUser}); err != nil {
		t.Fatalf("seed writer: %v", err)
	}
	tokens := testTokenManager(t)
	authenticator := NewAuthenticator(store, hasher, NewWindowLoginFailureLimiter(5, time.Minute), store)
	decider := NewAccessDecisionServiceWithAudit(NewRBACService(store), StaticDataACL{Allow: true}, store)
	manage := NewUserManagementService(store, hasher, decider)
	handler := NewHTTPHandler(NewLocalPasswordProvider(authenticator), tokens, store, manage, decider)
	return handler, store, tokens
}

func postJSON(t *testing.T, handler http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	return requestJSON(t, handler, http.MethodPost, path, body, token)
}

func requestJSON(t *testing.T, handler http.Handler, method string, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func getJSON(t *testing.T, handler http.Handler, path string, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
