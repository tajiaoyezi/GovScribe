package draft

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestC05CalibrationCSVHeadersStayAuditable(t *testing.T) {
	tests := map[string][]string{
		"c05-high-freq-doctype-calibration-candidates.csv": {
			"doctype", "candidate_batch", "raw_package_count", "readable_package_count",
			"c03_retrievable_count", "gate_status", "notes",
		},
		"c05-high-freq-doctype-calibration-runs.csv": {
			"run_id", "run_date", "doctype", "subtype", "source_sample_id", "c03_query_id",
			"prompt_variant_id", "topk", "prompt_total_chars", "prompt_token_estimate",
			"contract_version", "model_provider", "model_name", "model_backend",
			"content_security_level", "first_token_ms", "total_generation_ms",
			"completion_chars", "stream_completed", "error_reason", "output_ref",
			"review_record_id", "notes",
		},
		"c05-high-freq-doctype-calibration-reviews.csv": {
			"review_record_id", "run_id", "reviewer", "review_date", "doctype_norm_score",
			"structure_score", "direction_score", "organ_tone_score", "adoption_status",
			"counts_as_adopted", "revision_effort_minutes", "notes",
		},
		"c05-high-freq-doctype-calibration-decisions.csv": {
			"decision_id", "decision_date", "doctype", "subtype", "selected_topk",
			"selected_prompt_total_chars", "selected_contract_version", "run_count",
			"adoption_rate", "median_first_token_ms", "p95_total_generation_ms",
			"pass_fail", "decision_owner", "evidence_refs", "notes",
		},
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			header, _ := readCalibrationCSV(t, name)
			if got := strings.Join(header, ","); got != strings.Join(want, ",") {
				t.Fatalf("header = %q, want %q", got, strings.Join(want, ","))
			}
		})
	}
}

func TestC05CalibrationCandidatesCoverAllHighFrequencyDoctypes(t *testing.T) {
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-candidates.csv")
	if len(rows) != 9 {
		t.Fatalf("candidate rows = %d, want 9 high-frequency doctypes", len(rows))
	}

	index := csvIndex(header)
	wantDoctypes := map[string]bool{
		"通知": false, "请示": false, "报告": false, "函": false, "会议纪要": false,
		"通报": false, "批复": false, "讲话稿": false, "方案": false,
	}
	allowedGateStatus := map[string]bool{
		"pending_corpus": true, "pending_desensitization": true, "pending_c03": true,
		"ready_for_model_run": true, "insufficient": true,
	}

	for _, row := range rows {
		doctype := row[index["doctype"]]
		if _, ok := wantDoctypes[doctype]; !ok {
			t.Fatalf("unexpected doctype %q in calibration candidates", doctype)
		}
		wantDoctypes[doctype] = true
		for _, field := range []string{"raw_package_count", "readable_package_count"} {
			if _, err := strconv.Atoi(row[index[field]]); err != nil {
				t.Fatalf("%s for %s = %q, want integer: %v", field, doctype, row[index[field]], err)
			}
		}
		if status := row[index["gate_status"]]; !allowedGateStatus[status] {
			t.Fatalf("gate_status for %s = %q, want one of known statuses", doctype, status)
		}
		if got := row[index["c03_retrievable_count"]]; got != "pending" {
			if _, err := strconv.Atoi(got); err != nil {
				t.Fatalf("c03_retrievable_count for %s = %q, want pending or integer: %v", doctype, got, err)
			}
		}
	}
	for doctype, seen := range wantDoctypes {
		if !seen {
			t.Fatalf("missing c05 calibration candidate row for %s", doctype)
		}
	}
}

func TestC05CalibrationCSVsDoNotExposeRawCorpusArtifacts(t *testing.T) {
	files := []string{
		"c05-high-freq-doctype-calibration-candidates.csv",
		"c05-high-freq-doctype-calibration-runs.csv",
		"c05-high-freq-doctype-calibration-reviews.csv",
		"c05-high-freq-doctype-calibration-decisions.csv",
	}
	forbiddenEverywhere := []*regexp.Regexp{
		regexp.MustCompile(`(?i)[a-z]:[\\/]`),
		regexp.MustCompile(`^(?://|\\\\)`),
		regexp.MustCompile(`(^|[\\/])各类文件[\\/]`),
		regexp.MustCompile(`正文-`),
	}
	rawOfficeExtension := regexp.MustCompile(`(?i)\.(docx?|pdf|xlsx|et)\b`)
	fieldsAllowingSanitizedObjectExtensions := map[string]bool{
		"output_ref":    true,
		"evidence_refs": true,
	}

	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			header, rows := readCalibrationCSV(t, name)
			for rowIndex, row := range append([][]string{header}, rows...) {
				for colIndex, cell := range row {
					for _, pattern := range forbiddenEverywhere {
						if pattern.MatchString(cell) {
							t.Fatalf("%s row %d col %d contains raw corpus artifact reference %q", name, rowIndex, colIndex, cell)
						}
					}
					field := ""
					if colIndex < len(header) {
						field = header[colIndex]
					}
					if rawOfficeExtension.MatchString(cell) && !fieldsAllowingSanitizedObjectExtensions[field] {
						t.Fatalf("%s row %d field %q contains office/PDF extension outside sanitized reference fields: %q", name, rowIndex, field, cell)
					}
				}
			}
		})
	}
}

func readCalibrationCSV(t *testing.T, name string) ([]string, [][]string) {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "other", name)
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("read csv %s: %v", path, err)
	}
	if len(records) == 0 {
		t.Fatalf("csv %s is empty", path)
	}
	return records[0], records[1:]
}

func csvIndex(header []string) map[string]int {
	index := make(map[string]int, len(header))
	for i, field := range header {
		index[field] = i
	}
	return index
}
