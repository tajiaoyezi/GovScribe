CREATE TABLE IF NOT EXISTS corpus_documents (
    document_id TEXT PRIMARY KEY,
    source_object_key TEXT NOT NULL,
    classification TEXT NOT NULL,
    document_type TEXT NOT NULL,
    document_number TEXT,
    organization_name TEXT,
    original_filename TEXT,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS corpus_documents_acl_lookup_idx
    ON corpus_documents (classification, document_type, is_deleted);

CREATE INDEX IF NOT EXISTS corpus_documents_exact_recall_idx
    ON corpus_documents (document_number, organization_name)
    WHERE is_deleted = FALSE;

CREATE TABLE IF NOT EXISTS corpus_chunks (
    chunk_id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL REFERENCES corpus_documents(document_id),
    chunk_index INTEGER NOT NULL,
    content_text TEXT NOT NULL,
    classification TEXT NOT NULL,
    document_type TEXT NOT NULL,
    document_number TEXT,
    organization_name TEXT,
    embedding_model TEXT,
    embedding_dimensions INTEGER,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    milvus_indexed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS corpus_chunks_acl_lookup_idx
    ON corpus_chunks (classification, document_type, is_deleted);

CREATE INDEX IF NOT EXISTS corpus_chunks_exact_recall_idx
    ON corpus_chunks (document_number, organization_name)
    WHERE is_deleted = FALSE;

CREATE TABLE IF NOT EXISTS corpus_adoption_feedback (
    feedback_id BIGSERIAL PRIMARY KEY,
    generated_task_id TEXT NOT NULL,
    adoption_action_id TEXT NOT NULL,
    source_object_key TEXT NOT NULL,
    source_chunk_id TEXT,
    adoption_decision TEXT NOT NULL CHECK (adoption_decision IN (
        'direct_use',
        'minor_edit',
        'major_edit',
        'discarded'
    )),
    classification TEXT NOT NULL,
    document_type TEXT NOT NULL,
    ingested_document_id TEXT REFERENCES corpus_documents(document_id),
    status TEXT NOT NULL CHECK (status IN ('pending', 'ingested', 'blocked', 'failed')),
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS corpus_adoption_feedback_source_idx
    ON corpus_adoption_feedback (generated_task_id, adoption_action_id);

CREATE TABLE IF NOT EXISTS corpus_outbox_events (
    event_id BIGSERIAL PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    chunk_id TEXT REFERENCES corpus_chunks(chunk_id),
    outbox_event_type TEXT NOT NULL CHECK (outbox_event_type IN (
        'index_chunk',
        'soft_delete_chunk',
        'rebuild_chunk',
        'adoption_ingest'
    )),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT corpus_outbox_adoption_payload_contract_chk CHECK (
        outbox_event_type <> 'adoption_ingest'
        OR (
            jsonb_typeof(payload) = 'object'
            AND NOT (payload ? 'content')
            AND payload ? 'generated_task_id'
            AND payload ? 'adoption_action_id'
            AND payload ? 'source_object_key'
            AND payload ? 'decision'
            AND payload ? 'classification'
            AND payload ? 'document_type'
            AND (payload - ARRAY[
                'generated_task_id',
                'adoption_action_id',
                'source_object_key',
                'source_chunk_id',
                'decision',
                'classification',
                'document_type'
            ]::text[]) = '{}'::jsonb
        )
    ),
    status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'succeeded', 'failed')) DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS corpus_outbox_events_pending_idx
    ON corpus_outbox_events (status, next_retry_at, event_id)
    WHERE status IN ('pending', 'failed');
