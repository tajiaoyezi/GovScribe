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
	wantDoctypes := c05HighFreqDoctypeSeen()
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

func TestC05CalibrationRunRowsStayTraceableWhenPresent(t *testing.T) {
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-runs.csv")
	index := csvIndex(header)
	seenRunIDs := map[string]bool{}
	allowedSecurityLevels := map[string]bool{"非密": true, "敏感": true, "涉密": true}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		runID := requiredCell(t, row, index, "run_id", rowNumber)
		if seenRunIDs[runID] {
			t.Fatalf("duplicate run_id %q in calibration runs", runID)
		}
		seenRunIDs[runID] = true

		requireISODateCell(t, row, index, "run_date", rowNumber)
		requireKnownC05Doctype(t, requiredCell(t, row, index, "doctype", rowNumber), "calibration runs", rowNumber)
		for _, field := range []string{"subtype", "source_sample_id", "c03_query_id", "prompt_variant_id", "contract_version", "model_provider", "model_name", "model_backend"} {
			requiredCell(t, row, index, field, rowNumber)
		}
		for _, field := range []string{"topk", "prompt_total_chars", "prompt_token_estimate"} {
			requirePositiveIntCell(t, row, index, field, rowNumber)
		}
		if c03QueryID := row[index["c03_query_id"]]; strings.EqualFold(c03QueryID, "pending") || strings.Contains(c03QueryID, "各类文件") {
			t.Fatalf("calibration runs row %d c03_query_id = %q, want c03 retrieval evidence, not local/raw corpus state", rowNumber, c03QueryID)
		}
		if securityLevel := row[index["content_security_level"]]; !allowedSecurityLevels[securityLevel] {
			t.Fatalf("calibration runs row %d content_security_level = %q, want 非密/敏感/涉密", rowNumber, securityLevel)
		}

		streamCompleted, err := strconv.ParseBool(row[index["stream_completed"]])
		if err != nil {
			t.Fatalf("calibration runs row %d stream_completed = %q, want boolean: %v", rowNumber, row[index["stream_completed"]], err)
		}
		if streamCompleted {
			requiredCell(t, row, index, "output_ref", rowNumber)
			for _, field := range []string{"first_token_ms", "total_generation_ms", "completion_chars"} {
				requirePositiveIntCell(t, row, index, field, rowNumber)
			}
			if errorReason := strings.TrimSpace(row[index["error_reason"]]); errorReason != "" {
				t.Fatalf("calibration runs row %d stream_completed=true but error_reason = %q", rowNumber, errorReason)
			}
		} else {
			requiredCell(t, row, index, "error_reason", rowNumber)
			for _, field := range []string{"first_token_ms", "total_generation_ms", "completion_chars"} {
				requireNonNegativeIntCell(t, row, index, field, rowNumber)
			}
		}
	}
}

func TestC05CalibrationReviewRowsReferenceRunsAndScoreRubric(t *testing.T) {
	runHeader, runRows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-runs.csv")
	runIndex := csvIndex(runHeader)
	runIDs := map[string]bool{}
	for _, row := range runRows {
		if runID := strings.TrimSpace(row[runIndex["run_id"]]); runID != "" {
			runIDs[runID] = true
		}
	}

	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-reviews.csv")
	index := csvIndex(header)
	seenReviewIDs := map[string]bool{}
	allowedAdoptionStatus := map[string]bool{"直接用": true, "小改": true, "大改": true, "弃用": true}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		reviewID := requiredCell(t, row, index, "review_record_id", rowNumber)
		if seenReviewIDs[reviewID] {
			t.Fatalf("duplicate review_record_id %q in calibration reviews", reviewID)
		}
		seenReviewIDs[reviewID] = true

		runID := requiredCell(t, row, index, "run_id", rowNumber)
		if !runIDs[runID] {
			t.Fatalf("calibration reviews row %d references unknown run_id %q", rowNumber, runID)
		}
		requiredCell(t, row, index, "reviewer", rowNumber)
		requireISODateCell(t, row, index, "review_date", rowNumber)
		for _, field := range []string{"doctype_norm_score", "structure_score", "direction_score", "organ_tone_score"} {
			score := requirePositiveIntCell(t, row, index, field, rowNumber)
			if score > 5 {
				t.Fatalf("calibration reviews row %d %s = %d, want 1-5", rowNumber, field, score)
			}
		}
		adoptionStatus := row[index["adoption_status"]]
		if !allowedAdoptionStatus[adoptionStatus] {
			t.Fatalf("calibration reviews row %d adoption_status = %q, want known adoption status", rowNumber, adoptionStatus)
		}
		countsAsAdopted, err := strconv.ParseBool(row[index["counts_as_adopted"]])
		if err != nil {
			t.Fatalf("calibration reviews row %d counts_as_adopted = %q, want boolean: %v", rowNumber, row[index["counts_as_adopted"]], err)
		}
		wantAdopted := adoptionStatus == "直接用" || adoptionStatus == "小改"
		if countsAsAdopted != wantAdopted {
			t.Fatalf("calibration reviews row %d counts_as_adopted = %v, want %v for adoption_status %q", rowNumber, countsAsAdopted, wantAdopted, adoptionStatus)
		}
		if minutes := requireNonNegativeIntCell(t, row, index, "revision_effort_minutes", rowNumber); minutes < 0 {
			t.Fatalf("calibration reviews row %d revision_effort_minutes = %d, want >= 0", rowNumber, minutes)
		}
	}
}

