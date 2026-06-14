package config

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestPostgresStoreSavesAndReadsConfig(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	now := time.Unix(1700000000, 0).UTC()
	cfg := ModelConfig{
		ID: "cfg-1", Provider: ProviderOpenAICompatible, BaseURL: "https://private.example/v1",
		APIKey: "sk-private", Model: "model-a", Network: llm.NetworkPrivate,
		Enabled: true, ProbePassed: true, IsCurrent: true, CreatedAt: now, UpdatedAt: now,
	}

	mock.ExpectExec("INSERT INTO model_provider_configs").
		WithArgs(cfg.ID, string(cfg.Provider), cfg.BaseURL, cfg.APIKey, cfg.Model, string(cfg.Network), cfg.Enabled, cfg.ProbePassed, cfg.IsCurrent, cfg.CreatedAt, cfg.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.Save(context.Background(), cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	rows := sqlmock.NewRows(modelConfigColumns()).AddRow(
		cfg.ID, string(cfg.Provider), cfg.BaseURL, cfg.APIKey, cfg.Model, string(cfg.Network),
		cfg.Enabled, cfg.ProbePassed, cfg.IsCurrent, cfg.CreatedAt, cfg.UpdatedAt,
	)
	mock.ExpectQuery("SELECT id, provider_type, base_url, api_key, model, network, enabled, probe_passed, is_current, created_at, updated_at FROM model_provider_configs WHERE id = \\$1").
		WithArgs(cfg.ID).
		WillReturnRows(rows)
	got, err := store.Get(context.Background(), cfg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != cfg {
		t.Fatalf("config = %#v, want %#v", got, cfg)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreSwitchesCurrentInTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE model_provider_configs SET is_current = FALSE").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE model_provider_configs SET is_current = TRUE, updated_at = now\\(\\) WHERE id = \\$1").
		WithArgs("cfg-current").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := store.SetCurrent(context.Background(), "cfg-current"); err != nil {
		t.Fatalf("set current: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreWritesAuditDetailsAsJSON(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	now := time.Unix(1700000000, 0).UTC()
	mock.ExpectExec("INSERT INTO model_provider_config_audits").
		WithArgs("admin-1", "cfg-1", string(AuditActionProbe), `{"available":"false","error_reason":"timeout"}`, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = store.AppendAudit(context.Background(), AuditEntry{
		ActorID: "admin-1", ConfigID: "cfg-1", Action: AuditActionProbe, At: now,
		Details: map[string]string{"available": "false", "error_reason": "timeout"},
	})
	if err != nil {
		t.Fatalf("append audit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
