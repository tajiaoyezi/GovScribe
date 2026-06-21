package draft

import (
	"encoding/csv"
	"fmt"
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
			"desensitized_batch_ref", "c03_query_ref", "c03_retrievable_count", "gate_status", "candidate_gate_code",
		},
		"c05-high-freq-doctype-corpus-intake-readiness.csv": {
			"doctype", "candidate_batch", "source_group_ref", "raw_package_count", "readable_package_count",
			"readable_gap_to_100", "intake_stage", "next_c03_gate", "desensitization_owner",
			"c03_ingestion_owner", "readiness_code",
		},
		"c05-high-freq-doctype-local-package-audit.csv": {
			"doctype", "candidate_batch", "source_group_ref", "raw_package_count",
			"direct_extractable_package_count", "conversion_required_package_count",
			"blocked_package_count", "readable_package_count", "readable_gap_to_100",
			"package_audit_status", "package_audit_code",
		},
		"c05-high-freq-doctype-calibration-runs.csv": {
			"run_id", "run_date", "doctype", "subtype", "source_sample_id", "c03_query_id",
			"prompt_variant_id", "topk", "prompt_total_chars", "prompt_token_estimate",
			"contract_version", "model_provider", "model_name", "model_backend",
			"model_endpoint_evidence_ref", "content_security_level", "first_token_ms", "total_generation_ms",
			"completion_chars", "stream_completed", "error_reason", "output_ref",
			"review_record_id", "notes",
		},
		"c05-high-freq-doctype-calibration-variants.csv": {
			"variant_id", "doctype", "subtype", "topk", "prompt_total_chars",
			"prompt_token_estimate", "contract_version", "wording_version",
			"comparison_group", "comparison_axis", "variant_status", "notes",
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
	allowedCandidateGateCodes := map[string]bool{
		"missing_corpus":           true,
		"awaiting_desensitization": true,
		"awaiting_c03_retrieval":   true,
		"ready_for_model_run":      true,
		"low_sample_count":         true,
	}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		doctype := row[index["doctype"]]
		if _, ok := wantDoctypes[doctype]; !ok {
			t.Fatalf("unexpected doctype %q in calibration candidates", doctype)
		}
		wantDoctypes[doctype] = true
		candidateBatch := requiredCell(t, row, index, "candidate_batch", rowNumber)
		desensitizedBatchRef := requiredCell(t, row, index, "desensitized_batch_ref", rowNumber)
		c03QueryRef := requiredCell(t, row, index, "c03_query_ref", rowNumber)
		candidateGateCode := requiredCell(t, row, index, "candidate_gate_code", rowNumber)
		if !allowedCandidateGateCodes[candidateGateCode] {
			t.Fatalf("candidate_gate_code for %s = %q, want controlled gate code", doctype, candidateGateCode)
		}
		if c05RawCorpusReferencePattern.MatchString(candidateBatch) {
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
		wantGateCode := c05CandidateGateCodeFor(status, rawPackageCount, readablePackageCount)
		if candidateGateCode != wantGateCode {
			t.Fatalf("candidate_gate_code for %s = %q, want %q for gate_status=%s raw/readable=%d/%d", doctype, candidateGateCode, wantGateCode, status, rawPackageCount, readablePackageCount)
		}
		if got := row[index["c03_retrievable_count"]]; got != "pending" {
			if _, err := strconv.Atoi(got); err != nil {
				t.Fatalf("c03_retrievable_count for %s = %q, want pending or integer: %v", doctype, got, err)
			}
		}
		if status == "ready_for_model_run" {
			if strings.EqualFold(desensitizedBatchRef, "pending") {
				t.Fatalf("gate_status ready_for_model_run for %s requires desensitized_batch_ref, got pending", doctype)
			}
			if strings.EqualFold(c03QueryRef, "pending") {
				t.Fatalf("gate_status ready_for_model_run for %s requires c03_query_ref, got pending", doctype)
			}
			retrievableCount, err := strconv.Atoi(row[index["c03_retrievable_count"]])
			if err != nil || retrievableCount <= 0 {
				t.Fatalf("gate_status ready_for_model_run for %s requires positive c03_retrievable_count, got %q", doctype, row[index["c03_retrievable_count"]])
			}
		}
		if status == "pending_c03" && strings.EqualFold(desensitizedBatchRef, "pending") {
			t.Fatalf("gate_status pending_c03 for %s requires a desensitized_batch_ref before waiting on c03", doctype)
		}
		if !strings.EqualFold(c03QueryRef, "pending") {
			requireNoSyntheticPoCEvidence(t, c03QueryRef, "calibration candidates", "c03_query_ref", rowNumber)
			if c05RawCorpusReferencePattern.MatchString(c03QueryRef) {
				t.Fatalf("c03_query_ref for %s exposes local raw corpus directory: %q", doctype, c03QueryRef)
			}
			retrievableCount, err := strconv.Atoi(row[index["c03_retrievable_count"]])
			if err != nil || retrievableCount <= 0 {
				t.Fatalf("c03_query_ref for %s requires positive c03_retrievable_count, got %q", doctype, row[index["c03_retrievable_count"]])
			}
		}
		if !strings.EqualFold(desensitizedBatchRef, "pending") {
			if !strings.HasPrefix(desensitizedBatchRef, "sanitized-batch:") {
				t.Fatalf("desensitized_batch_ref for %s = %q, want sanitized-batch:<batch-id>", doctype, desensitizedBatchRef)
			}
			requireNoSyntheticPoCEvidence(t, desensitizedBatchRef, "calibration candidates", "desensitized_batch_ref", rowNumber)
		}
		if status == "pending_c03" {
			if !strings.EqualFold(c03QueryRef, "pending") {
				t.Fatalf("gate_status pending_c03 for %s requires c03_query_ref=pending, got %q", doctype, c03QueryRef)
			}
			if got := row[index["c03_retrievable_count"]]; !strings.EqualFold(got, "pending") {
				t.Fatalf("gate_status pending_c03 for %s requires c03_retrievable_count=pending, got %q", doctype, got)
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

func c05CandidateGateCodeFor(status string, rawPackageCount, readablePackageCount int) string {
	if rawPackageCount == 0 || readablePackageCount == 0 {
		return "missing_corpus"
	}
	if status == "insufficient" {
		return "low_sample_count"
	}
	switch status {
	case "pending_desensitization":
		return "awaiting_desensitization"
	case "pending_c03":
		return "awaiting_c03_retrieval"
	case "ready_for_model_run":
		return "ready_for_model_run"
	default:
		return "missing_corpus"
	}
}

func TestC05LocalPackageAuditExplainsReadableCandidateCounts(t *testing.T) {
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-local-package-audit.csv")
	if len(rows) != 9 {
		t.Fatalf("local package audit rows = %d, want 9 high-frequency doctypes", len(rows))
	}

	index := csvIndex(header)
	candidateSummaries := readC05CalibrationCandidateSummaries(t)
	wantDoctypes := c05HighFreqDoctypeSeen()
	allowedAuditStatus := map[string]bool{
		"ready_for_desensitization": true,
		"needs_more_corpus":         true,
		"missing_corpus":            true,
	}
	allowedAuditCodes := map[string]bool{
		"all_packages_extractable":   true,
		"partial_extraction_blocked": true,
		"missing_corpus":             true,
	}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		if _, ok := wantDoctypes[doctype]; !ok {
			t.Fatalf("unexpected doctype %q in local package audit", doctype)
		}
		wantDoctypes[doctype] = true

		candidateSummary, ok := candidateSummaries[doctype]
		if !ok {
			t.Fatalf("local package audit row %d has no matching calibration candidate row for %s", rowNumber, doctype)
		}
		if got := requiredCell(t, row, index, "candidate_batch", rowNumber); got != candidateSummary.candidateBatch {
			t.Fatalf("local package audit row %d candidate_batch = %q, want %q from calibration candidates", rowNumber, got, candidateSummary.candidateBatch)
		}
		requiredCell(t, row, index, "source_group_ref", rowNumber)

		rawPackageCount := requireNonNegativeIntCell(t, row, index, "raw_package_count", rowNumber)
		directExtractableCount := requireNonNegativeIntCell(t, row, index, "direct_extractable_package_count", rowNumber)
		conversionRequiredCount := requireNonNegativeIntCell(t, row, index, "conversion_required_package_count", rowNumber)
		blockedPackageCount := requireNonNegativeIntCell(t, row, index, "blocked_package_count", rowNumber)
		readablePackageCount := requireNonNegativeIntCell(t, row, index, "readable_package_count", rowNumber)
		if rawPackageCount != candidateSummary.rawPackageCount || readablePackageCount != candidateSummary.readablePackageCount {
			t.Fatalf("local package audit row %d counts raw/readable = %d/%d, want %d/%d from calibration candidates", rowNumber, rawPackageCount, readablePackageCount, candidateSummary.rawPackageCount, candidateSummary.readablePackageCount)
		}
		if directExtractableCount+conversionRequiredCount != readablePackageCount {
			t.Fatalf("local package audit row %d direct+conversion = %d, want readable_package_count %d", rowNumber, directExtractableCount+conversionRequiredCount, readablePackageCount)
		}
		if directExtractableCount+conversionRequiredCount+blockedPackageCount != rawPackageCount {
			t.Fatalf("local package audit row %d direct+conversion+blocked = %d, want raw_package_count %d", rowNumber, directExtractableCount+conversionRequiredCount+blockedPackageCount, rawPackageCount)
		}
		readableGap := requireNonNegativeIntCell(t, row, index, "readable_gap_to_100", rowNumber)
		wantGap := 100 - readablePackageCount
		if wantGap < 0 {
			wantGap = 0
		}
		if readableGap != wantGap {
			t.Fatalf("local package audit row %d readable_gap_to_100 = %d, want %d", rowNumber, readableGap, wantGap)
		}
		status := requiredCell(t, row, index, "package_audit_status", rowNumber)
		if !allowedAuditStatus[status] {
			t.Fatalf("local package audit row %d package_audit_status = %q, want known status", rowNumber, status)
		}
		code := requiredCell(t, row, index, "package_audit_code", rowNumber)
		if !allowedAuditCodes[code] {
			t.Fatalf("local package audit row %d package_audit_code = %q, want controlled audit code", rowNumber, code)
		}
		if rawPackageCount == 0 {
			if status != "missing_corpus" || code != "missing_corpus" || readablePackageCount != 0 || blockedPackageCount != 0 {
				t.Fatalf("local package audit row %d missing corpus counts/status/code are inconsistent", rowNumber)
			}
			continue
		}
		if blockedPackageCount > 0 && code != "partial_extraction_blocked" {
			t.Fatalf("local package audit row %d blocked_package_count = %d requires partial_extraction_blocked code", rowNumber, blockedPackageCount)
		}
		if blockedPackageCount == 0 && readablePackageCount == rawPackageCount && code != "all_packages_extractable" {
			t.Fatalf("local package audit row %d all packages extractable requires all_packages_extractable code", rowNumber)
		}
		if readablePackageCount == 0 {
			t.Fatalf("local package audit row %d has raw packages but no readable packages", rowNumber)
		}
	}
	for doctype, seen := range wantDoctypes {
		if !seen {
			t.Fatalf("missing c05 local package audit row for %s", doctype)
		}
	}
}

func TestC05CorpusIntakeReadinessRowsStayAggregateAndActionable(t *testing.T) {
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-corpus-intake-readiness.csv")
	if len(rows) != 9 {
		t.Fatalf("corpus intake readiness rows = %d, want 9 high-frequency doctypes", len(rows))
	}

	index := csvIndex(header)
	candidateSummaries := readC05CalibrationCandidateSummaries(t)
	wantDoctypes := c05HighFreqDoctypeSeen()
	allowedIntakeStages := map[string]bool{
		"ready_for_desensitization": true,
		"needs_more_corpus":         true,
		"missing_corpus":            true,
	}
	allowedNextGates := map[string]bool{
		"pending_corpus": true, "pending_desensitization": true, "insufficient": true,
	}
	allowedReadinessCodes := map[string]bool{
		"has_readable_candidates": true,
		"low_sample_count":        true,
		"missing_corpus":          true,
	}
	rawArtifactExtension := regexp.MustCompile(`(?i)\.(docx?|pdf|xlsx|et)\b`)

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		if _, ok := wantDoctypes[doctype]; !ok {
			t.Fatalf("unexpected doctype %q in corpus intake readiness", doctype)
		}
		wantDoctypes[doctype] = true

		candidateSummary, ok := candidateSummaries[doctype]
		if !ok {
			t.Fatalf("corpus intake readiness row %d has no matching calibration candidate row for %s", rowNumber, doctype)
		}
		candidateBatch := requiredCell(t, row, index, "candidate_batch", rowNumber)
		if candidateBatch != candidateSummary.candidateBatch {
			t.Fatalf("corpus intake readiness row %d candidate_batch = %q, want %q from calibration candidates", rowNumber, candidateBatch, candidateSummary.candidateBatch)
		}
		sourceGroupRef := requiredCell(t, row, index, "source_group_ref", rowNumber)
		for field, value := range map[string]string{
			"candidate_batch":  candidateBatch,
			"source_group_ref": sourceGroupRef,
		} {
			if c05RawCorpusReferencePattern.MatchString(value) {
				t.Fatalf("corpus intake readiness row %d field %s exposes local raw corpus directory: %q", rowNumber, field, value)
			}
			if rawArtifactExtension.MatchString(value) {
				t.Fatalf("corpus intake readiness row %d field %s exposes raw file extension: %q", rowNumber, field, value)
			}
		}

		rawPackageCount := requireNonNegativeIntCell(t, row, index, "raw_package_count", rowNumber)
		readablePackageCount := requireNonNegativeIntCell(t, row, index, "readable_package_count", rowNumber)
		if rawPackageCount != candidateSummary.rawPackageCount || readablePackageCount != candidateSummary.readablePackageCount {
			t.Fatalf("corpus intake readiness row %d counts raw/readable = %d/%d, want %d/%d from calibration candidates", rowNumber, rawPackageCount, readablePackageCount, candidateSummary.rawPackageCount, candidateSummary.readablePackageCount)
		}
		if readablePackageCount > rawPackageCount {
			t.Fatalf("corpus intake readiness row %d readable_package_count = %d, want <= raw_package_count %d", rowNumber, readablePackageCount, rawPackageCount)
		}
		readableGap := requireNonNegativeIntCell(t, row, index, "readable_gap_to_100", rowNumber)
		wantGap := 100 - readablePackageCount
		if wantGap < 0 {
			wantGap = 0
		}
		if readableGap != wantGap {
			t.Fatalf("corpus intake readiness row %d readable_gap_to_100 = %d, want %d", rowNumber, readableGap, wantGap)
		}

		intakeStage := requiredCell(t, row, index, "intake_stage", rowNumber)
		if !allowedIntakeStages[intakeStage] {
			t.Fatalf("corpus intake readiness row %d intake_stage = %q, want known stage", rowNumber, intakeStage)
		}
		nextGate := requiredCell(t, row, index, "next_c03_gate", rowNumber)
		if !allowedNextGates[nextGate] {
			t.Fatalf("corpus intake readiness row %d next_c03_gate = %q, want known c03 gate", rowNumber, nextGate)
		}
		requiredCell(t, row, index, "desensitization_owner", rowNumber)
		requiredCell(t, row, index, "c03_ingestion_owner", rowNumber)
		readinessCode := requiredCell(t, row, index, "readiness_code", rowNumber)
		if !allowedReadinessCodes[readinessCode] {
			t.Fatalf("corpus intake readiness row %d readiness_code = %q, want controlled readiness code", rowNumber, readinessCode)
		}

		switch intakeStage {
		case "ready_for_desensitization":
			if readablePackageCount <= 0 || nextGate != "pending_desensitization" {
				t.Fatalf("corpus intake readiness row %d ready_for_desensitization requires readable candidates and next_c03_gate=pending_desensitization", rowNumber)
			}
			if readinessCode != "has_readable_candidates" {
				t.Fatalf("corpus intake readiness row %d ready_for_desensitization readiness_code = %q, want has_readable_candidates", rowNumber, readinessCode)
			}
		case "needs_more_corpus":
			if readablePackageCount <= 0 || readablePackageCount >= 100 || nextGate != "insufficient" {
				t.Fatalf("corpus intake readiness row %d needs_more_corpus requires 1-99 readable candidates and next_c03_gate=insufficient", rowNumber)
			}
			if readinessCode != "low_sample_count" {
				t.Fatalf("corpus intake readiness row %d needs_more_corpus readiness_code = %q, want low_sample_count", rowNumber, readinessCode)
			}
		case "missing_corpus":
			if readablePackageCount != 0 || nextGate != "pending_corpus" {
				t.Fatalf("corpus intake readiness row %d missing_corpus requires zero readable candidates and next_c03_gate=pending_corpus", rowNumber)
			}
			if readinessCode != "missing_corpus" {
				t.Fatalf("corpus intake readiness row %d missing_corpus readiness_code = %q, want missing_corpus", rowNumber, readinessCode)
			}
		}
	}
	for doctype, seen := range wantDoctypes {
		if !seen {
			t.Fatalf("missing c05 corpus intake readiness row for %s", doctype)
		}
	}
}

func TestC05CalibrationVariantsPlanComparableCoverageForEveryDoctype(t *testing.T) {
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-variants.csv")
	index := csvIndex(header)
	wantSubtypes := map[string]string{
		"通知": "开展活动通知", "请示": "重大事项请求审定请示", "报告": "专项工作报告",
		"函": "工作商洽函", "会议纪要": "专项工作会议纪要", "通报": "工作事务类情况通报",
		"批复": "表态式批复", "讲话稿": "工作部署讲话", "方案": "工作方案",
	}
	type variantCoverage struct {
		count            int
		hasBaseline      bool
		hasComparison    bool
		comparisonGroups map[string]int
	}
	coverage := map[string]variantCoverage{}
	for doctype := range c05HighFreqDoctypeSeen() {
		coverage[doctype] = variantCoverage{comparisonGroups: map[string]int{}}
	}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		requireKnownC05Doctype(t, doctype, "calibration variants", rowNumber)
		if gotSubtype := requiredCell(t, row, index, "subtype", rowNumber); gotSubtype != wantSubtypes[doctype] {
			t.Fatalf("calibration variants row %d subtype for %s = %q, want representative subtype %q", rowNumber, doctype, gotSubtype, wantSubtypes[doctype])
		}
		status := requiredCell(t, row, index, "variant_status", rowNumber)
		if status != "planned" {
			t.Fatalf("calibration variants row %d status = %q, want planned until c03 ready_for_model_run evidence exists", rowNumber, status)
		}
		axis := requiredCell(t, row, index, "comparison_axis", rowNumber)
		group := requiredCell(t, row, index, "comparison_group", rowNumber)
		current := coverage[doctype]
		current.count++
		current.comparisonGroups[group]++
		if axis == "baseline" {
			current.hasBaseline = true
		} else {
			current.hasComparison = true
		}
		coverage[doctype] = current
	}

	for doctype, current := range coverage {
		if current.count < 2 {
			t.Fatalf("calibration variants for %s = %d, want at least baseline plus one comparison variant", doctype, current.count)
		}
		if !current.hasBaseline || !current.hasComparison {
			t.Fatalf("calibration variants for %s must include baseline and non-baseline comparison axes", doctype)
		}
		if len(current.comparisonGroups) != 1 {
			t.Fatalf("calibration variants for %s span %d comparison groups, want one group for comparable planned variants", doctype, len(current.comparisonGroups))
		}
	}
}

type c05CalibrationCandidateSummary struct {
	candidateBatch       string
	rawPackageCount      int
	readablePackageCount int
}

func readC05CalibrationCandidateSummaries(t *testing.T) map[string]c05CalibrationCandidateSummary {
	t.Helper()
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-candidates.csv")
	index := csvIndex(header)
	summaries := map[string]c05CalibrationCandidateSummary{}
	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		if summaries[doctype].candidateBatch != "" {
			t.Fatalf("calibration candidates row %d duplicates doctype %q", rowNumber, doctype)
		}
		summaries[doctype] = c05CalibrationCandidateSummary{
			candidateBatch:       requiredCell(t, row, index, "candidate_batch", rowNumber),
			rawPackageCount:      requireNonNegativeIntCell(t, row, index, "raw_package_count", rowNumber),
			readablePackageCount: requireNonNegativeIntCell(t, row, index, "readable_package_count", rowNumber),
		}
	}
	return summaries
}

