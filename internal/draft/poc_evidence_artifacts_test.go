package draft

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestC05PoCEvidenceCSVHeadersStayAuditable(t *testing.T) {
	tests := map[string][]string{
		"c05-high-freq-doctype-private-model-runs.csv": {
			"run_id", "run_date", "doctype", "subtype", "model_provider", "model_name",
			"model_backend", "deployment_scope", "content_security_level", "c03_query_id",
			"prompt_variant_id", "topk", "prompt_total_chars", "contract_version",
			"first_token_ms", "total_generation_ms", "completion_chars",
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
			"kernel_version", "go_version", "binary_ref", "postgres_connected",
			"minio_connected", "c01_connected", "c03_connected", "sse_stream_completed",
			"first_token_ms", "total_generation_ms", "error_reason", "evidence_ref",
			"operator", "notes",
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

func TestC05PrivateModelPoCEvidenceRowsStayTraceableWhenPresent(t *testing.T) {
	runs := readC05PrivateModelRuns(t)
	reviews := readC05PrivateModelReviews(t, runs)

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
			}
			if runCount != len(referencedRuns) {
				t.Fatalf("private model decisions row %d run_count = %d, want %d referenced unique runs", rowNumber, runCount, len(referencedRuns))
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
		regexp.MustCompile(`(^|[\\/])各类文件[\\/]`),
		regexp.MustCompile(`正文-`),
		regexp.MustCompile(`(?i)(api[_-]?key|sk-[a-z0-9])`),
	}
	rawOfficeExtension := regexp.MustCompile(`(?i)\.(docx?|pdf|xlsx|et)\b`)
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
					if rawOfficeExtension.MatchString(cell) {
						t.Fatalf("%s row %d col %d contains raw office/PDF artifact extension %q", name, rowIndex, colIndex, cell)
					}
				}
			}
		})
	}
}

type c05PrivateModelRun struct {
	id              string
	doctype         string
	modelProfile    string
	streamCompleted bool
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
		for _, field := range []string{"subtype", "model_provider", "model_name", "model_backend", "prompt_variant_id", "contract_version"} {
			requiredCell(t, row, index, field, rowNumber)
		}
		if scope := row[index["deployment_scope"]]; !allowedDeploymentScopes[scope] {
			t.Fatalf("private model runs row %d deployment_scope = %q, want private/domestic/xinchuang_private", rowNumber, scope)
		}
		if securityLevel := row[index["content_security_level"]]; !allowedSecurityLevels[securityLevel] {
			t.Fatalf("private model runs row %d content_security_level = %q, want 非密/敏感/涉密", rowNumber, securityLevel)
		}
		if c03QueryID := row[index["c03_query_id"]]; strings.EqualFold(c03QueryID, "pending") || strings.Contains(c03QueryID, "各类文件") {
			t.Fatalf("private model runs row %d c03_query_id = %q, want c03 retrieval evidence", rowNumber, c03QueryID)
		}
		for _, field := range []string{"topk", "prompt_total_chars"} {
			requirePositiveIntCell(t, row, index, field, rowNumber)
		}
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
			requiredCell(t, row, index, "output_ref", rowNumber)
		} else {
			requiredCell(t, row, index, "error_reason", rowNumber)
			for _, field := range []string{"first_token_ms", "total_generation_ms", "completion_chars"} {
				requireNonNegativeIntCell(t, row, index, field, rowNumber)
			}
		}

		runs[runID] = c05PrivateModelRun{
			id:              runID,
			doctype:         doctype,
			modelProfile:    row[index["model_provider"]] + "/" + row[index["model_name"]] + "/" + row[index["model_backend"]],
			streamCompleted: streamCompleted,
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
		for _, field := range []string{"platform_id", "cpu_arch", "os_name", "os_version", "kernel_version", "go_version", "binary_ref", "evidence_ref", "operator", "notes"} {
			requiredCell(t, row, index, field, rowNumber)
		}
		if !allowedArch[row[index["cpu_arch"]]] {
			t.Fatalf("xinchuang runs row %d cpu_arch = %q, want loongarch64 or arm64", rowNumber, row[index["cpu_arch"]])
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
