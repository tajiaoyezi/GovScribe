package draft

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
			"model_endpoint_evidence_ref", "content_security_level", "first_token_ms", "total_generation_ms",
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

func TestC05CalibrationEvidenceRejectsSyntheticTargetModelSignals(t *testing.T) {
	rejected := []string{
		"fake-target-model",
		"mocked-target-model",
		"mock-calibration-endpoint",
		"local-model-endpoint",
		"dev-server-calibration-run",
		"dummy-calibration-model",
		"httptest-calibration-run",
		"http://localhost:8080/v1",
		"http://127.0.0.1:9000/v1",
		"unit-test-model",
	}
	for _, value := range rejected {
		if !looksSyntheticPoCEvidence(value) {
			t.Fatalf("value %q should be treated as synthetic target model evidence", value)
		}
	}

	allowed := []string{
		"openai-compatible-prod-gateway",
		"deployment-evidence:target-model-gateway-20260620",
		"model-run-audit:calibration-20260620-001",
	}
	for _, value := range allowed {
		if looksSyntheticPoCEvidence(value) {
			t.Fatalf("value %q should be allowed as non-synthetic target model evidence", value)
		}
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

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		doctype := row[index["doctype"]]
		if _, ok := wantDoctypes[doctype]; !ok {
			t.Fatalf("unexpected doctype %q in calibration candidates", doctype)
		}
		wantDoctypes[doctype] = true
		candidateBatch := requiredCell(t, row, index, "candidate_batch", rowNumber)
		requiredCell(t, row, index, "notes", rowNumber)
		if strings.Contains(candidateBatch, "各类文件") {
			t.Fatalf("candidate_batch for %s exposes local raw corpus directory: %q", doctype, candidateBatch)
		}
		rawPackageCount, err := strconv.Atoi(row[index["raw_package_count"]])
		if err != nil {
			t.Fatalf("raw_package_count for %s = %q, want integer: %v", doctype, row[index["raw_package_count"]], err)
		}
		readablePackageCount, err := strconv.Atoi(row[index["readable_package_count"]])
		if err != nil {
			t.Fatalf("readable_package_count for %s = %q, want integer: %v", doctype, row[index["readable_package_count"]], err)
		}
		if rawPackageCount < 0 || readablePackageCount < 0 {
			t.Fatalf("candidate counts for %s must be non-negative, got raw=%d readable=%d", doctype, rawPackageCount, readablePackageCount)
		}
		if readablePackageCount > rawPackageCount {
			t.Fatalf("readable_package_count for %s = %d, want <= raw_package_count %d", doctype, readablePackageCount, rawPackageCount)
		}
		status := row[index["gate_status"]]
		if !allowedGateStatus[status] {
			t.Fatalf("gate_status for %s = %q, want one of known statuses", doctype, status)
		}
		if got := row[index["c03_retrievable_count"]]; got != "pending" {
			if _, err := strconv.Atoi(got); err != nil {
				t.Fatalf("c03_retrievable_count for %s = %q, want pending or integer: %v", doctype, got, err)
			}
		}
		if status == "ready_for_model_run" {
			retrievableCount, err := strconv.Atoi(row[index["c03_retrievable_count"]])
			if err != nil || retrievableCount <= 0 {
				t.Fatalf("gate_status ready_for_model_run for %s requires positive c03_retrievable_count, got %q", doctype, row[index["c03_retrievable_count"]])
			}
		}
		if rawPackageCount == 0 && status != "pending_corpus" && status != "insufficient" {
			t.Fatalf("gate_status for %s = %q with zero raw candidates, want pending_corpus or insufficient", doctype, status)
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
		for _, field := range []string{"subtype", "source_sample_id", "c03_query_id", "prompt_variant_id", "contract_version", "model_provider", "model_name", "model_backend", "model_endpoint_evidence_ref"} {
			requiredCell(t, row, index, field, rowNumber)
		}
		for _, field := range []string{"model_provider", "model_name", "model_backend", "model_endpoint_evidence_ref"} {
			requireNoSyntheticPoCEvidence(t, row[index[field]], "calibration runs", field, rowNumber)
		}
		for _, field := range []string{"topk", "prompt_total_chars", "prompt_token_estimate"} {
			requirePositiveIntCell(t, row, index, field, rowNumber)
		}
		if c03QueryID := row[index["c03_query_id"]]; strings.EqualFold(c03QueryID, "pending") || strings.Contains(c03QueryID, "各类文件") {
			t.Fatalf("calibration runs row %d c03_query_id = %q, want c03 retrieval evidence, not local/raw corpus state", rowNumber, c03QueryID)
		} else {
			requireNoSyntheticPoCEvidence(t, c03QueryID, "calibration runs", "c03_query_id", rowNumber)
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
	runs := readCalibrationRunEvidence(t)
	reviews := readCalibrationReviewEvidence(t)
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
			selectedTopK := requirePositiveIntCell(t, row, index, "selected_topk", rowNumber)
			selectedPromptTotalChars := requirePositiveIntCell(t, row, index, "selected_prompt_total_chars", rowNumber)
			selectedContractVersion := requiredCell(t, row, index, "selected_contract_version", rowNumber)
			runCount := requirePositiveIntCell(t, row, index, "run_count", rowNumber)
			adoptionRate := requireRatioCell(t, row, index, "adoption_rate", rowNumber)
			medianFirstTokenMS := requirePositiveIntCell(t, row, index, "median_first_token_ms", rowNumber)
			p95TotalGenerationMS := requirePositiveIntCell(t, row, index, "p95_total_generation_ms", rowNumber)
			evidenceRefs := parseCalibrationEvidenceRefs(t, requiredCell(t, row, index, "evidence_refs", rowNumber), rowNumber)

			if len(evidenceRefs.runIDs) == 0 || len(evidenceRefs.reviewRecordIDs) == 0 {
				t.Fatalf("calibration decisions row %d evidence_refs must include at least one run:<id> and one review:<id>", rowNumber)
			}

			referencedRuns := make([]calibrationRunEvidence, 0, len(evidenceRefs.runIDs))
			referencedRunIDs := map[string]bool{}
			for _, runID := range evidenceRefs.runIDs {
				run, ok := runs[runID]
				if !ok {
					t.Fatalf("calibration decisions row %d references unknown run_id %q", rowNumber, runID)
				}
				if referencedRunIDs[runID] {
					continue
				}
				referencedRunIDs[runID] = true
				if run.doctype != doctype {
					t.Fatalf("calibration decisions row %d references run %q for doctype %q, want %q", rowNumber, runID, run.doctype, doctype)
				}
				if subtype := strings.TrimSpace(row[index["subtype"]]); subtype != "" && run.subtype != subtype {
					t.Fatalf("calibration decisions row %d references run %q for subtype %q, want %q", rowNumber, runID, run.subtype, subtype)
				}
				if !run.streamCompleted {
					t.Fatalf("calibration decisions row %d references incomplete run %q for pass/fail decision", rowNumber, runID)
				}
				if run.topK != selectedTopK || run.promptTotalChars != selectedPromptTotalChars || run.contractVersion != selectedContractVersion {
					t.Fatalf(
						"calibration decisions row %d references run %q with topk/chars/version = %d/%d/%q, want selected %d/%d/%q",
						rowNumber, runID, run.topK, run.promptTotalChars, run.contractVersion,
						selectedTopK, selectedPromptTotalChars, selectedContractVersion,
					)
				}
				referencedRuns = append(referencedRuns, run)
			}
			if runCount != len(referencedRuns) {
				t.Fatalf("calibration decisions row %d run_count = %d, want %d referenced unique runs", rowNumber, runCount, len(referencedRuns))
			}
			if gotMedian := medianInt(referencedRuns, func(run calibrationRunEvidence) int { return run.firstTokenMS }); medianFirstTokenMS != gotMedian {
				t.Fatalf("calibration decisions row %d median_first_token_ms = %d, want %d from referenced runs", rowNumber, medianFirstTokenMS, gotMedian)
			}
			if gotP95 := p95Int(referencedRuns, func(run calibrationRunEvidence) int { return run.totalGenerationMS }); p95TotalGenerationMS != gotP95 {
				t.Fatalf("calibration decisions row %d p95_total_generation_ms = %d, want %d from referenced runs", rowNumber, p95TotalGenerationMS, gotP95)
			}

			referencedReviews := make([]calibrationReviewEvidence, 0, len(evidenceRefs.reviewRecordIDs))
			reviewedRunIDs := map[string]bool{}
			for _, reviewRecordID := range evidenceRefs.reviewRecordIDs {
				review, ok := reviews[reviewRecordID]
				if !ok {
					t.Fatalf("calibration decisions row %d references unknown review_record_id %q", rowNumber, reviewRecordID)
				}
				if !referencedRunIDs[review.runID] {
					t.Fatalf("calibration decisions row %d references review %q for run %q not present in evidence_refs", rowNumber, reviewRecordID, review.runID)
				}
				reviewedRunIDs[review.runID] = true
				referencedReviews = append(referencedReviews, review)
			}
			for runID := range referencedRunIDs {
				if !reviewedRunIDs[runID] {
					t.Fatalf("calibration decisions row %d references run %q without a corresponding review evidence ref", rowNumber, runID)
				}
			}
			if gotAdoptionRate := adoptionRateFromReviews(referencedReviews); math.Abs(adoptionRate-gotAdoptionRate) > 0.005 {
				t.Fatalf("calibration decisions row %d adoption_rate = %.4f, want %.4f from referenced reviews", rowNumber, adoptionRate, gotAdoptionRate)
			}
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
		c05RawCorpusReferencePattern,
		regexp.MustCompile(`正文-`),
	}
	rawOfficeExtension := regexp.MustCompile(`(?i)\.(docx?|pdf|xlsx|et)\b`)
	fieldsAllowingSanitizedObjectExtensions := map[string]bool{
		"output_ref":                  true,
		"evidence_refs":               true,
		"model_endpoint_evidence_ref": true,
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

type calibrationRunEvidence struct {
	id                string
	doctype           string
	subtype           string
	topK              int
	promptTotalChars  int
	contractVersion   string
	firstTokenMS      int
	totalGenerationMS int
	streamCompleted   bool
}

type calibrationReviewEvidence struct {
	id              string
	runID           string
	countsAsAdopted bool
}

type calibrationEvidenceRefs struct {
	runIDs          []string
	reviewRecordIDs []string
}

func readCalibrationRunEvidence(t *testing.T) map[string]calibrationRunEvidence {
	t.Helper()
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-runs.csv")
	index := csvIndex(header)
	runs := map[string]calibrationRunEvidence{}
	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		runID := strings.TrimSpace(row[index["run_id"]])
		if runID == "" {
			continue
		}
		topK, _ := strconv.Atoi(row[index["topk"]])
		promptTotalChars, _ := strconv.Atoi(row[index["prompt_total_chars"]])
		firstTokenMS, _ := strconv.Atoi(row[index["first_token_ms"]])
		totalGenerationMS, _ := strconv.Atoi(row[index["total_generation_ms"]])
		streamCompleted, err := strconv.ParseBool(row[index["stream_completed"]])
		if err != nil {
			t.Fatalf("calibration runs row %d stream_completed = %q, want boolean: %v", rowNumber, row[index["stream_completed"]], err)
		}
		runs[runID] = calibrationRunEvidence{
			id:                runID,
			doctype:           strings.TrimSpace(row[index["doctype"]]),
			subtype:           strings.TrimSpace(row[index["subtype"]]),
			topK:              topK,
			promptTotalChars:  promptTotalChars,
			contractVersion:   strings.TrimSpace(row[index["contract_version"]]),
			firstTokenMS:      firstTokenMS,
			totalGenerationMS: totalGenerationMS,
			streamCompleted:   streamCompleted,
		}
	}
	return runs
}

func readCalibrationReviewEvidence(t *testing.T) map[string]calibrationReviewEvidence {
	t.Helper()
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-reviews.csv")
	index := csvIndex(header)
	reviews := map[string]calibrationReviewEvidence{}
	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		reviewRecordID := strings.TrimSpace(row[index["review_record_id"]])
		if reviewRecordID == "" {
			continue
		}
		countsAsAdopted, err := strconv.ParseBool(row[index["counts_as_adopted"]])
		if err != nil {
			t.Fatalf("calibration reviews row %d counts_as_adopted = %q, want boolean: %v", rowNumber, row[index["counts_as_adopted"]], err)
		}
		reviews[reviewRecordID] = calibrationReviewEvidence{
			id:              reviewRecordID,
			runID:           strings.TrimSpace(row[index["run_id"]]),
			countsAsAdopted: countsAsAdopted,
		}
	}
	return reviews
}

func parseCalibrationEvidenceRefs(t *testing.T, value string, rowNumber int) calibrationEvidenceRefs {
	t.Helper()
	refs := calibrationEvidenceRefs{}
	seenRunIDs := map[string]bool{}
	seenReviewRecordIDs := map[string]bool{}
	for _, token := range strings.Split(value, ";") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		switch {
		case strings.HasPrefix(token, "run:"):
			runID := strings.TrimSpace(strings.TrimPrefix(token, "run:"))
			if runID == "" {
				t.Fatalf("calibration decisions row %d has empty run evidence ref in %q", rowNumber, value)
			}
			if seenRunIDs[runID] {
				t.Fatalf("calibration decisions row %d duplicates run evidence ref %q", rowNumber, runID)
			}
			seenRunIDs[runID] = true
			refs.runIDs = append(refs.runIDs, runID)
		case strings.HasPrefix(token, "review:"):
			reviewRecordID := strings.TrimSpace(strings.TrimPrefix(token, "review:"))
			if reviewRecordID == "" {
				t.Fatalf("calibration decisions row %d has empty review evidence ref in %q", rowNumber, value)
			}
			if seenReviewRecordIDs[reviewRecordID] {
				t.Fatalf("calibration decisions row %d duplicates review evidence ref %q", rowNumber, reviewRecordID)
			}
			seenReviewRecordIDs[reviewRecordID] = true
			refs.reviewRecordIDs = append(refs.reviewRecordIDs, reviewRecordID)
		default:
			t.Fatalf("calibration decisions row %d evidence_refs token %q must use run:<id> or review:<id>", rowNumber, token)
		}
	}
	return refs
}

func medianInt(runs []calibrationRunEvidence, pick func(calibrationRunEvidence) int) int {
	values := make([]int, 0, len(runs))
	for _, run := range runs {
		values = append(values, pick(run))
	}
	sort.Ints(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return int(math.Round(float64(values[mid-1]+values[mid]) / 2))
}

func p95Int(runs []calibrationRunEvidence, pick func(calibrationRunEvidence) int) int {
	values := make([]int, 0, len(runs))
	for _, run := range runs {
		values = append(values, pick(run))
	}
	sort.Ints(values)
	index := int(math.Ceil(float64(len(values))*0.95)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func adoptionRateFromReviews(reviews []calibrationReviewEvidence) float64 {
	if len(reviews) == 0 {
		return 0
	}
	adopted := 0
	for _, review := range reviews {
		if review.countsAsAdopted {
			adopted++
		}
	}
	return float64(adopted) / float64(len(reviews))
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
