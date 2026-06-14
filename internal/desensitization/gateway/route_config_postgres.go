package gateway

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

var routePolicySelectColumns = []string{
	"classification", "target_network", "model_config_id", "allow_degraded_public", "updated_by", "updated_at",
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
	policy = hardenPolicy(policy)
	var modelConfigID any
	if policy.ModelConfigID != "" {
		modelConfigID = policy.ModelConfigID
	}
	_, err := s.db.ExecContext(ctx, `
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

type routePolicyScanner interface {
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
