package corpus

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationDefinesCorpusAuthorityTablesAndIndexes(t *testing.T) {
	content, err := os.ReadFile("../../../backend/migrations/000003_corpus_rag_retrieval.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(content)
	required := []string{
		"CREATE TABLE IF NOT EXISTS corpus_documents",
		"CREATE TABLE IF NOT EXISTS corpus_chunks",
		"CREATE TABLE IF NOT EXISTS corpus_adoption_feedback",
		"CREATE TABLE IF NOT EXISTS corpus_outbox_events",
		"chunk_id TEXT PRIMARY KEY",
		"classification TEXT NOT NULL",
		"document_type TEXT NOT NULL",
		"document_number TEXT",
		"organization_name TEXT",
		"is_deleted BOOLEAN NOT NULL DEFAULT FALSE",
		"created_at TIMESTAMPTZ NOT NULL DEFAULT now()",
		"updated_at TIMESTAMPTZ NOT NULL DEFAULT now()",
		"outbox_event_type TEXT NOT NULL CHECK",
		"corpus_outbox_adoption_payload_contract_chk",
		"NOT (payload ? 'content')",
		"payload ? 'source_object_key'",
		"payload ? 'decision'",
		"retry_count INTEGER NOT NULL DEFAULT 0",
		"corpus_chunks_acl_lookup_idx",
		"corpus_chunks_exact_recall_idx",
		"corpus_outbox_events_pending_idx",
	}
	for _, want := range required {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
