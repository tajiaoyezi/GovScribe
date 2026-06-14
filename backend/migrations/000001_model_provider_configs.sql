CREATE TABLE IF NOT EXISTS model_provider_configs (
    id TEXT PRIMARY KEY,
    provider_type TEXT NOT NULL CHECK (provider_type IN ('openai', 'anthropic', 'openai_compatible')),
    base_url TEXT NOT NULL,
    api_key TEXT NOT NULL,
    model TEXT NOT NULL,
    network TEXT NOT NULL CHECK (network IN ('public', 'private')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    probe_passed BOOLEAN NOT NULL DEFAULT FALSE,
    is_current BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS model_provider_configs_single_current_idx
    ON model_provider_configs (is_current)
    WHERE is_current;

CREATE TABLE IF NOT EXISTS model_provider_config_audits (
    id BIGSERIAL PRIMARY KEY,
    actor_id TEXT NOT NULL,
    config_id TEXT NOT NULL REFERENCES model_provider_configs(id),
    action TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
