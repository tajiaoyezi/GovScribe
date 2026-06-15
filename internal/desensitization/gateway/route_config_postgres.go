package gateway

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

var routePolicySelectColumns = []string{
	"classification", "target_network", "model_config_id", "allow_degraded_public", "updated_by", "updated_at",
}

var dispositionAuditSelectColumns = []string{
	"actor_id", "request_id", "content_classification", "original_diff", "match_details",
	"disposition_event", "disposition_reason", "created_at",
}

type PostgresRouteConfigStore struct {
	db *sql.DB
}

func NewPostgresRouteConfigStore(db *sql.DB) *PostgresRouteConfigStore {
	return &PostgresRouteConfigStore{db: db}
}

func (s *PostgresRouteConfigStore) GetPolicy(ctx context.Context, level llm.ContentSecurityLevel) (RoutePolicy, error) {
	row := s.db.QueryRowContext(ctx, selectRoutePolicySQL("WHERE classification = $1"), routePolicyDBLevel(level))
	policy, err := scanRoutePolicy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultPolicy(level), nil
	}
	if err != nil {
		return RoutePolicy{}, err
	}
	return hardenPolicy(policy), nil
}

func (s *PostgresRouteConfigStore) SavePolicy(ctx context.Context, policy RoutePolicy) error {
	return execSavePolicy(ctx, s.db, policy)
}

func (s *PostgresRouteConfigStore) SavePolicyWithAudit(ctx context.Context, policy RoutePolicy, actorID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := lockRoutePolicy(ctx, tx, policy.Level); err != nil {
		_ = tx.Rollback()
		return err
	}
	before, err := getPolicyWithQueryer(ctx, tx, policy.Level)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := execSavePolicy(ctx, tx, policy); err != nil {
		_ = tx.Rollback()
		return err
	}
	audit := routePolicyAuditEntry(actorID, before, hardenPolicy(policy))
	if err := execAppendDispositionAudit(ctx, tx, audit); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit route policy audit transaction: %w", err)
	}
	return nil
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func lockRoutePolicy(ctx context.Context, execer sqlExecutor, level llm.ContentSecurityLevel) error {
	_, err := execer.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, routePolicyDBLevel(level))
	return err
}

type routePolicyQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getPolicyWithQueryer(ctx context.Context, queryer routePolicyQueryer, level llm.ContentSecurityLevel) (RoutePolicy, error) {
	row := queryer.QueryRowContext(ctx, selectRoutePolicySQL("WHERE classification = $1"), routePolicyDBLevel(level))
	policy, err := scanRoutePolicy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultPolicy(level), nil
	}
	if err != nil {
		return RoutePolicy{}, err
	}
	return hardenPolicy(policy), nil
}

func execSavePolicy(ctx context.Context, execer sqlExecutor, policy RoutePolicy) error {
	policy = hardenPolicy(policy)
	var modelConfigID any
	if policy.ModelConfigID != "" {
		modelConfigID = policy.ModelConfigID
	}
	_, err := execer.ExecContext(ctx, `
INSERT INTO security_classification_routes (
	classification, target_network, model_config_id, allow_degraded_public, updated_by, updated_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (classification) DO UPDATE SET
	target_network = EXCLUDED.target_network,
	model_config_id = EXCLUDED.model_config_id,
	allow_degraded_public = EXCLUDED.allow_degraded_public,
	updated_by = EXCLUDED.updated_by,
	updated_at = EXCLUDED.updated_at`,
		routePolicyDBLevel(policy.Level), string(policy.TargetNetwork), modelConfigID,
		policy.AllowDegradedPublic, policy.UpdatedBy, policy.UpdatedAt,
	)
	return err
}

func (s *PostgresRouteConfigStore) AppendDispositionAudit(ctx context.Context, entry DispositionAuditEntry) error {
	return execAppendDispositionAudit(ctx, s.db, entry)
}

