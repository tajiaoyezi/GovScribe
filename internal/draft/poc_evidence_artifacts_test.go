package draft

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	c05SyntheticPoCEvidencePattern   = regexp.MustCompile(`(?i)(^|[^a-z0-9])(fake|faked|mock|mocked|stub|stubbed|dummy|httptest|testserver|localhost|127\.0\.0\.1|::1|unit[_ -]?test|example\.com|local[-_ ]?(model|endpoint|gateway|server)|dev[-_ ]?(model|endpoint|gateway|server)|test[-_ ]?(model|endpoint|gateway|server))([^a-z0-9]|$)`)
	c05CrossCompileOnlyEvidenceRegex = regexp.MustCompile(`(?i)(cross[-_ ]?(build|compile|compiled)|交叉编译|host[-_ ]?only|local[-_ ]?(host|runtime|machine)|dev[-_ ]?(host|machine)|localhost|127\.0\.0\.1|windows|x86_64|amd64|本机运行)`)
	// Evidence CSVs may reference sanitized batches or c03 query ids, but never the local raw corpus directory name.
	c05RawCorpusReferencePattern = regexp.MustCompile(`各类文件`)
)

func TestC05PoCEvidenceCSVHeadersStayAuditable(t *testing.T) {
	tests := map[string][]string{
		"c05-high-freq-doctype-private-model-runs.csv": {
			"run_id", "run_date", "doctype", "subtype", "model_provider", "model_name",
			"model_backend", "model_endpoint_evidence_ref", "deployment_scope",
			"content_security_level", "c03_query_id", "prompt_variant_id", "topk",
			"prompt_total_chars", "contract_version", "first_token_ms", "total_generation_ms", "completion_chars",
			"stream_completed", "error_reason", "output_ref", "review_record_id", "notes",
		},
		"c05-high-freq-doctype-private-model-reviews.csv": {
			"review_record_id", "run_id", "reviewer", "review_date", "doctype_norm_score",
			"structure_score", "direction_score", "organ_tone_score", "adoption_status",
			"counts_as_adopted", "meets_like_govdoc", "notes",
		},
		"c05-high-freq-doctype-private-model-decisions.csv": {
			"decision_id", "decision_date", "doctype", "model_profile", "run_count",
			"adoption_rate", "average_rubric_score", "pass_fail", "quality_conclusion",
			"downgrade_recommended", "decision_owner", "evidence_refs", "notes",
		},
		"c05-high-freq-doctype-xinchuang-runtime-runs.csv": {
			"run_id", "run_date", "platform_id", "cpu_arch", "os_name", "os_version",
			"kernel_version", "go_version", "binary_ref", "runtime_mode",
			"platform_fingerprint_ref", "postgres_connected", "minio_connected", "c01_connected",
			"c03_connected", "sse_stream_completed", "first_token_ms", "total_generation_ms",
			"error_reason", "evidence_ref", "operator", "notes",
		},
		"c05-high-freq-doctype-xinchuang-runtime-decisions.csv": {
			"decision_id", "decision_date", "platform_id", "cpu_arch", "os_name",
			"run_id", "end_to_end_pass", "decision_owner", "evidence_refs", "notes",
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

func TestC05PoCEvidenceRejectsSyntheticPrivateModelSignals(t *testing.T) {
	rejected := []string{
		"fake-provider",
		"mocked-provider",
		"local_stub_gateway",
		"local-model-endpoint",
		"dev-server-private-model",
		"dummy-domestic-model",
		"httptest-model-endpoint",
		"http://localhost:8080/v1",
		"http://127.0.0.1:9000/v1",
		"unit-test-run",
	}
	for _, value := range rejected {
		if !looksSyntheticPoCEvidence(value) {
			t.Fatalf("value %q should be treated as synthetic private model evidence", value)
		}
	}

	allowed := []string{
		"qwen-vllm-private-gateway",
		"deployment-evidence:private-model-gateway-20260620",
		"domestic-provider-audit:run-20260620-001",
	}
	for _, value := range allowed {
		if looksSyntheticPoCEvidence(value) {
			t.Fatalf("value %q should be allowed as non-synthetic private model evidence", value)
		}
	}
}

func TestC05PoCEvidenceRejectsCrossCompileOnlyRuntimeSignals(t *testing.T) {
	rejected := []string{
		"cross-build-log:loongarch64",
		"cross_compile_artifact_only",
		"仅交叉编译未上机",
		"windows-amd64-host-only",
		"local-host-runtime-log",
		"dev-machine-run",
		"本机运行记录",
	}
	for _, value := range rejected {
		if !looksCrossCompileOnlyEvidence(value) {
			t.Fatalf("value %q should be treated as cross-compile/local-host runtime evidence", value)
		}
	}

	allowed := []string{
		"platform-fingerprint:kylin-arm64-uname-audit-20260620",
		"runtime-log:loongarch64-target-host-sse-pass",
		"binary:govscribe-linux-loongarch64-20260620",
	}
	for _, value := range allowed {
		if looksCrossCompileOnlyEvidence(value) {
			t.Fatalf("value %q should be allowed as target platform runtime evidence", value)
		}
	}
}

func TestC05EvidenceRejectsRawCorpusDirectoryReferences(t *testing.T) {
	rejected := []string{
		"各类文件",
		"各类文件/",
		"source:各类文件",
		`H:\devlopment\code\GovScribe\各类文件`,
	}
	for _, value := range rejected {
		if !c05RawCorpusReferencePattern.MatchString(value) {
			t.Fatalf("value %q should be treated as a raw local corpus directory reference", value)
		}
	}

	allowed := []string{
		"local-corpus-20260620",
		"sanitized-corpus-batch-20260620",
		"c03-query:highfreq-notice-001",
	}
	for _, value := range allowed {
		if c05RawCorpusReferencePattern.MatchString(value) {
			t.Fatalf("value %q should be allowed as an abstract or c03 evidence reference", value)
		}
	}
}

func TestC05PrivateModelPoCEvidenceRowsStayTraceableWhenPresent(t *testing.T) {
	runs := readC05PrivateModelRuns(t)
	reviews := readC05PrivateModelReviews(t, runs)
	variants := readC05CalibrationVariants(t)

	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-private-model-decisions.csv")
	index := csvIndex(header)
	seenDecisionIDs := map[string]bool{}
	allowedPassFail := map[string]bool{"pass": true, "fail": true, "blocked": true, "insufficient_evidence": true}
	decidedDoctypes := map[string]bool{}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		decisionID := requiredCell(t, row, index, "decision_id", rowNumber)
		if seenDecisionIDs[decisionID] {
			t.Fatalf("private model decisions row %d duplicates decision_id %q", rowNumber, decisionID)
		}
		seenDecisionIDs[decisionID] = true
		requireISODateCell(t, row, index, "decision_date", rowNumber)
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		requireKnownC05Doctype(t, doctype, "private model decisions", rowNumber)
		modelProfile := requiredCell(t, row, index, "model_profile", rowNumber)
		passFail := requiredCell(t, row, index, "pass_fail", rowNumber)
		if !allowedPassFail[passFail] {
			t.Fatalf("private model decisions row %d pass_fail = %q, want known status", rowNumber, passFail)
		}
		requiredCell(t, row, index, "quality_conclusion", rowNumber)
		if _, err := strconv.ParseBool(requiredCell(t, row, index, "downgrade_recommended", rowNumber)); err != nil {
			t.Fatalf("private model decisions row %d downgrade_recommended = %q, want boolean: %v", rowNumber, row[index["downgrade_recommended"]], err)
		}
		requiredCell(t, row, index, "decision_owner", rowNumber)
		requiredCell(t, row, index, "notes", rowNumber)

		if passFail == "pass" || passFail == "fail" {
			runCount := requirePositiveIntCell(t, row, index, "run_count", rowNumber)
			adoptionRate := requireRatioCell(t, row, index, "adoption_rate", rowNumber)
			averageRubricScore := requireFloatRangeCell(t, row, index, "average_rubric_score", rowNumber, 1, 5)
			evidenceRefs := parseCalibrationEvidenceRefs(t, requiredCell(t, row, index, "evidence_refs", rowNumber), rowNumber)
			if len(evidenceRefs.runIDs) == 0 || len(evidenceRefs.reviewRecordIDs) == 0 {
				t.Fatalf("private model decisions row %d evidence_refs must include run and review refs", rowNumber)
			}

			referencedRuns := map[string]c05PrivateModelRun{}
			referencedRunList := make([]c05PrivateModelRun, 0, len(evidenceRefs.runIDs))
			for _, runID := range evidenceRefs.runIDs {
				run, ok := runs[runID]
				if !ok {
					t.Fatalf("private model decisions row %d references unknown run_id %q", rowNumber, runID)
				}
				if run.doctype != doctype || run.modelProfile != modelProfile {
					t.Fatalf("private model decisions row %d references run %q for %s/%s, want %s/%s", rowNumber, runID, run.doctype, run.modelProfile, doctype, modelProfile)
				}
				if !run.streamCompleted {
					t.Fatalf("private model decisions row %d references incomplete run %q", rowNumber, runID)
				}
				referencedRuns[runID] = run
				referencedRunList = append(referencedRunList, run)
			}
			if runCount != len(referencedRuns) {
				t.Fatalf("private model decisions row %d run_count = %d, want %d referenced unique runs", rowNumber, runCount, len(referencedRuns))
			}
			if err := privateModelDecisionVariantEvidenceError(evidenceRefs, referencedRunList, variants, doctype); err != nil {
				t.Fatalf("private model decisions row %d has invalid prompt variant evidence: %v", rowNumber, err)
			}

			referencedReviews := make([]c05PrivateModelReview, 0, len(evidenceRefs.reviewRecordIDs))
			reviewedRunIDs := map[string]bool{}
			for _, reviewID := range evidenceRefs.reviewRecordIDs {
				review, ok := reviews[reviewID]
				if !ok {
					t.Fatalf("private model decisions row %d references unknown review_record_id %q", rowNumber, reviewID)
				}
				if _, ok := referencedRuns[review.runID]; !ok {
					t.Fatalf("private model decisions row %d references review %q for run %q outside evidence run set", rowNumber, reviewID, review.runID)
				}
				reviewedRunIDs[review.runID] = true
				referencedReviews = append(referencedReviews, review)
			}
			for runID := range referencedRuns {
				if !reviewedRunIDs[runID] {
					t.Fatalf("private model decisions row %d references run %q without matching review", rowNumber, runID)
				}
			}
			if gotAdoptionRate := privateModelAdoptionRate(referencedReviews); math.Abs(adoptionRate-gotAdoptionRate) > 0.005 {
				t.Fatalf("private model decisions row %d adoption_rate = %.4f, want %.4f", rowNumber, adoptionRate, gotAdoptionRate)
			}
			if gotAverage := privateModelAverageRubricScore(referencedReviews); math.Abs(averageRubricScore-gotAverage) > 0.005 {
				t.Fatalf("private model decisions row %d average_rubric_score = %.4f, want %.4f", rowNumber, averageRubricScore, gotAverage)
			}
			decidedDoctypes[doctype] = true
		}
	}

	if c05TaskChecked(t, "8.1") {
		for doctype := range c05HighFreqDoctypeSeen() {
			if !decidedDoctypes[doctype] {
				t.Fatalf("task 8.1 is checked but private model decisions have no pass/fail evidence for %s", doctype)
			}
		}
	}
}

func TestC05PrivateModelCalibrationVariantMatching(t *testing.T) {
	variants := map[string]calibrationVariant{
		"variant:notice-topk3-v1": {
			id:               "variant:notice-topk3-v1",
			doctype:          "通知",
			subtype:          "工作通知",
			topK:             3,
			promptTotalChars: 6000,
			contractVersion:  "contract:v2026-06-20-r1",
		},
	}

	if !hasMatchingC05PrivateModelCalibrationVariant(variants, calibrationVariant{
		id:               "variant:notice-topk3-v1",
		doctype:          "通知",
		subtype:          "工作通知",
		topK:             3,
		promptTotalChars: 6000,
		contractVersion:  "contract:v2026-06-20-r1",
	}) {
		t.Fatalf("expected identical private model run variant settings to match")
	}

	rejected := []struct {
		name string
		run  calibrationVariant
	}{
		{name: "unknown variant", run: calibrationVariant{id: "variant:missing", doctype: "通知", subtype: "工作通知", topK: 3, promptTotalChars: 6000, contractVersion: "contract:v2026-06-20-r1"}},
		{name: "doctype mismatch", run: calibrationVariant{id: "variant:notice-topk3-v1", doctype: "请示", subtype: "工作通知", topK: 3, promptTotalChars: 6000, contractVersion: "contract:v2026-06-20-r1"}},
		{name: "subtype mismatch", run: calibrationVariant{id: "variant:notice-topk3-v1", doctype: "通知", subtype: "会议通知", topK: 3, promptTotalChars: 6000, contractVersion: "contract:v2026-06-20-r1"}},
		{name: "topk mismatch", run: calibrationVariant{id: "variant:notice-topk3-v1", doctype: "通知", subtype: "工作通知", topK: 5, promptTotalChars: 6000, contractVersion: "contract:v2026-06-20-r1"}},
		{name: "prompt length mismatch", run: calibrationVariant{id: "variant:notice-topk3-v1", doctype: "通知", subtype: "工作通知", topK: 3, promptTotalChars: 8000, contractVersion: "contract:v2026-06-20-r1"}},
		{name: "contract mismatch", run: calibrationVariant{id: "variant:notice-topk3-v1", doctype: "通知", subtype: "工作通知", topK: 3, promptTotalChars: 6000, contractVersion: "contract:v2026-06-21-r2"}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if hasMatchingC05PrivateModelCalibrationVariant(variants, tc.run) {
				t.Fatalf("expected %s to be rejected", tc.name)
			}
		})
	}
}

func TestC05PrivateModelDecisionVariantEvidenceRequiresReferencedRunVariants(t *testing.T) {
	variants := map[string]calibrationVariant{
		"variant:notice-topk3-v1": {
			id:               "variant:notice-topk3-v1",
			doctype:          "通知",
			subtype:          "工作通知",
			topK:             3,
			promptTotalChars: 6000,
			contractVersion:  "contract:v2026-06-20-r1",
		},
		"variant:notice-topk5-v1": {
			id:               "variant:notice-topk5-v1",
			doctype:          "通知",
			subtype:          "工作通知",
			topK:             5,
			promptTotalChars: 6000,
			contractVersion:  "contract:v2026-06-20-r1",
		},
	}
	run := c05PrivateModelRun{
		id:               "pm-run-notice-001",
		doctype:          "通知",
		modelProfile:     "国产厂商/政务模型/private-gateway",
		streamCompleted:  true,
		subtype:          "工作通知",
		promptVariantID:  "variant:notice-topk3-v1",
		topK:             3,
		promptTotalChars: 6000,
		contractVersion:  "contract:v2026-06-20-r1",
	}
	refs := calibrationEvidenceRefs{
		runIDs:          []string{run.id},
		reviewRecordIDs: []string{"pm-review-notice-001"},
		variantIDs:      []string{"variant:notice-topk3-v1"},
	}
	if err := privateModelDecisionVariantEvidenceError(refs, []c05PrivateModelRun{run}, variants, "通知"); err != nil {
		t.Fatalf("expected matching private model decision variant evidence to pass: %v", err)
	}

	tests := []struct {
		name string
		refs calibrationEvidenceRefs
		runs []c05PrivateModelRun
	}{
		{
			name: "missing variant refs",
			refs: calibrationEvidenceRefs{
				runIDs:          []string{run.id},
				reviewRecordIDs: []string{"pm-review-notice-001"},
			},
			runs: []c05PrivateModelRun{run},
		},
		{
			name: "run variant outside evidence refs",
			refs: calibrationEvidenceRefs{
				runIDs:          []string{run.id},
				reviewRecordIDs: []string{"pm-review-notice-001"},
				variantIDs:      []string{"variant:notice-topk5-v1"},
			},
			runs: []c05PrivateModelRun{run},
		},
		{
			name: "variant ref not backed by a referenced run",
			refs: calibrationEvidenceRefs{
				runIDs:          []string{run.id},
				reviewRecordIDs: []string{"pm-review-notice-001"},
				variantIDs:      []string{"variant:notice-topk3-v1", "variant:notice-topk5-v1"},
			},
			runs: []c05PrivateModelRun{run},
		},
		{
			name: "variant settings mismatch run settings",
			refs: refs,
			runs: []c05PrivateModelRun{{
				id:               "pm-run-notice-001",
				doctype:          "通知",
				modelProfile:     "国产厂商/政务模型/private-gateway",
				streamCompleted:  true,
				subtype:          "工作通知",
				promptVariantID:  "variant:notice-topk3-v1",
				topK:             5,
				promptTotalChars: 6000,
				contractVersion:  "contract:v2026-06-20-r1",
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := privateModelDecisionVariantEvidenceError(tc.refs, tc.runs, variants, "通知"); err == nil {
				t.Fatalf("expected invalid private model decision variant evidence to be rejected")
			}
		})
	}
}

func TestC05XinchuangRuntimePoCEvidenceRowsStayTraceableWhenPresent(t *testing.T) {
	runs := readC05XinchuangRuntimeRuns(t)

	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-xinchuang-runtime-decisions.csv")
	index := csvIndex(header)
	seenDecisionIDs := map[string]bool{}
	hasLoongArchPass := false
	hasARM64KylinPass := false

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		decisionID := requiredCell(t, row, index, "decision_id", rowNumber)
		if seenDecisionIDs[decisionID] {
			t.Fatalf("xinchuang decisions row %d duplicates decision_id %q", rowNumber, decisionID)
		}
		seenDecisionIDs[decisionID] = true
		requireISODateCell(t, row, index, "decision_date", rowNumber)
		platformID := requiredCell(t, row, index, "platform_id", rowNumber)
		cpuArch := requiredCell(t, row, index, "cpu_arch", rowNumber)
		osName := requiredCell(t, row, index, "os_name", rowNumber)
		runID := requiredCell(t, row, index, "run_id", rowNumber)
		endToEndPass, err := strconv.ParseBool(requiredCell(t, row, index, "end_to_end_pass", rowNumber))
		if err != nil {
			t.Fatalf("xinchuang decisions row %d end_to_end_pass = %q, want boolean: %v", rowNumber, row[index["end_to_end_pass"]], err)
		}
		requiredCell(t, row, index, "decision_owner", rowNumber)
		evidenceRefs := parseXinchuangEvidenceRefs(t, requiredCell(t, row, index, "evidence_refs", rowNumber), rowNumber)
		requiredCell(t, row, index, "notes", rowNumber)

		run, ok := runs[runID]
		if !ok {
			t.Fatalf("xinchuang decisions row %d references unknown run_id %q", rowNumber, runID)
		}
		if !evidenceRefs[runID] {
			t.Fatalf("xinchuang decisions row %d evidence_refs must include run:%s", rowNumber, runID)
		}
		if run.platformID != platformID || run.cpuArch != cpuArch || run.osName != osName {
			t.Fatalf("xinchuang decisions row %d references run %q for %s/%s/%s, want %s/%s/%s", rowNumber, runID, run.platformID, run.cpuArch, run.osName, platformID, cpuArch, osName)
		}
		if endToEndPass && !run.endToEndPassed() {
			t.Fatalf("xinchuang decisions row %d marks pass but run %q is not fully connected and streamed", rowNumber, runID)
		}
		if endToEndPass && run.cpuArch == "loongarch64" {
			hasLoongArchPass = true
		}
		if endToEndPass && run.cpuArch == "arm64" && (strings.Contains(strings.ToLower(run.osName), "kylin") || strings.Contains(run.osName, "麒麟")) {
			hasARM64KylinPass = true
		}
	}

	if c05TaskChecked(t, "8.3") {
		if !hasLoongArchPass {
			t.Fatalf("task 8.3 is checked but no passing LoongArch64 runtime decision exists")
		}
		if !hasARM64KylinPass {
			t.Fatalf("task 8.3 is checked but no passing ARM64 + Kylin runtime decision exists")
		}
	}
}

func TestC05PoCEvidenceCSVsDoNotExposeRawArtifactsOrSecrets(t *testing.T) {
	files := []string{
		"c05-high-freq-doctype-private-model-runs.csv",
		"c05-high-freq-doctype-private-model-reviews.csv",
		"c05-high-freq-doctype-private-model-decisions.csv",
		"c05-high-freq-doctype-xinchuang-runtime-runs.csv",
		"c05-high-freq-doctype-xinchuang-runtime-decisions.csv",
	}
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`(?i)[a-z]:[\\/]`),
		regexp.MustCompile(`^(?://|\\\\)`),
		c05RawCorpusReferencePattern,
		regexp.MustCompile(`正文-`),
		regexp.MustCompile(`(?i)(api[_-]?key|sk-[a-z0-9])`),
	}
	rawOfficeExtension := regexp.MustCompile(`(?i)\.(docx?|pdf|xlsx|et)\b`)
	fieldsAllowingSanitizedObjectExtensions := map[string]bool{
		"output_ref":                  true,
		"evidence_ref":                true,
		"evidence_refs":               true,
		"model_endpoint_evidence_ref": true,
		"platform_fingerprint_ref":    true,
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			header, rows := readCalibrationCSV(t, name)
			for rowIndex, row := range append([][]string{header}, rows...) {
				for colIndex, cell := range row {
					for _, pattern := range forbidden {
						if pattern.MatchString(cell) {
							t.Fatalf("%s row %d col %d contains forbidden raw artifact or secret reference %q", name, rowIndex, colIndex, cell)
						}
					}
					field := ""
					if colIndex < len(header) {
						field = header[colIndex]
					}
					if rawOfficeExtension.MatchString(cell) && !fieldsAllowingSanitizedObjectExtensions[field] {
						t.Fatalf("%s row %d field %q contains office/PDF extension outside sanitized evidence reference fields: %q", name, rowIndex, field, cell)
					}
				}
			}
		})
	}
}

