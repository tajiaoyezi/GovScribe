package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

var modelConfigSelectColumns = []string{
	"id", "provider_type", "base_url", "api_key", "model", "network",
	"enabled", "probe_passed", "is_current", "created_at", "updated_at",
}

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Save(ctx context.Context, cfg ModelConfig) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO model_provider_configs (
	id, provider_type, base_url, api_key, model, network,
	enabled, probe_passed, is_current, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE SET
	provider_type = EXCLUDED.provider_type,
	base_url = EXCLUDED.base_url,
	api_key = EXCLUDED.api_key,
	model = EXCLUDED.model,
	network = EXCLUDED.network,
	enabled = EXCLUDED.enabled,
	probe_passed = EXCLUDED.probe_passed,
	is_current = EXCLUDED.is_current,
	created_at = EXCLUDED.created_at,
	updated_at = EXCLUDED.updated_at`,
		cfg.ID, string(cfg.Provider), cfg.BaseURL, cfg.APIKey, cfg.Model, string(cfg.Network),
		cfg.Enabled, cfg.ProbePassed, cfg.IsCurrent, cfg.CreatedAt, cfg.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) Get(ctx context.Context, id string) (ModelConfig, error) {
	row := s.db.QueryRowContext(ctx, selectModelConfigSQL("WHERE id = $1"), id)
	return scanModelConfig(row)
}

func (s *PostgresStore) List(ctx context.Context) ([]ModelConfig, error) {
	rows, err := s.db.QueryContext(ctx, selectModelConfigSQL("ORDER BY id"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []ModelConfig
	for rows.Next() {
		cfg, err := scanModelConfig(rows)
		if err != nil {
			return nil, err
		}
		configs = append(configs, cfg)
	}
	return configs, rows.Err()
}

func (s *PostgresStore) Current(ctx context.Context) (ModelConfig, error) {
	row := s.db.QueryRowContext(ctx, selectModelConfigSQL("WHERE is_current = TRUE LIMIT 1"))
	return scanModelConfig(row)
}

func (s *PostgresStore) SetCurrent(ctx context.Context, id string) error {
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
	if _, err := tx.ExecContext(ctx, "UPDATE model_provider_configs SET is_current = FALSE"); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE model_provider_configs SET is_current = TRUE, updated_at = now() WHERE id = $1", id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrConfigNotFound
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
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
INSERT INTO model_provider_config_audits (actor_id, config_id, action, details, created_at)
VALUES ($1, $2, $3, $4, $5)`,
		entry.ActorID, entry.ConfigID, string(entry.Action), string(encoded), entry.At,
	)
	return err
}

func selectModelConfigSQL(suffix string) string {
	query := "SELECT " + strings.Join(modelConfigSelectColumns, ", ") + " FROM model_provider_configs"
	if suffix != "" {
		query += " " + suffix
	}
	return query
}

func modelConfigColumns() []string {
	out := make([]string, len(modelConfigSelectColumns))
	copy(out, modelConfigSelectColumns)
	return out
}

type modelConfigScanner interface {
	Scan(dest ...any) error
}

func scanModelConfig(scanner modelConfigScanner) (ModelConfig, error) {
	var cfg ModelConfig
	var provider, network string
	err := scanner.Scan(
		&cfg.ID, &provider, &cfg.BaseURL, &cfg.APIKey, &cfg.Model, &network,
		&cfg.Enabled, &cfg.ProbePassed, &cfg.IsCurrent, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelConfig{}, ErrConfigNotFound
	}
	if err != nil {
		return ModelConfig{}, err
	}
	cfg.Provider = Provider(provider)
	cfg.Network = llm.Network(network)
	return cfg, nil
}
