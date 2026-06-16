package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) UserByUsername(ctx context.Context, username string) (User, error) {
	row := s.db.QueryRowContext(ctx, userSelectSQL("WHERE username = $1"), strings.TrimSpace(username))
	return scanUser(row)
}

func (s *PostgresStore) UserByID(ctx context.Context, id string) (User, error) {
	row := s.db.QueryRowContext(ctx, userSelectSQL("WHERE id = $1"), id)
	return scanUser(row)
}

func (s *PostgresStore) RolesForUser(ctx context.Context, id string) ([]RoleCode, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT role_code
FROM user_roles
WHERE user_id = $1
ORDER BY role_code`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []RoleCode
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, RoleCode(role))
	}
	return roles, rows.Err()
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, userSelectSQL("ORDER BY username"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *PostgresStore) HasAnyUserWithRole(ctx context.Context, role RoleCode) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM user_roles
	WHERE role_code = $1
)`, string(role)).Scan(&ok)
	return ok, err
}

func (s *PostgresStore) CreateUser(ctx context.Context, user User, roles []RoleCode) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO users (
	id, username, password_hash, password_hash_algorithm, is_active,
	department, must_change_password, created_at, disabled_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			user.ID, user.Username, user.PasswordHash, user.PasswordAlgorithm,
			user.IsActive, user.Department, user.MustChangePassword, user.CreatedAt, user.DisabledAt,
		)
		if err != nil {
			return err
		}
		return replaceUserRoles(ctx, tx, user.ID, roles)
	})
}

func (s *PostgresStore) SetUserActive(ctx context.Context, id string, active bool) error {
	var result sql.Result
	var err error
	if active {
		result, err = s.db.ExecContext(ctx, `
UPDATE users SET is_active = TRUE, disabled_at = NULL WHERE id = $1`, id)
	} else {
		result, err = s.db.ExecContext(ctx, `
UPDATE users SET is_active = FALSE, disabled_at = now() WHERE id = $1`, id)
	}
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *PostgresStore) SetUserRoles(ctx context.Context, id string, roles []RoleCode) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)", id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrUserNotFound
		}
		return replaceUserRoles(ctx, tx, id, roles)
	})
}

func (s *PostgresStore) UpdatePassword(ctx context.Context, id, hash string, mustChange bool) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE users
SET password_hash = $2,
	password_hash_algorithm = $3,
	must_change_password = $4
WHERE id = $1`, id, hash, PasswordHashAlgorithmBcrypt, mustChange)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *PostgresStore) AppendAudit(ctx context.Context, entry AuditEntry) error {
	details := entry.Details
	if details == nil {
		details = map[string]string{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO account_security_audit_logs (
	actor_id, target_user_id, event_type, details, created_at
) VALUES ($1, $2, $3, $4, $5)`,
		entry.ActorID, nullString(entry.TargetUserID), string(entry.EventType), string(encoded), entry.At,
	)
	return err
}

func (s *PostgresStore) ListAudits(ctx context.Context) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT actor_id, COALESCE(target_user_id, ''), event_type, details, created_at
FROM account_security_audit_logs
ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []AuditEntry
	for rows.Next() {
		var entry AuditEntry
		var eventType string
		var encoded []byte
		if err := rows.Scan(&entry.ActorID, &entry.TargetUserID, &eventType, &encoded, &entry.At); err != nil {
			return nil, err
		}
		entry.EventType = AuditEventType(eventType)
		if len(encoded) > 0 {
			if err := json.Unmarshal(encoded, &entry.Details); err != nil {
				return nil, err
			}
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *PostgresStore) PermissionDeclared(ctx context.Context, permission Permission) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM permissions WHERE code = $1)`, string(permission)).Scan(&ok)
	return ok, err
}

func (s *PostgresStore) RoleHasPermission(ctx context.Context, role RoleCode, permission Permission) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM role_permissions
	WHERE role_code = $1 AND permission_code = $2
)`, string(role), string(permission)).Scan(&ok)
	return ok, err
}

func (s *PostgresStore) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func replaceUserRoles(ctx context.Context, tx *sql.Tx, userID string, roles []RoleCode) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_roles WHERE user_id = $1", userID); err != nil {
		return err
	}
	for _, role := range roles {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO user_roles (user_id, role_code) VALUES ($1, $2)
ON CONFLICT (user_id, role_code) DO NOTHING`, userID, string(role)); err != nil {
			return err
		}
	}
	return nil
}

func requireAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func userSelectSQL(suffix string) string {
	query := `
SELECT id, username, password_hash, password_hash_algorithm, is_active,
	department, must_change_password, created_at, disabled_at
FROM users`
	if suffix != "" {
		query += " " + suffix
	}
	return query
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(scanner userScanner) (User, error) {
	var user User
	var disabled sql.NullTime
	err := scanner.Scan(
		&user.ID, &user.Username, &user.PasswordHash, &user.PasswordAlgorithm,
		&user.IsActive, &user.Department, &user.MustChangePassword, &user.CreatedAt, &disabled,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	if disabled.Valid {
		user.DisabledAt = &disabled.Time
	}
	return user, nil
}

func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: strings.TrimSpace(value) != ""}
}