type c05PrivateModelRun struct {
	id               string
	doctype          string
	modelProfile     string
	streamCompleted  bool
	subtype          string
	promptVariantID  string
	topK             int
	promptTotalChars int
	contractVersion  string
}

type c05PrivateModelReview struct {
	id               string
	runID            string
	countsAsAdopted  bool
	doctypeNormScore int
	structureScore   int
	directionScore   int
	organToneScore   int
}

type c05XinchuangRuntimeRun struct {
	id                 string
	platformID         string
	cpuArch            string
	osName             string
	postgresConnected  bool
	minioConnected     bool
	c01Connected       bool
	c03Connected       bool
	sseStreamCompleted bool
}

func readC05PrivateModelRuns(t *testing.T) map[string]c05PrivateModelRun {
	t.Helper()
	readyC03Queries := readReadyC05CalibrationCandidateQueries(t)
	variants := readC05CalibrationVariants(t)
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-private-model-runs.csv")
	index := csvIndex(header)
	runs := map[string]c05PrivateModelRun{}
	seenRunIDs := map[string]bool{}
	allowedSecurityLevels := map[string]bool{"非密": true, "敏感": true, "涉密": true}
	allowedDeploymentScopes := map[string]bool{"private": true, "domestic": true, "xinchuang_private": true}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		runID := requiredCell(t, row, index, "run_id", rowNumber)
		if seenRunIDs[runID] {
			t.Fatalf("private model runs row %d duplicates run_id %q", rowNumber, runID)
		}
		seenRunIDs[runID] = true
		requireISODateCell(t, row, index, "run_date", rowNumber)
		doctype := requiredCell(t, row, index, "doctype", rowNumber)
		requireKnownC05Doctype(t, doctype, "private model runs", rowNumber)
		for _, field := range []string{"subtype", "model_provider", "model_name", "model_backend", "model_endpoint_evidence_ref", "prompt_variant_id", "contract_version"} {
			requireNoSyntheticPoCEvidence(t, requiredCell(t, row, index, field, rowNumber), "private model runs", field, rowNumber)
		}
		if scope := row[index["deployment_scope"]]; !allowedDeploymentScopes[scope] {
			t.Fatalf("private model runs row %d deployment_scope = %q, want private/domestic/xinchuang_private", rowNumber, scope)
		}
		if securityLevel := row[index["content_security_level"]]; !allowedSecurityLevels[securityLevel] {
			t.Fatalf("private model runs row %d content_security_level = %q, want 非密/敏感/涉密", rowNumber, securityLevel)
		}
		c03QueryID := requiredCell(t, row, index, "c03_query_id", rowNumber)
		if strings.EqualFold(c03QueryID, "pending") || c05RawCorpusReferencePattern.MatchString(c03QueryID) {
			t.Fatalf("private model runs row %d c03_query_id = %q, want c03 retrieval evidence", rowNumber, c03QueryID)
		} else {
			requireNoSyntheticPoCEvidence(t, c03QueryID, "private model runs", "c03_query_id", rowNumber)
			requireReadyC05CalibrationCandidateQuery(t, readyC03Queries, doctype, c03QueryID, "private model runs", rowNumber)
		}
		topK := requirePositiveIntCell(t, row, index, "topk", rowNumber)
		promptTotalChars := requirePositiveIntCell(t, row, index, "prompt_total_chars", rowNumber)
		requireMatchingC05PrivateModelCalibrationVariant(t, variants, calibrationVariant{
			id:               row[index["prompt_variant_id"]],
			doctype:          doctype,
			subtype:          row[index["subtype"]],
			topK:             topK,
			promptTotalChars: promptTotalChars,
			contractVersion:  row[index["contract_version"]],
		}, rowNumber)
		streamCompleted, err := strconv.ParseBool(requiredCell(t, row, index, "stream_completed", rowNumber))
		if err != nil {
			t.Fatalf("private model runs row %d stream_completed = %q, want boolean: %v", rowNumber, row[index["stream_completed"]], err)
		}
		if streamCompleted {
			for _, field := range []string{"first_token_ms", "total_generation_ms", "completion_chars"} {
				requirePositiveIntCell(t, row, index, field, rowNumber)
			}
			if errorReason := strings.TrimSpace(row[index["error_reason"]]); errorReason != "" {
				t.Fatalf("private model runs row %d succeeded but error_reason = %q", rowNumber, errorReason)
			}
			requireNoSyntheticPoCEvidence(t, requiredCell(t, row, index, "output_ref", rowNumber), "private model runs", "output_ref", rowNumber)
		} else {
			requiredCell(t, row, index, "error_reason", rowNumber)
			for _, field := range []string{"first_token_ms", "total_generation_ms", "completion_chars"} {
				requireNonNegativeIntCell(t, row, index, field, rowNumber)
			}
		}

		runs[runID] = c05PrivateModelRun{
			id:               runID,
			doctype:          doctype,
			modelProfile:     row[index["model_provider"]] + "/" + row[index["model_name"]] + "/" + row[index["model_backend"]],
			streamCompleted:  streamCompleted,
			subtype:          strings.TrimSpace(row[index["subtype"]]),
			promptVariantID:  strings.TrimSpace(row[index["prompt_variant_id"]]),
			topK:             topK,
			promptTotalChars: promptTotalChars,
			contractVersion:  strings.TrimSpace(row[index["contract_version"]]),
		}
	}
	return runs
}

