CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    password_hash_algorithm TEXT NOT NULL DEFAULT 'bcrypt',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    department TEXT NOT NULL DEFAULT '',
    must_change_password BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS users_active_idx
    ON users (is_active, username);

CREATE TABLE IF NOT EXISTS roles (
    code TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS permissions (
    code TEXT PRIMARY KEY,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_code TEXT NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    permission_code TEXT NOT NULL REFERENCES permissions(code) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_code, permission_code)
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_code TEXT NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_code)
);

CREATE INDEX IF NOT EXISTS user_roles_role_idx
    ON user_roles (role_code);

CREATE TABLE IF NOT EXISTS account_security_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_id TEXT NOT NULL,
    target_user_id TEXT,
    event_type TEXT NOT NULL CHECK (event_type IN (
        'user_created',
        'user_disabled',
        'role_assigned',
        'password_reset',
        'password_changed',
        'login_failed',
        'login_blocked',
        'authorization_denied'
    )),
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT account_security_audit_no_password_chk CHECK (
        NOT (details ? 'password')
        AND NOT (details ? 'password_hash')
        AND NOT (details ? 'new_password')
    )
);

CREATE INDEX IF NOT EXISTS account_security_audit_logs_actor_idx
    ON account_security_audit_logs (actor_id, created_at DESC);

CREATE INDEX IF NOT EXISTS account_security_audit_logs_target_idx
    ON account_security_audit_logs (target_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS data_security_acl_grants (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    classification TEXT NOT NULL,
    action TEXT NOT NULL,
    allow BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, resource_type, resource_id, classification, action)
);

CREATE INDEX IF NOT EXISTS data_security_acl_grants_lookup_idx
    ON data_security_acl_grants (user_id, resource_type, resource_id, classification, action)
    WHERE allow = TRUE;

CREATE TABLE IF NOT EXISTS data_security_role_acl_grants (
    id BIGSERIAL PRIMARY KEY,
    role_code TEXT NOT NULL REFERENCES roles(code) ON DELETE CASCADE,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    classification TEXT NOT NULL,
    action TEXT NOT NULL,
    allow BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (role_code, resource_type, resource_id, classification, action)
);

CREATE INDEX IF NOT EXISTS data_security_role_acl_grants_lookup_idx
    ON data_security_role_acl_grants (role_code, resource_type, resource_id, classification, action)
    WHERE allow = TRUE;

INSERT INTO roles (code, display_name)
VALUES
    ('system_admin', 'System Administrator'),
    ('secretary', 'Secretary'),
    ('business_user', 'Business User'),
    ('auditor', 'Auditor')
ON CONFLICT (code) DO UPDATE SET display_name = EXCLUDED.display_name;

INSERT INTO permissions (code, description)
VALUES
    ('document.open', 'Open document'),
    ('document.edit', 'Edit document'),
    ('document.export', 'Export document'),
    ('review.online', 'Online review'),
    ('adopt.decide', 'Adoption decision'),
    ('audit.read', 'Read audit logs'),
    ('dict.manage', 'Manage desensitization dictionary'),
    ('model.config', 'Manage model provider configuration'),
    ('template.search', 'Search corpus templates'),
    ('template.ingest', 'Ingest corpus templates'),
    ('draft.create', 'Create draft'),
    ('user.manage', 'Manage users and roles')
ON CONFLICT (code) DO UPDATE SET description = EXCLUDED.description;

DELETE FROM role_permissions
WHERE role_code IN ('system_admin', 'secretary', 'business_user', 'auditor');

INSERT INTO role_permissions (role_code, permission_code)
VALUES
    ('system_admin', 'model.config'),
    ('system_admin', 'dict.manage'),
    ('system_admin', 'audit.read'),
    ('system_admin', 'user.manage'),
    ('secretary', 'document.open'),
    ('secretary', 'document.edit'),
    ('secretary', 'document.export'),
    ('secretary', 'review.online'),
    ('secretary', 'adopt.decide'),
    ('secretary', 'template.search'),
    ('secretary', 'template.ingest'),
    ('secretary', 'draft.create'),
    ('business_user', 'document.open'),
    ('business_user', 'document.edit'),
    ('business_user', 'document.export'),
    ('business_user', 'draft.create'),
    ('business_user', 'template.search'),
    ('auditor', 'audit.read')
ON CONFLICT (role_code, permission_code) DO NOTHING;

INSERT INTO data_security_role_acl_grants (
    role_code, resource_type, resource_id, classification, action, allow
)
VALUES
    ('system_admin', 'account_security', 'users', 'internal', 'manage', TRUE),
    ('system_admin', 'account_security', 'users', 'internal', 'read', TRUE),
    ('auditor', 'account_security', 'users', 'internal', 'read', TRUE)
ON CONFLICT (role_code, resource_type, resource_id, classification, action)
DO UPDATE SET allow = EXCLUDED.allow;