func TestC05CalibrationDecisionRowsGateCompletionEvidence(t *testing.T) {
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-decisions.csv")
	index := csvIndex(header)
	seenDecisionIDs := map[string]bool{}
	passedDoctypes := map[string]bool{}
	allowedPassFail := map[string]bool{"pass": true, "fail": true, "blocked": true, "insufficient_evidence": true}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		decisionID := requiredCell(t, row, index, "decision_id", rowNumber)
		if seenDecisionIDs[decisionID] {
			t.Fatalf("duplicate decision_id %q in calibration decisions", decisionID)
		}
		seenDecisionIDs[decisionID] = true

		requireISODateCell(t, row, index, "decision_date", rowNumber)
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		requireKnownC05Doctype(t, doctype, "calibration decisions", rowNumber)
		passFail := requiredCell(t, row, index, "pass_fail", rowNumber)
		if !allowedPassFail[passFail] {
			t.Fatalf("calibration decisions row %d pass_fail = %q, want pass/fail/blocked/insufficient_evidence", rowNumber, passFail)
		}
		requiredCell(t, row, index, "decision_owner", rowNumber)
		requiredCell(t, row, index, "notes", rowNumber)

		if passFail == "pass" || passFail == "fail" {
			requirePositiveIntCell(t, row, index, "selected_topk", rowNumber)
			requirePositiveIntCell(t, row, index, "selected_prompt_total_chars", rowNumber)
			requiredCell(t, row, index, "selected_contract_version", rowNumber)
			requirePositiveIntCell(t, row, index, "run_count", rowNumber)
			requireRatioCell(t, row, index, "adoption_rate", rowNumber)
			requirePositiveIntCell(t, row, index, "median_first_token_ms", rowNumber)
			requirePositiveIntCell(t, row, index, "p95_total_generation_ms", rowNumber)
			requiredCell(t, row, index, "evidence_refs", rowNumber)
		}
		if passFail == "pass" {
			passedDoctypes[doctype] = true
		}
	}

	if c05TaskChecked(t, "7.3") {
		for doctype := range c05HighFreqDoctypeSeen() {
			if !passedDoctypes[doctype] {
				t.Fatalf("task 7.3 is checked but calibration decisions have no passing evidence for %s", doctype)
			}
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

func c05HighFreqDoctypeSeen() map[string]bool {
	return map[string]bool{
		"通知": false, "请示": false, "报告": false, "函": false, "会议纪要": false,
		"通报": false, "批复": false, "讲话稿": false, "方案": false,
	}
}

func requireKnownC05Doctype(t *testing.T, doctype, source string, rowNumber int) {
	t.Helper()
	if _, ok := c05HighFreqDoctypeSeen()[doctype]; !ok {
		t.Fatalf("%s row %d doctype = %q, want c05 high-frequency doctype", source, rowNumber, doctype)
	}
}

func requiredCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int) string {
	t.Helper()
	value := strings.TrimSpace(row[index[field]])
	if value == "" {
		t.Fatalf("row %d field %s is required", rowNumber, field)
	}
	return value
}

func requirePositiveIntCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int) int {
	t.Helper()
	parsed := requireNonNegativeIntCell(t, row, index, field, rowNumber)
	if parsed <= 0 {
		t.Fatalf("row %d field %s = %d, want > 0", rowNumber, field, parsed)
	}
	return parsed
}

func requireNonNegativeIntCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int) int {
	t.Helper()
	value := requiredCell(t, row, index, field, rowNumber)
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("row %d field %s = %q, want integer: %v", rowNumber, field, value, err)
	}
	if parsed < 0 {
		t.Fatalf("row %d field %s = %d, want >= 0", rowNumber, field, parsed)
	}
	return parsed
}

func requireRatioCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int) float64 {
	t.Helper()
	value := requiredCell(t, row, index, field, rowNumber)
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("row %d field %s = %q, want number: %v", rowNumber, field, value, err)
	}
	if parsed < 0 || parsed > 1 {
		t.Fatalf("row %d field %s = %f, want 0..1", rowNumber, field, parsed)
	}
	return parsed
}

func requireISODateCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int) {
	t.Helper()
	value := requiredCell(t, row, index, field, rowNumber)
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(value) {
		t.Fatalf("row %d field %s = %q, want YYYY-MM-DD", rowNumber, field, value)
	}
}

func c05TaskChecked(t *testing.T, taskID string) bool {
	t.Helper()
	path := filepath.Join("..", "..", "openspec", "changes", "c05-high-freq-doctypes", "tasks.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	pattern := regexp.MustCompile(`(?m)^- \[(x| )\] ` + regexp.QuoteMeta(taskID) + `\b`)
	match := pattern.FindStringSubmatch(string(content))
	if match == nil {
		t.Fatalf("task %s not found in %s", taskID, path)
	}
	return match[1] == "x"
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