func readC05PrivateModelReviews(t *testing.T, runs map[string]c05PrivateModelRun) map[string]c05PrivateModelReview {
	t.Helper()
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-private-model-reviews.csv")
	index := csvIndex(header)
	reviews := map[string]c05PrivateModelReview{}
	seenReviewIDs := map[string]bool{}
	allowedAdoptionStatus := map[string]bool{"直接用": true, "小改": true, "大改": true, "弃用": true}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		reviewID := requiredCell(t, row, index, "review_record_id", rowNumber)
		if seenReviewIDs[reviewID] {
			t.Fatalf("private model reviews row %d duplicates review_record_id %q", rowNumber, reviewID)
		}
		seenReviewIDs[reviewID] = true
		runID := requiredCell(t, row, index, "run_id", rowNumber)
		if _, ok := runs[runID]; !ok {
			t.Fatalf("private model reviews row %d references unknown run_id %q", rowNumber, runID)
		}
		requiredCell(t, row, index, "reviewer", rowNumber)
		requireISODateCell(t, row, index, "review_date", rowNumber)
		doctypeNormScore := requireRubricScoreCell(t, row, index, "doctype_norm_score", rowNumber)
		structureScore := requireRubricScoreCell(t, row, index, "structure_score", rowNumber)
		directionScore := requireRubricScoreCell(t, row, index, "direction_score", rowNumber)
		organToneScore := requireRubricScoreCell(t, row, index, "organ_tone_score", rowNumber)
		adoptionStatus := row[index["adoption_status"]]
		if !allowedAdoptionStatus[adoptionStatus] {
			t.Fatalf("private model reviews row %d adoption_status = %q, want known adoption status", rowNumber, adoptionStatus)
		}
		countsAsAdopted, err := strconv.ParseBool(requiredCell(t, row, index, "counts_as_adopted", rowNumber))
		if err != nil {
			t.Fatalf("private model reviews row %d counts_as_adopted = %q, want boolean: %v", rowNumber, row[index["counts_as_adopted"]], err)
		}
		wantAdopted := adoptionStatus == "直接用" || adoptionStatus == "小改"
		if countsAsAdopted != wantAdopted {
			t.Fatalf("private model reviews row %d counts_as_adopted = %v, want %v for %q", rowNumber, countsAsAdopted, wantAdopted, adoptionStatus)
		}
		if _, err := strconv.ParseBool(requiredCell(t, row, index, "meets_like_govdoc", rowNumber)); err != nil {
			t.Fatalf("private model reviews row %d meets_like_govdoc = %q, want boolean: %v", rowNumber, row[index["meets_like_govdoc"]], err)
		}
		reviews[reviewID] = c05PrivateModelReview{
			id:               reviewID,
			runID:            runID,
			countsAsAdopted:  countsAsAdopted,
			doctypeNormScore: doctypeNormScore,
			structureScore:   structureScore,
			directionScore:   directionScore,
			organToneScore:   organToneScore,
		}
	}
	return reviews
}

