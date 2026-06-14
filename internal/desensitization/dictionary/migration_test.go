package dictionary

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationDefinesDictionaryRoutingAndAuditTables(t *testing.T) {
	content, err := os.ReadFile("../../../backend/migrations/000002_desensitization_gateway.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(content)
	required := []string{
		"CREATE TABLE IF NOT EXISTS desensitization_dictionary_entries",
		"CREATE TABLE IF NOT EXISTS security_classification_routes",
		"CREATE TABLE IF NOT EXISTS desensitization_audit_logs",
		"allow_degraded_public BOOLEAN NOT NULL DEFAULT FALSE",
		"entry_type TEXT NOT NULL CHECK",
		"deleted BOOLEAN NOT NULL DEFAULT FALSE",
	}
	for _, want := range required {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
}
