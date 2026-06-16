package auth

import "context"

const ProviderLocalPassword = "local_password"

type Credentials struct {
	Username string
	Password string
}

type ExternalIdentity struct {
	Provider       string
	ExternalUserID string
	Attributes     map[string]string
}

type AuthProvider interface {
	Authenticate(context.Context, Credentials) (Principal, error)
}

type ExternalIdentityMapper interface {
	MapExternalIdentity(context.Context, ExternalIdentity) (Principal, error)
}

type LocalPasswordProvider struct {
	authenticator *Authenticator
}

func NewLocalPasswordProvider(authenticator *Authenticator) *LocalPasswordProvider {
	return &LocalPasswordProvider{authenticator: authenticator}
}

func (p *LocalPasswordProvider) Authenticate(ctx context.Context, credentials Credentials) (Principal, error) {
	if p == nil || p.authenticator == nil {
		return Principal{}, ErrInvalidCredentials
	}
	principal, err := p.authenticator.Authenticate(ctx, credentials.Username, credentials.Password)
	if err != nil {
		return Principal{}, err
	}
	principal.Provider = ProviderLocalPassword
	return principal, nil
}

type ExternalIdentityMappingStore interface {
	UserByExternalIdentity(context.Context, string, string) (User, error)
	RolesForUser(context.Context, string) ([]RoleCode, error)
}

type ExternalIdentityAdapter struct {
	store ExternalIdentityMappingStore
}

func NewExternalIdentityAdapter(store ExternalIdentityMappingStore) *ExternalIdentityAdapter {
	return &ExternalIdentityAdapter{store: store}
}

func (a *ExternalIdentityAdapter) MapExternalIdentity(ctx context.Context, identity ExternalIdentity) (Principal, error) {
	if a == nil || a.store == nil {
		return Principal{}, ErrInvalidCredentials
	}
	user, err := a.store.UserByExternalIdentity(ctx, identity.Provider, identity.ExternalUserID)
	if err != nil {
		return Principal{}, err
	}
	roles, err := a.store.RolesForUser(ctx, user.ID)
	if err != nil {
		return Principal{}, err
	}
	return principalFromUser(user, roles, identity.Provider), nil
}