func requireMatchingC05PrivateModelCalibrationVariant(t *testing.T, variants map[string]calibrationVariant, run calibrationVariant, rowNumber int) {
	t.Helper()
	if !hasMatchingC05PrivateModelCalibrationVariant(variants, run) {
		t.Fatalf("private model runs row %d prompt_variant_id = %q does not match registered calibration variant settings", rowNumber, run.id)
	}
}

func hasMatchingC05PrivateModelCalibrationVariant(variants map[string]calibrationVariant, run calibrationVariant) bool {
	variant, ok := variants[strings.TrimSpace(run.id)]
	if !ok {
		return false
	}
	return variant.doctype == run.doctype &&
		variant.subtype == strings.TrimSpace(run.subtype) &&
		variant.topK == run.topK &&
		variant.promptTotalChars == run.promptTotalChars &&
		variant.contractVersion == strings.TrimSpace(run.contractVersion)
}

func privateModelDecisionVariantEvidenceError(refs calibrationEvidenceRefs, runs []c05PrivateModelRun, variants map[string]calibrationVariant, doctype string) error {
	if len(refs.variantIDs) == 0 {
		return fmt.Errorf("evidence_refs must include variant:<id> refs")
	}

	referencedVariantIDs := map[string]bool{}
	for _, variantID := range refs.variantIDs {
		variant, ok := variants[variantID]
		if !ok {
			return fmt.Errorf("references unknown variant %s", variantID)
		}
		if variant.doctype != doctype {
			return fmt.Errorf("variant %s belongs to doctype %s, want %s", variantID, variant.doctype, doctype)
		}
		referencedVariantIDs[variantID] = true
	}

	runVariantIDs := map[string]bool{}
	for _, run := range runs {
		variantID := strings.TrimSpace(run.promptVariantID)
		if variantID == "" {
			return fmt.Errorf("run %s has no prompt_variant_id", run.id)
		}
		if !referencedVariantIDs[variantID] {
			return fmt.Errorf("run %s uses prompt_variant_id %s outside evidence_refs", run.id, variantID)
		}
		variant := variants[variantID]
		if variant.doctype != run.doctype ||
			variant.subtype != strings.TrimSpace(run.subtype) ||
			variant.topK != run.topK ||
			variant.promptTotalChars != run.promptTotalChars ||
			variant.contractVersion != strings.TrimSpace(run.contractVersion) {
			return fmt.Errorf("run %s prompt_variant_id %s does not match registered variant settings", run.id, variantID)
		}
		runVariantIDs[variantID] = true
	}

	for _, variantID := range refs.variantIDs {
		if !runVariantIDs[variantID] {
			return fmt.Errorf("variant %s is not covered by referenced private model runs", variantID)
		}
	}
	return nil
}

