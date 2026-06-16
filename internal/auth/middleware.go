package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const principalContextKey contextKey = "auth.principal"

type PrincipalResolver interface {
	ResolvePrincipal(context.Context, string) (Principal, error)
}

type StorePrincipalResolver struct {
	Users UserReader
}

func (r StorePrincipalResolver) ResolvePrincipal(ctx context.Context, userID string) (Principal, error) {
	if r.Users == nil {
		return Principal{}, ErrUnauthenticated
	}
	user, err := r.Users.UserByID(ctx, userID)
	if err != nil || !user.IsActive {
		return Principal{}, ErrUnauthenticated
	}
	roles, err := r.Users.RolesForUser(ctx, user.ID)
	if err != nil {
		return Principal{}, err
	}
	return principalFromUser(user, roles, ProviderLocalPassword), nil
}

type AuthMiddleware struct {
	Tokens                  *TokenManager
	Resolver                PrincipalResolver
	AllowMustChangePassword func(*http.Request) bool
	UnauthorizedStatus      int
	PasswordChangeStatus    int
}

func (m AuthMiddleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.Tokens == nil || m.Resolver == nil {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		token := bearerToken(r.Header.Get("Authorization"))
		if token == "" {
			http.Error(w, http.StatusText(http.StatusUnauthorized), m.unauthorizedStatus())
			return
		}
		principal, err := m.Tokens.PrincipalFromToken(r.Context(), token, m.Resolver)
		if err != nil || !principal.Authenticated {
			http.Error(w, http.StatusText(http.StatusUnauthorized), m.unauthorizedStatus())
			return
		}
		if principal.MustChangePassword && !m.allowPasswordChange(r) {
			http.Error(w, ErrMustChangePassword.Error(), m.passwordChangeStatus())
			return
		}
		next.ServeHTTP(w, r.WithContext(ContextWithPrincipal(r.Context(), principal)))
	})
}

func (m AuthMiddleware) unauthorizedStatus() int {
	if m.UnauthorizedStatus != 0 {
		return m.UnauthorizedStatus
	}
	return http.StatusUnauthorized
}

func (m AuthMiddleware) passwordChangeStatus() int {
	if m.PasswordChangeStatus != 0 {
		return m.PasswordChangeStatus
	}
	return http.StatusForbidden
}

func (m AuthMiddleware) allowPasswordChange(r *http.Request) bool {
	if m.AllowMustChangePassword == nil {
		return false
	}
	return m.AllowMustChangePassword(r)
}

func ContextWithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey).(Principal)
	return principal, ok
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}
