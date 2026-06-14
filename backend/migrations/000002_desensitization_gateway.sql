CREATE TABLE IF NOT EXISTS desensitization_dictionary_entries (
    id TEXT PRIMARY KEY,
    entry_text TEXT NOT NULL,
    entry_type TEXT NOT NULL CHECK (entry_type IN (
        'organization',
        'person',
        'project_code',
        'secret_keyword_blacklist'
    )),
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS desensitization_dictionary_entries_active_idx
    ON desensitization_dictionary_entries (entry_type, entry_text)
    WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS security_classification_routes (
    classification TEXT PRIMARY KEY CHECK (classification IN (
        'unclassified',
        'sensitive',
        'classified',
        'unknown'
    )),
    target_network TEXT NOT NULL CHECK (target_network IN ('public', 'private')),
    model_config_id TEXT REFERENCES model_provider_configs(id),
    allow_degraded_public BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS desensitization_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    actor_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    content_classification TEXT NOT NULL,
    original_diff JSONB NOT NULL DEFAULT '{}'::jsonb,
    match_details JSONB NOT NULL DEFAULT '[]'::jsonb,
    disposition_event TEXT NOT NULL,
    disposition_reason TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