func readC05XinchuangRuntimeRuns(t *testing.T) map[string]c05XinchuangRuntimeRun {
	t.Helper()
	header, rows := readCalibrationCSV(t, "c05-high-freq-doctype-xinchuang-runtime-runs.csv")
	index := csvIndex(header)
	runs := map[string]c05XinchuangRuntimeRun{}
	seenRunIDs := map[string]bool{}
	allowedArch := map[string]bool{"loongarch64": true, "arm64": true}

	for rowIndex, row := range rows {
		rowNumber := rowIndex + 2
		runID := requiredCell(t, row, index, "run_id", rowNumber)
		if seenRunIDs[runID] {
			t.Fatalf("xinchuang runs row %d duplicates run_id %q", rowNumber, runID)
		}
		seenRunIDs[runID] = true
		requireISODateCell(t, row, index, "run_date", rowNumber)
		for _, field := range []string{"platform_id", "cpu_arch", "os_name", "os_version", "kernel_version", "go_version", "binary_ref", "runtime_mode", "platform_fingerprint_ref", "evidence_ref", "operator", "notes"} {
			requiredCell(t, row, index, field, rowNumber)
		}
		if !allowedArch[row[index["cpu_arch"]]] {
			t.Fatalf("xinchuang runs row %d cpu_arch = %q, want loongarch64 or arm64", rowNumber, row[index["cpu_arch"]])
		}
		if runtimeMode := row[index["runtime_mode"]]; runtimeMode != "target_host" {
			t.Fatalf("xinchuang runs row %d runtime_mode = %q, want target_host", rowNumber, runtimeMode)
		}
		for _, field := range []string{"binary_ref", "platform_fingerprint_ref", "evidence_ref", "notes"} {
			requireNoCrossCompileOnlyEvidence(t, row[index[field]], "xinchuang runs", field, rowNumber)
		}
		postgresConnected := requireBoolCell(t, row, index, "postgres_connected", rowNumber)
		minioConnected := requireBoolCell(t, row, index, "minio_connected", rowNumber)
		c01Connected := requireBoolCell(t, row, index, "c01_connected", rowNumber)
		c03Connected := requireBoolCell(t, row, index, "c03_connected", rowNumber)
		sseStreamCompleted := requireBoolCell(t, row, index, "sse_stream_completed", rowNumber)
		if postgresConnected && minioConnected && c01Connected && c03Connected && sseStreamCompleted {
			requirePositiveIntCell(t, row, index, "first_token_ms", rowNumber)
			requirePositiveIntCell(t, row, index, "total_generation_ms", rowNumber)
			if errorReason := strings.TrimSpace(row[index["error_reason"]]); errorReason != "" {
				t.Fatalf("xinchuang runs row %d passed but error_reason = %q", rowNumber, errorReason)
			}
		} else {
			requiredCell(t, row, index, "error_reason", rowNumber)
			requireNonNegativeIntCell(t, row, index, "first_token_ms", rowNumber)
			requireNonNegativeIntCell(t, row, index, "total_generation_ms", rowNumber)
		}
		runs[runID] = c05XinchuangRuntimeRun{
			id:                 runID,
			platformID:         row[index["platform_id"]],
			cpuArch:            row[index["cpu_arch"]],
			osName:             row[index["os_name"]],
			postgresConnected:  postgresConnected,
			minioConnected:     minioConnected,
			c01Connected:       c01Connected,
			c03Connected:       c03Connected,
			sseStreamCompleted: sseStreamCompleted,
		}
	}
	return runs
}

