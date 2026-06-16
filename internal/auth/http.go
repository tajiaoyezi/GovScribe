package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type HTTPHandler struct {
	Provider AuthProvider
	Tokens   *TokenManager
	Users    UserStore
	Manage   *UserManagementService
	Decider  *AccessDecisionService
}

func NewHTTPHandler(provider AuthProvider, tokens *TokenManager, users UserStore, manage *UserManagementService, decider *AccessDecisionService) *HTTPHandler {
	return &HTTPHandler{
		Provider: provider,
		Tokens:   tokens,
		Users:    users,
		Manage:   manage,
		Decider:  decider,
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
		h.handleLogin(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/auth/change-password":
		h.handleChangePassword(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/admin/users":
		h.handleListUsers(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/admin/users":
		h.handleCreateUser(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/admin/users/"):
		h.handleUserAction(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if h.Provider == nil || h.Tokens == nil {
		writeError(w, http.StatusUnauthorized)
		return
	}
	principal, err := h.Provider.Authenticate(r.Context(), Credentials{Username: req.Username, Password: req.Password})
	if err != nil {
		writeError(w, http.StatusUnauthorized)
		return
	}
	token, err := h.Tokens.Sign(principal)
	if err != nil {
		writeError(w, http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *HTTPHandler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requirePrincipal(w, r, true)
	if !ok {
		return
	}
	if h.Manage == nil {
		writeError(w, http.StatusForbidden)
		return
	}
	var req struct {
		NewPassword string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.Manage.ChangeOwnPassword(r.Context(), principal, req.NewPassword); err != nil {
		writeMappedError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTPHandler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requirePrincipal(w, r, false)
	if !ok {
		return
	}
	if h.Users == nil {
		writeError(w, http.StatusForbidden)
		return
	}
	if !h.decideUserManage(r, principal) {
		writeError(w, http.StatusForbidden)
		return
	}
	users, err := h.Users.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError)
		return
	}
	out := make([]managedUserResponse, 0, len(users))
	for _, user := range users {
		roles, err := h.Users.RolesForUser(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError)
			return
		}
		out = append(out, managedUserView(user, roles))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *HTTPHandler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requirePrincipal(w, r, false)
	if !ok {
		return
	}
	if h.Manage == nil {
		writeError(w, http.StatusForbidden)
		return
	}
	var req struct {
		Username   string   `json:"username"`
		Password   string   `json:"password"`
		Department string   `json:"department"`
		Role       RoleCode `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := h.Manage.CreateUser(r.Context(), principal, CreateUserRequest{
		Username:   req.Username,
		Password:   req.Password,
		Department: req.Department,
		Role:       req.Role,
	})
	if err != nil {
		writeMappedError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, managedUserView(user, []RoleCode{req.Role}))
}

func (h *HTTPHandler) handleUserAction(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requirePrincipal(w, r, false)
	if !ok {
		return
	}
	if h.Manage == nil {
		writeError(w, http.StatusForbidden)
		return
	}
	userID, action, ok := parseUserAction(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case r.Method == http.MethodPost && action == "disable":
		err := h.Manage.DisableUser(r.Context(), principal, userID)
		writeActionResult(w, err)
	case r.Method == http.MethodPut && action == "role":
		var req struct {
			Role RoleCode `json:"role"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		err := h.Manage.AssignRole(r.Context(), principal, userID, req.Role)
		writeActionResult(w, err)
	case r.Method == http.MethodPost && action == "password":
		var req struct {
			Password string `json:"password"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		err := h.Manage.ResetPassword(r.Context(), principal, userID, req.Password)
		writeActionResult(w, err)
	default:
		http.NotFound(w, r)
	}
}

func (h *HTTPHandler) requirePrincipal(w http.ResponseWriter, r *http.Request, allowMustChange bool) (Principal, bool) {
	if h == nil || h.Tokens == nil || h.Users == nil {
		writeError(w, http.StatusUnauthorized)
		return Principal{}, false
	}
	principal, err := h.Tokens.PrincipalFromToken(r.Context(), bearerToken(r.Header.Get("Authorization")), StorePrincipalResolver{Users: h.Users})
	if err != nil || !principal.Authenticated {
		writeError(w, http.StatusUnauthorized)
		return Principal{}, false
	}
	if principal.MustChangePassword && !allowMustChange {
		writeError(w, http.StatusForbidden)
		return Principal{}, false
	}
	return principal, true
}

func (h *HTTPHandler) decideUserManage(r *http.Request, principal Principal) bool {
	if h.Decider == nil {
		return false
	}
	return h.Decider.Decide(r.Context(), principal, PermissionUserManage, AccountSecurityManageContext()).Allowed()
}

func parseUserAction(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, "/api/admin/users/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func writeActionResult(w http.ResponseWriter, err error) {
	if err != nil {
		writeMappedError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeMappedError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized)
	case errors.Is(err, ErrUnauthorized):
		writeError(w, http.StatusForbidden)
	case errors.Is(err, ErrPasswordPolicyViolation):
		writeError(w, http.StatusBadRequest)
	default:
		writeError(w, http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

type managedUserResponse struct {
	ID                 string     `json:"id"`
	Username           string     `json:"username"`
	Department         string     `json:"department"`
	IsActive           bool       `json:"isActive"`
	MustChangePassword bool       `json:"mustChangePassword"`
	Roles              []RoleCode `json:"roles"`
}

func managedUserView(user User, roles []RoleCode) managedUserResponse {
	return managedUserResponse{
		ID:                 user.ID,
		Username:           user.Username,
		Department:         user.Department,
		IsActive:           user.IsActive,
		MustChangePassword: user.MustChangePassword,
		Roles:              append([]RoleCode(nil), roles...),
	}
}