func execAppendDispositionAudit(ctx context.Context, execer sqlExecutor, entry DispositionAuditEntry) error {
	entry = normalizeDispositionAuditEntry(entry)
	_, err := execer.ExecContext(ctx, `
INSERT INTO desensitization_audit_logs (
	actor_id, request_id, content_classification, original_diff, match_details,
	disposition_event, disposition_reason, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		entry.ActorID,
		entry.RequestID,
		routePolicyDBLevel(entry.ContentClassification),
		entry.OriginalDiff,
		entry.MatchDetails,
		string(entry.DispositionEvent),
		string(entry.DispositionReason),
		entry.At,
	)
	return err
}

func (s *PostgresRouteConfigStore) ListDispositionAudits(ctx context.Context, query DispositionAuditQuery) ([]DispositionAuditEntry, error) {
	args := make([]any, 0, len(query.ContentClassifications)+2)
	var filters []string
	if len(query.ContentClassifications) > 0 {
		placeholders := make([]string, 0, len(query.ContentClassifications))
		for _, level := range query.ContentClassifications {
			args = append(args, routePolicyDBLevel(level))
			placeholders = append(placeholders, "$"+strconv.Itoa(len(args)))
		}
		filters = append(filters, "content_classification IN ("+strings.Join(placeholders, ", ")+")")
	}
	if query.ActorID != "" {
		args = append(args, query.ActorID)
		filters = append(filters, "actor_id = $"+strconv.Itoa(len(args)))
	}
	if query.RequestID != "" {
		args = append(args, query.RequestID)
		filters = append(filters, "request_id = $"+strconv.Itoa(len(args)))
	}

	sqlQuery := "SELECT " + strings.Join(dispositionAuditSelectColumns, ", ") + " FROM desensitization_audit_logs"
	if len(filters) > 0 {
		sqlQuery += " WHERE " + strings.Join(filters, " AND ")
	}
	sqlQuery += " ORDER BY created_at DESC, id DESC LIMIT " + strconv.Itoa(auditQueryLimit(query.Limit))

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []DispositionAuditEntry
	for rows.Next() {
		entry, err := scanDispositionAudit(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func selectRoutePolicySQL(suffix string) string {
	query := "SELECT " + strings.Join(routePolicySelectColumns, ", ") + " FROM security_classification_routes"
	if suffix != "" {
		query += " " + suffix
	}
	return query
}

func routePolicyColumns() []string {
	out := make([]string, len(routePolicySelectColumns))
	copy(out, routePolicySelectColumns)
	return out
}

func dispositionAuditColumns() []string {
	out := make([]string, len(dispositionAuditSelectColumns))
	copy(out, dispositionAuditSelectColumns)
	return out
}

type routePolicyScanner interface {
	Scan(dest ...any) error
}

type dispositionAuditScanner interface {
	Scan(dest ...any) error
}

func scanRoutePolicy(scanner routePolicyScanner) (RoutePolicy, error) {
	var policy RoutePolicy
	var level, target string
	var modelConfigID sql.NullString
	err := scanner.Scan(
		&level,
		&target,
		&modelConfigID,
		&policy.AllowDegradedPublic,
		&policy.UpdatedBy,
		&policy.UpdatedAt,
	)
	if err != nil {
		return RoutePolicy{}, err
	}
	policy.Level = normalizeLevel(llm.ContentSecurityLevel(level))
	policy.TargetNetwork = llm.Network(target)
	if modelConfigID.Valid {
		policy.ModelConfigID = modelConfigID.String
	}
	return policy, nil
}

func scanDispositionAudit(scanner dispositionAuditScanner) (DispositionAuditEntry, error) {
	var entry DispositionAuditEntry
	var level, event, reason string
	err := scanner.Scan(
		&entry.ActorID,
		&entry.RequestID,
		&level,
		&entry.OriginalDiff,
		&entry.MatchDetails,
		&event,
		&reason,
		&entry.At,
	)
	if err != nil {
		return DispositionAuditEntry{}, err
	}
	entry.ContentClassification = normalizeLevel(llm.ContentSecurityLevel(level))
	entry.DispositionEvent = DispositionEvent(event)
	entry.DispositionReason = DispositionReason(reason)
	return normalizeDispositionAuditEntry(entry), nil
}

func defaultPolicy(level llm.ContentSecurityLevel) RoutePolicy {
	for _, policy := range defaultRoutePolicies() {
		if policy.Level == normalizeLevel(level) {
			return policy
		}
	}
	return RoutePolicy{Level: llm.ContentSecurityLevelUnknown, TargetNetwork: llm.NetworkPrivate}
}

func hardenPolicy(policy RoutePolicy) RoutePolicy {
	originalTarget := policy.TargetNetwork
	policy.Level = normalizeLevel(policy.Level)
	if policy.Level == llm.ContentSecurityLevelClassified || policy.Level == llm.ContentSecurityLevelUnknown {
		policy.TargetNetwork = llm.NetworkPrivate
		policy.AllowDegradedPublic = false
		if originalTarget != llm.NetworkPrivate {
			policy.ModelConfigID = ""
		}
	}
	return policy
}