func (run c05XinchuangRuntimeRun) endToEndPassed() bool {
	return run.postgresConnected && run.minioConnected && run.c01Connected && run.c03Connected && run.sseStreamCompleted
}

func requireRubricScoreCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int) int {
	t.Helper()
	score := requirePositiveIntCell(t, row, index, field, rowNumber)
	if score > 5 {
		t.Fatalf("row %d field %s = %d, want 1-5", rowNumber, field, score)
	}
	return score
}

func requireFloatRangeCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int, min float64, max float64) float64 {
	t.Helper()
	value := requiredCell(t, row, index, field, rowNumber)
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		t.Fatalf("row %d field %s = %q, want number: %v", rowNumber, field, value, err)
	}
	if parsed < min || parsed > max {
		t.Fatalf("row %d field %s = %.4f, want %.4f..%.4f", rowNumber, field, parsed, min, max)
	}
	return parsed
}

func requireNoSyntheticPoCEvidence(t *testing.T, value, source, field string, rowNumber int) {
	t.Helper()
	if looksSyntheticPoCEvidence(value) {
		t.Fatalf("%s row %d field %s = %q looks like local/fake evidence, want real private/domestic model evidence", source, rowNumber, field, value)
	}
}

func requireNoCrossCompileOnlyEvidence(t *testing.T, value, source, field string, rowNumber int) {
	t.Helper()
	if looksCrossCompileOnlyEvidence(value) {
		t.Fatalf("%s row %d field %s = %q looks like cross-compile/local-host evidence, want target platform runtime evidence", source, rowNumber, field, value)
	}
}