func TestC05CalibrationRunRowsStayTraceableWhenPresent(t *testing.T) {
	readyC03Queries := readReadyC05CalibrationCandidateQueries(t)
	variants := readC05CalibrationVariants(t)
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
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		requireKnownC05Doctype(t, doctype, "calibration runs", rowNumber)
		for _, field := range []string{"subtype", "source_sample_id", "c03_query_id", "prompt_variant_id", "contract_version", "model_provider", "model_name", "model_backend", "model_endpoint_evidence_ref"} {
			requiredCell(t, row, index, field, rowNumber)
		}
		for _, field := range []string{"model_provider", "model_name", "model_backend", "model_endpoint_evidence_ref"} {
			requireNoSyntheticPoCEvidence(t, row[index[field]], "calibration runs", field, rowNumber)
		}
		topK := requirePositiveIntCell(t, row, index, "topk", rowNumber)
		promptTotalChars := requirePositiveIntCell(t, row, index, "prompt_total_chars", rowNumber)
		promptTokenEstimate := requirePositiveIntCell(t, row, index, "prompt_token_estimate", rowNumber)
		requireMatchingC05CalibrationVariant(t, variants, calibrationVariant{
			id:                  row[index["prompt_variant_id"]],
			doctype:             doctype,
			subtype:             row[index["subtype"]],
			topK:                topK,
			promptTotalChars:    promptTotalChars,
			promptTokenEstimate: promptTokenEstimate,
			contractVersion:     row[index["contract_version"]],
		}, "calibration runs", rowNumber)
		c03QueryID := requiredCell(t, row, index, "c03_query_id", rowNumber)
		if strings.EqualFold(c03QueryID, "pending") || c05RawCorpusReferencePattern.MatchString(c03QueryID) {
			t.Fatalf("calibration runs row %d c03_query_id = %q, want c03 retrieval evidence, not local/raw corpus state", rowNumber, c03QueryID)
		} else {
			requireNoSyntheticPoCEvidence(t, c03QueryID, "calibration runs", "c03_query_id", rowNumber)
			requireReadyC05CalibrationCandidateQuery(t, readyC03Queries, doctype, c03QueryID, "calibration runs", rowNumber)
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

func TestC05CalibrationVariantRowsStayAuditableWhenPresent(t *testing.T) {
	readC05CalibrationVariants(t)
}

func TestC05CalibrationVariantVersionsRejectOpaqueLabels(t *testing.T) {
	rejected := []string{"默认值", "默认", "default", "latest", "current", "TBD", "待定", "unknown", "pending"}
	for _, value := range rejected {
		if !looksOpaqueC05CalibrationVersion(value) {
			t.Fatalf("version label %q should be treated as opaque", value)
		}
	}

	allowed := []string{"contract:v2026-06-20-r1", "wording:v1.1-notice-rubric-a", "template-object:notice/v3"}
	for _, value := range allowed {
		if looksOpaqueC05CalibrationVersion(value) {
			t.Fatalf("version label %q should be allowed as traceable", value)
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
	variants := readC05CalibrationVariants(t)
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
			selectedRuns := make([]calibrationRunEvidence, 0, len(evidenceRefs.runIDs))
			selectedRunIDs := map[string]bool{}
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
				if run.topK == selectedTopK && run.promptTotalChars == selectedPromptTotalChars && run.contractVersion == selectedContractVersion {
					selectedRuns = append(selectedRuns, run)
					selectedRunIDs[runID] = true
				}
				referencedRuns = append(referencedRuns, run)
			}
			if len(selectedRuns) == 0 {
				t.Fatalf("calibration decisions row %d has no referenced completed run matching selected topk/chars/version", rowNumber)
			}
			if runCount != len(selectedRuns) {
				t.Fatalf("calibration decisions row %d run_count = %d, want %d selected referenced unique runs", rowNumber, runCount, len(selectedRuns))
			}
			if gotMedian := medianInt(selectedRuns, func(run calibrationRunEvidence) int { return run.firstTokenMS }); medianFirstTokenMS != gotMedian {
				t.Fatalf("calibration decisions row %d median_first_token_ms = %d, want %d from referenced runs", rowNumber, medianFirstTokenMS, gotMedian)
			}
			if gotP95 := p95Int(selectedRuns, func(run calibrationRunEvidence) int { return run.totalGenerationMS }); p95TotalGenerationMS != gotP95 {
				t.Fatalf("calibration decisions row %d p95_total_generation_ms = %d, want %d from referenced runs", rowNumber, p95TotalGenerationMS, gotP95)
			}

			referencedReviews := make([]calibrationReviewEvidence, 0, len(evidenceRefs.reviewRecordIDs))
			selectedReviews := make([]calibrationReviewEvidence, 0, len(evidenceRefs.reviewRecordIDs))
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
				if selectedRunIDs[review.runID] {
					selectedReviews = append(selectedReviews, review)
				}
			}
			for runID := range referencedRunIDs {
				if !reviewedRunIDs[runID] {
					t.Fatalf("calibration decisions row %d references run %q without a corresponding review evidence ref", rowNumber, runID)
				}
			}
			if len(selectedReviews) == 0 {
				t.Fatalf("calibration decisions row %d has no review for selected run evidence", rowNumber)
			}
			if gotAdoptionRate := adoptionRateFromReviews(selectedReviews); math.Abs(adoptionRate-gotAdoptionRate) > 0.005 {
				t.Fatalf("calibration decisions row %d adoption_rate = %.4f, want %.4f from referenced reviews", rowNumber, adoptionRate, gotAdoptionRate)
			}
			if err := calibrationDecisionVariantEvidenceError(
				evidenceRefs,
				referencedRuns,
				variants,
				doctype,
				strings.TrimSpace(row[index["subtype"]]),
				selectedTopK,
				selectedPromptTotalChars,
				selectedContractVersion,
			); err != nil {
				t.Fatalf("calibration decisions row %d has invalid prompt variant comparison evidence: %v", rowNumber, err)
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

func TestC05CalibrationDecisionVariantEvidenceRequiresComparableVariants(t *testing.T) {
	variants := map[string]calibrationVariant{
		"variant:notice-topk3": {
			id:                  "variant:notice-topk3",
			doctype:             "通知",
			subtype:             "工作通知",
			topK:                3,
			promptTotalChars:    6000,
			promptTokenEstimate: 1800,
			contractVersion:     "contract:v2026-06-20-r1",
			comparisonGroup:     "notice-topk-calibration",
			comparisonAxis:      "topk",
		},
		"variant:notice-topk5": {
			id:                  "variant:notice-topk5",
			doctype:             "通知",
			subtype:             "工作通知",
			topK:                5,
			promptTotalChars:    6000,
			promptTokenEstimate: 2400,
			contractVersion:     "contract:v2026-06-20-r1",
			comparisonGroup:     "notice-topk-calibration",
			comparisonAxis:      "topk",
		},
	}
	runs := []calibrationRunEvidence{
		{id: "run-notice-topk3", doctype: "通知", subtype: "工作通知", promptVariantID: "variant:notice-topk3", topK: 3, promptTotalChars: 6000, contractVersion: "contract:v2026-06-20-r1", streamCompleted: true},
		{id: "run-notice-topk5", doctype: "通知", subtype: "工作通知", promptVariantID: "variant:notice-topk5", topK: 5, promptTotalChars: 6000, contractVersion: "contract:v2026-06-20-r1", streamCompleted: true},
	}
	refs := calibrationEvidenceRefs{
		runIDs:          []string{"run-notice-topk3", "run-notice-topk5"},
		reviewRecordIDs: []string{"review-notice-topk3", "review-notice-topk5"},
		variantIDs:      []string{"variant:notice-topk3", "variant:notice-topk5"},
	}

	if err := calibrationDecisionVariantEvidenceError(refs, runs, variants, "通知", "工作通知", 3, 6000, "contract:v2026-06-20-r1"); err != nil {
		t.Fatalf("expected comparable variant evidence to pass: %v", err)
	}

	tests := []struct {
		name string
		refs calibrationEvidenceRefs
		runs []calibrationRunEvidence
	}{
		{
			name: "missing variant refs",
			refs: calibrationEvidenceRefs{
				runIDs:          []string{"run-notice-topk3", "run-notice-topk5"},
				reviewRecordIDs: []string{"review-notice-topk3", "review-notice-topk5"},
			},
			runs: runs,
		},
		{
			name: "single variant ref",
			refs: calibrationEvidenceRefs{
				runIDs:          []string{"run-notice-topk3"},
				reviewRecordIDs: []string{"review-notice-topk3"},
				variantIDs:      []string{"variant:notice-topk3"},
			},
			runs: runs[:1],
		},
		{
			name: "variant not covered by run",
			refs: refs,
			runs: runs[:1],
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := calibrationDecisionVariantEvidenceError(tc.refs, tc.runs, variants, "通知", "工作通知", 3, 6000, "contract:v2026-06-20-r1"); err == nil {
				t.Fatalf("expected invalid variant evidence to be rejected")
			}
		})
	}
}

func TestC05CalibrationEvidenceRefsKeepVariantIDPrefix(t *testing.T) {
	refs := parseCalibrationEvidenceRefs(t, "run:run-notice-topk3;review:review-notice-topk3;variant:notice-topk3", 2)
	if got, want := strings.Join(refs.variantIDs, ","), "variant:notice-topk3"; got != want {
		t.Fatalf("variant refs = %q, want %q", got, want)
	}
}

func TestC05CalibrationCSVsDoNotExposeRawCorpusArtifacts(t *testing.T) {
	files := []string{
		"c05-high-freq-doctype-calibration-candidates.csv",
		"c05-high-freq-doctype-corpus-intake-readiness.csv",
		"c05-high-freq-doctype-local-package-audit.csv",
		"c05-high-freq-doctype-calibration-variants.csv",
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

func TestC05ReadyCalibrationCandidateQueryMatching(t *testing.T) {
	readyQueries := map[string]map[string]bool{
		"通知": {
			"c03-query:notice-ready-001": true,
		},
		"请示": {
			"c03-query:request-ready-001": true,
		},
	}

	if !hasReadyC05CalibrationCandidateQuery(readyQueries, "通知", "c03-query:notice-ready-001") {
		t.Fatalf("expected matching doctype/query pair to be accepted")
	}

	rejected := []struct {
		name       string
		doctype    string
		c03QueryID string
	}{
		{name: "different doctype", doctype: "请示", c03QueryID: "c03-query:notice-ready-001"},
		{name: "unknown query", doctype: "通知", c03QueryID: "c03-query:notice-missing"},
		{name: "doctype not ready", doctype: "报告", c03QueryID: "c03-query:report-ready-001"},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if hasReadyC05CalibrationCandidateQuery(readyQueries, tc.doctype, tc.c03QueryID) {
				t.Fatalf("expected %s/%s to be rejected without a same-doctype ready candidate", tc.doctype, tc.c03QueryID)
			}
		})
	}
}

func TestC05CalibrationRunVariantEvidenceRequiresReadyForRunVariant(t *testing.T) {
	variant := calibrationVariant{
		id:                  "variant:notice-planned",
		doctype:             "通知",
		subtype:             "工作通知",
		topK:                3,
		promptTotalChars:    6000,
		promptTokenEstimate: 1800,
		contractVersion:     "contract:v2026-06-20-r1",
		status:              "planned",
	}
	run := calibrationVariant{
		id:                  variant.id,
		doctype:             variant.doctype,
		subtype:             variant.subtype,
		topK:                variant.topK,
		promptTotalChars:    variant.promptTotalChars,
		promptTokenEstimate: variant.promptTokenEstimate,
		contractVersion:     variant.contractVersion,
	}

	err := calibrationRunVariantEvidenceError(variant, run)
	if err == nil || !strings.Contains(err.Error(), "ready_for_run") {
		t.Fatalf("planned variant error = %v, want ready_for_run rejection", err)
	}

	variant.status = "ready_for_run"
	if err := calibrationRunVariantEvidenceError(variant, run); err != nil {
		t.Fatalf("ready_for_run variant should be accepted, got %v", err)
	}
}

func c05HighFreqDoctypeSeen() map[string]bool {
	return map[string]bool{
		"通知": false, "请示": false, "报告": false, "函": false, "会议纪要": false,
		"通报": false, "批复": false, "讲话稿": false, "方案": false,
	}
}

func readReadyC05CalibrationCandidateQueries(t *testing.T) map[string]map[string]bool {
	t.Helper()
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-candidates.csv")
	index := csvIndex(header)
	queriesByDoctype := map[string]map[string]bool{}
	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		requireKnownC05Doctype(t, doctype, "calibration candidates", rowNumber)
		if row[index["gate_status"]] != "ready_for_model_run" {
			continue
		}
		c03QueryRef := requiredCell(t, row, index, "c03_query_ref", rowNumber)
		if strings.EqualFold(c03QueryRef, "pending") {
			t.Fatalf("calibration candidates row %d is ready_for_model_run but c03_query_ref is pending", rowNumber)
		}
		if queriesByDoctype[doctype] == nil {
			queriesByDoctype[doctype] = map[string]bool{}
		}
		queriesByDoctype[doctype][c03QueryRef] = true
	}
	return queriesByDoctype
}

func requireReadyC05CalibrationCandidateQuery(t *testing.T, readyQueries map[string]map[string]bool, doctype, c03QueryID, source string, rowNumber int) {
	t.Helper()
	if !hasReadyC05CalibrationCandidateQuery(readyQueries, doctype, c03QueryID) {
		t.Fatalf("%s row %d c03_query_id = %q has no matching ready_for_model_run candidate for %s", source, rowNumber, c03QueryID, doctype)
	}
}

func hasReadyC05CalibrationCandidateQuery(readyQueries map[string]map[string]bool, doctype, c03QueryID string) bool {
	return readyQueries[doctype][c03QueryID]
}

type calibrationRunEvidence struct {
	id                string
	doctype           string
	subtype           string
	promptVariantID   string
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
	variantIDs      []string
}

type calibrationVariant struct {
	id                  string
	doctype             string
	subtype             string
	topK                int
	promptTotalChars    int
	promptTokenEstimate int
	contractVersion     string
	comparisonGroup     string
	comparisonAxis      string
	status              string
}

func readC05CalibrationVariants(t *testing.T) map[string]calibrationVariant {
	t.Helper()
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-calibration-variants.csv")
	index := csvIndex(header)
	variants := map[string]calibrationVariant{}
	readyC03Queries := readReadyC05CalibrationCandidateQueries(t)
	allowedAxis := map[string]bool{
		"baseline": true, "topk": true, "prompt_total_chars": true,
		"contract_wording": true, "combined": true,
	}
	allowedStatus := map[string]bool{"planned": true, "ready_for_run": true, "retired": true}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		variantID := requiredCell(t, row, index, "variant_id", rowNumber)
		if variants[variantID].id != "" {
			t.Fatalf("calibration variants row %d duplicates variant_id %q", rowNumber, variantID)
		}
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		requireKnownC05Doctype(t, doctype, "calibration variants", rowNumber)
		subtype := requiredCell(t, row, index, "subtype", rowNumber)
		topK := requirePositiveIntCell(t, row, index, "topk", rowNumber)
		promptTotalChars := requirePositiveIntCell(t, row, index, "prompt_total_chars", rowNumber)
		promptTokenEstimate := requirePositiveIntCell(t, row, index, "prompt_token_estimate", rowNumber)
		contractVersion := requiredTraceableC05CalibrationVersion(t, row, index, "contract_version", rowNumber)
		requiredTraceableC05CalibrationVersion(t, row, index, "wording_version", rowNumber)
		comparisonGroup := requiredCell(t, row, index, "comparison_group", rowNumber)
		axis := requiredCell(t, row, index, "comparison_axis", rowNumber)
		if !allowedAxis[axis] {
			t.Fatalf("calibration variants row %d comparison_axis = %q, want known calibration axis", rowNumber, axis)
		}
		status := requiredCell(t, row, index, "variant_status", rowNumber)
		if !allowedStatus[status] {
			t.Fatalf("calibration variants row %d variant_status = %q, want planned/ready_for_run/retired", rowNumber, status)
		}
		requiredCell(t, row, index, "notes", rowNumber)
		if status == "ready_for_run" && len(readyC03Queries[doctype]) == 0 {
			t.Fatalf("calibration variants row %d is ready_for_run but %s has no ready c03 candidate", rowNumber, doctype)
		}
		variants[variantID] = calibrationVariant{
			id:                  variantID,
			doctype:             doctype,
			subtype:             subtype,
			topK:                topK,
			promptTotalChars:    promptTotalChars,
			promptTokenEstimate: promptTokenEstimate,
			contractVersion:     contractVersion,
			comparisonGroup:     comparisonGroup,
			comparisonAxis:      axis,
			status:              status,
		}
	}
	return variants
}

func requireMatchingC05CalibrationVariant(t *testing.T, variants map[string]calibrationVariant, run calibrationVariant, source string, rowNumber int) {
	t.Helper()
	variant, ok := variants[strings.TrimSpace(run.id)]
	if !ok {
		t.Fatalf("%s row %d prompt_variant_id = %q has no matching calibration variant", source, rowNumber, run.id)
	}
	if err := calibrationRunVariantEvidenceError(variant, run); err != nil {
		t.Fatalf(
			"%s row %d prompt_variant_id = %q is not valid run evidence: %v",
			source, rowNumber, run.id, err,
		)
	}
}

func calibrationRunVariantEvidenceError(variant, run calibrationVariant) error {
	if strings.TrimSpace(variant.status) != "ready_for_run" {
		return fmt.Errorf("variant status = %q, want ready_for_run before model run evidence", variant.status)
	}
	if variant.doctype != run.doctype || variant.subtype != strings.TrimSpace(run.subtype) ||
		variant.topK != run.topK || variant.promptTotalChars != run.promptTotalChars ||
		variant.promptTokenEstimate != run.promptTokenEstimate || variant.contractVersion != strings.TrimSpace(run.contractVersion) {
		return fmt.Errorf("run settings do not match registered variant settings")
	}
	return nil
}

func requiredTraceableC05CalibrationVersion(t *testing.T, row []string, index map[string]int, field string, rowNumber int) string {
	t.Helper()
	value := requiredCell(t, row, index, field, rowNumber)
	if looksOpaqueC05CalibrationVersion(value) {
		t.Fatalf("calibration variants row %d %s = %q, want traceable version reference", rowNumber, field, value)
	}
	return value
}

func looksOpaqueC05CalibrationVersion(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "默认", "默认值", "default", "latest", "current", "tbd", "todo", "pending", "unknown", "n/a", "na", "待定":
		return true
	default:
		return false
	}
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
			promptVariantID:   strings.TrimSpace(row[index["prompt_variant_id"]]),
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
	seenVariantIDs := map[string]bool{}
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
		case strings.HasPrefix(token, "variant:"):
			if strings.TrimSpace(strings.TrimPrefix(token, "variant:")) == "" {
				t.Fatalf("calibration decisions row %d has empty variant evidence ref in %q", rowNumber, value)
			}
			variantID := token
			if seenVariantIDs[variantID] {
				t.Fatalf("calibration decisions row %d duplicates variant evidence ref %q", rowNumber, variantID)
			}
			seenVariantIDs[variantID] = true
			refs.variantIDs = append(refs.variantIDs, variantID)
		default:
			t.Fatalf("calibration decisions row %d evidence_refs token %q must use run:<id>, review:<id>, or variant:<id>", rowNumber, token)
		}
	}
	return refs
}

