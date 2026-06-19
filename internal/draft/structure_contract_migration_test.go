package draft

import (
	"os"
	"strings"
	"testing"
)

func TestMigrationDefinesHighFreqStructureContractsWithoutC06ClassificationFields(t *testing.T) {
	content, err := os.ReadFile("../../backend/migrations/000007_high_freq_doctype_contracts.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(content)
	required := []string{
		"CREATE TABLE IF NOT EXISTS high_freq_doctype_structure_contracts",
		"doctype TEXT NOT NULL",
		"direction TEXT NOT NULL",
		"CHECK (direction IN ('', 'upward', 'downward', 'horizontal'))",
		"title_rule TEXT NOT NULL",
		"salutation_rule TEXT NOT NULL",
		"recipient_rule TEXT NOT NULL",
		"body_structure JSONB NOT NULL",
		"required_slots JSONB NOT NULL",
		"closing_rule TEXT NOT NULL",
		"signature_rule TEXT NOT NULL",
		"tone_rules JSONB NOT NULL",
		"redline_rules JSONB NOT NULL",
		"template_object_key TEXT NOT NULL DEFAULT ''",
		"template_version TEXT NOT NULL DEFAULT ''",
		"PRIMARY KEY (doctype, direction)",
	}
	for _, want := range required {
		if !strings.Contains(sql, want) {
			t.Fatalf("migration missing %q", want)
		}
	}
	forbidden := []string{"capability_tier", "is_starred_rare", "target_capability"}
	for _, bad := range forbidden {
		if strings.Contains(sql, bad) {
			t.Fatalf("migration must not carry c06 classification field %q", bad)
		}
	}
}