func looksSyntheticPoCEvidence(value string) bool {
	return c05SyntheticPoCEvidencePattern.MatchString(value)
}

func looksCrossCompileOnlyEvidence(value string) bool {
	return c05CrossCompileOnlyEvidenceRegex.MatchString(value)
}

func requireBoolCell(t *testing.T, row []string, index map[string]int, field string, rowNumber int) bool {
	t.Helper()
	value, err := strconv.ParseBool(requiredCell(t, row, index, field, rowNumber))
	if err != nil {
		t.Fatalf("row %d field %s = %q, want boolean: %v", rowNumber, field, row[index[field]], err)
	}
	return value
}

func parseXinchuangEvidenceRefs(t *testing.T, value string, rowNumber int) map[string]bool {
	t.Helper()
	refs := map[string]bool{}
	for _, token := range strings.Split(value, ";") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if !strings.HasPrefix(token, "run:") {
			t.Fatalf("xinchuang decisions row %d evidence_refs token %q must use run:<id>", rowNumber, token)
		}
		runID := strings.TrimSpace(strings.TrimPrefix(token, "run:"))
		if runID == "" {
			t.Fatalf("xinchuang decisions row %d has empty run evidence ref", rowNumber)
		}
		refs[runID] = true
	}
	return refs
}

func privateModelAdoptionRate(reviews []c05PrivateModelReview) float64 {
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

func privateModelAverageRubricScore(reviews []c05PrivateModelReview) float64 {
	if len(reviews) == 0 {
		return 0
	}
	sum := 0
	for _, review := range reviews {
		sum += review.doctypeNormScore + review.structureScore + review.directionScore + review.organToneScore
	}
	return float64(sum) / float64(len(reviews)*4)
}