func calibrationDecisionVariantEvidenceError(refs calibrationEvidenceRefs, runs []calibrationRunEvidence, variants map[string]calibrationVariant, doctype, subtype string, selectedTopK, selectedPromptTotalChars int, selectedContractVersion string) error {
	if len(refs.variantIDs) < 2 {
		return fmt.Errorf("evidence_refs must include at least two variant:<id> refs")
	}

	referencedVariantIDs := map[string]bool{}
	for _, variantID := range refs.variantIDs {
		referencedVariantIDs[variantID] = true
	}
	runVariantIDs := map[string]bool{}
	for _, run := range runs {
		if run.promptVariantID == "" {
			return fmt.Errorf("run %s has no prompt_variant_id", run.id)
		}
		if !referencedVariantIDs[run.promptVariantID] {
			return fmt.Errorf("run %s uses prompt_variant_id %s outside evidence_refs", run.id, run.promptVariantID)
		}
		runVariantIDs[run.promptVariantID] = true
	}

	comparisonGroup := ""
	hasCalibrationAxis := false
	selectedVariantFound := false
	seenVariantIDs := map[string]bool{}
	for _, variantID := range refs.variantIDs {
		if seenVariantIDs[variantID] {
			return fmt.Errorf("duplicates variant ref %s", variantID)
		}
		seenVariantIDs[variantID] = true

		variant, ok := variants[variantID]
		if !ok {
			return fmt.Errorf("references unknown variant %s", variantID)
		}
		if variant.doctype != doctype {
			return fmt.Errorf("variant %s belongs to doctype %s, want %s", variantID, variant.doctype, doctype)
		}
		if subtype != "" && variant.subtype != subtype {
			return fmt.Errorf("variant %s belongs to subtype %s, want %s", variantID, variant.subtype, subtype)
		}
		if !runVariantIDs[variantID] {
			return fmt.Errorf("variant %s is not covered by referenced completed runs", variantID)
		}
		if comparisonGroup == "" {
			comparisonGroup = variant.comparisonGroup
		} else if variant.comparisonGroup != comparisonGroup {
			return fmt.Errorf("variant %s comparison_group = %s, want %s", variantID, variant.comparisonGroup, comparisonGroup)
		}
		if variant.comparisonAxis != "baseline" {
			hasCalibrationAxis = true
		}
		if variant.topK == selectedTopK && variant.promptTotalChars == selectedPromptTotalChars && variant.contractVersion == selectedContractVersion {
			selectedVariantFound = true
		}
	}
	if len(seenVariantIDs) < 2 {
		return fmt.Errorf("evidence_refs must include at least two distinct variants")
	}
	if !hasCalibrationAxis {
		return fmt.Errorf("variant refs only include baseline axis, want calibration axis evidence")
	}
	if !selectedVariantFound {
		return fmt.Errorf("no variant ref matches selected topk/chars/contract version")
	}
	return nil
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
