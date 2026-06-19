package draft

import (
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	retrievalcontract "github.com/tajiaoyezi/GovScribe/internal/rag/retrieval/contract"
)

func TestAssembleFewShotPromptPartitionsC03ExamplesAndScenario(t *testing.T) {
	result, err := AssembleFewShotPrompt(FewShotInput{
		Doctype:          "通知",
		Subtype:          "召开会议",
		SceneDescription: "通知各部门周五召开安全生产会议",
		MaxExamples:      1,
		C03RetrievedExamples: []retrievalcontract.TemplateExample{
			{
				ChunkID:          "c1",
				DocumentID:       "doc-1",
				Text:             "范文一：关于召开安全生产会议的通知",
				DocumentType:     "通知",
				DocumentNumber:   "政办发〔2024〕1号",
				OrganizationName: "XX市人民政府办公室",
				Score:            0.91,
			},
			{
				ChunkID:      "c2",
				DocumentID:   "doc-2",
				Text:         "范文二：关于召开经济运行会的通知",
				DocumentType: "通知",
				Score:        0.88,
			},
			{
				ChunkID:      "c3",
				DocumentID:   "doc-3",
				Text:         "错误文种：工作情况报告",
				DocumentType: "报告",
				Score:        0.93,
			},
		},
		FilledSlots: map[doctype.RequiredSlot]string{
			doctype.SlotIssuer:    "市政府办公室",
			doctype.SlotRecipient: "各区县政府",
			doctype.SlotSubject:   "安全生产会议",
		},
		MissingSlots: []doctype.RequiredSlot{doctype.SlotTimePlace},
	})
	if err != nil {
		t.Fatalf("assemble few-shot prompt: %v", err)
	}
	if len(result.Examples) != 1 {
		t.Fatalf("examples len = %d, want TopK-limited 1", len(result.Examples))
	}
	if result.Examples[0].ChunkID != "c1" {
		t.Fatalf("selected example = %#v, want first matching c03 example c1", result.Examples[0])
	}

	content := result.Content
	scenarioSection := strings.Index(content, "## 待写场景要素")
	examplesSection := strings.Index(content, "## Few-shot 范文样例")
	if scenarioSection < 0 || examplesSection < 0 || scenarioSection > examplesSection {
		t.Fatalf("prompt must partition scenario before examples:\n%s", content)
	}
	for _, want := range []string{
		"来源：c03 corpus-rag-retrieval",
		"TopK 上限：1",
		"目标文种：通知",
		"代表子类：召开会议",
		"通知各部门周五召开安全生产会议",
		"发文单位=市政府办公室",
		"主送机关=各区县政府",
		"事由=安全生产会议",
		"缺失 / 未确认要素：关键时间地点",
		"范文一：关于召开安全生产会议的通知",
		"政办发〔2024〕1号",
		"XX市人民政府办公室",
		"同文种；优先同子类",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt missing %q:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{"范文二：关于召开经济运行会的通知", "错误文种：工作情况报告"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("prompt contains non-selected example %q:\n%s", forbidden, content)
		}
	}
}

func TestAssembleFewShotPromptPassesC03ExampleTextVerbatim(t *testing.T) {
	c03Text := "  脱敏范文原文：请[单位A]于[日期]前反馈。\n第二段保留  空格、占位符[姓名B]与标点；不得被改写。  "

	result, err := AssembleFewShotPrompt(FewShotInput{
		Doctype:     "通知",
		MaxExamples: 1,
		C03RetrievedExamples: []retrievalcontract.TemplateExample{
			{ChunkID: "c1", Text: c03Text, DocumentType: "通知"},
		},
	})
	if err != nil {
		t.Fatalf("assemble few-shot prompt: %v", err)
	}
	if len(result.Examples) != 1 || result.Examples[0].Text != c03Text {
		t.Fatalf("selected example text = %#v, want verbatim c03 text %q", result.Examples, c03Text)
	}

	wantBlock := "脱敏后样例文本（逐字透传）：\n" + c03Text + "\n"
	if !strings.Contains(result.Content, wantBlock) {
		t.Fatalf("prompt must include c03 text verbatim after pass-through marker.\nwant block:\n%q\nprompt:\n%s", wantBlock, result.Content)
	}
}

func TestAssembleFewShotPromptDoesNotInventExamplesWhenC03ReturnsNone(t *testing.T) {
	result, err := AssembleFewShotPrompt(FewShotInput{
		Doctype:              "通知",
		SceneDescription:     "通知各部门开会",
		MaxExamples:          2,
		C03RetrievedExamples: nil,
	})
	if err != nil {
		t.Fatalf("assemble few-shot prompt: %v", err)
	}
	if len(result.Examples) != 0 {
		t.Fatalf("examples = %#v, want none when c03 returns none", result.Examples)
	}
	for _, forbidden := range []string{"范文一：", "示例正文", "参考范文", "模拟范文"} {
		if strings.Contains(result.Content, forbidden) {
			t.Fatalf("prompt invented example marker %q:\n%s", forbidden, result.Content)
		}
	}
}

func TestAssembleFewShotPromptMarksInsufficientExamplesAndKeepsStructureContract(t *testing.T) {
	contract := defaultStructureContractForFewShotTest(t, "通知")

	result, err := AssembleFewShotPrompt(FewShotInput{
		Doctype:                   "通知",
		Subtype:                   "召开会议",
		SceneDescription:          "通知各部门周五召开安全生产会议",
		MaxExamples:               3,
		MinimumSufficientExamples: 2,
		StructureContract:         contract,
		C03RetrievedExamples: []retrievalcontract.TemplateExample{
			{ChunkID: "c1", Text: "关于召开安全生产会议的通知", DocumentType: "通知"},
		},
	})
	if err != nil {
		t.Fatalf("assemble few-shot prompt: %v", err)
	}
	if !result.Metadata.InsufficientExamples {
		t.Fatalf("metadata insufficient examples = false, want true")
	}
	if result.Metadata.Warning != "范文样例不足、质量可能下降" {
		t.Fatalf("metadata warning = %q, want insufficient example warning", result.Metadata.Warning)
	}
	if !result.Metadata.StructureContractApplied {
		t.Fatalf("metadata structure contract applied = false, want true")
	}
	if result.Metadata.ExampleCount != 1 || result.Metadata.MinimumSufficientExamples != 2 {
		t.Fatalf("metadata counts = %#v, want example count 1 / sufficient threshold 2", result.Metadata)
	}

	content := result.Content
	for _, want := range []string{
		"## 结构契约",
		"标题构成：关于 + 事由 + 通知",
		"正文段落结构：开头说明通知缘由；主体列明事项、时间、地点、要求；结尾提出执行或反馈要求",
		"范文样例不足、质量可能下降",
		"## Few-shot 范文样例",
		"关于召开安全生产会议的通知",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt missing %q:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{"通用兜底", "移交 c07", "fallback"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("prompt must not silently fallback via %q:\n%s", forbidden, content)
		}
	}
}

func TestAssembleFewShotPromptZeroExamplesStillAppliesStructureContract(t *testing.T) {
	contract := defaultStructureContractForFewShotTest(t, "请示")

	result, err := AssembleFewShotPrompt(FewShotInput{
		Doctype:                   "请示",
		SceneDescription:          "申请拨付专项经费",
		MaxExamples:               3,
		MinimumSufficientExamples: 1,
		StructureContract:         contract,
		C03RetrievedExamples:      nil,
	})
	if err != nil {
		t.Fatalf("assemble few-shot prompt: %v", err)
	}
	if len(result.Examples) != 0 {
		t.Fatalf("examples = %#v, want none when c03 returns zero examples", result.Examples)
	}
	if !result.Metadata.InsufficientExamples || result.Metadata.Warning != "范文样例不足、质量可能下降" {
		t.Fatalf("metadata = %#v, want insufficient example warning", result.Metadata)
	}
	if !result.Metadata.StructureContractApplied {
		t.Fatalf("metadata structure contract applied = false, want true")
	}

	content := result.Content
	for _, want := range []string{
		"## 结构契约",
		"标题构成：关于 + 事由 + 的请示",
		"结束语约束：妥否，请批示。",
		"无可注入样例。",
		"范文样例不足、质量可能下降",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("prompt missing %q:\n%s", want, content)
		}
	}
	for _, forbidden := range []string{"通用兜底", "移交 c07", "fallback"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("prompt must not silently fallback via %q:\n%s", forbidden, content)
		}
	}
}

func defaultStructureContractForFewShotTest(t *testing.T, doctypeName string) StructureContract {
	t.Helper()
	for _, contract := range DefaultStructureContracts() {
		if contract.Doctype == doctypeName {
			return contract
		}
	}
	t.Fatalf("missing default structure contract for %s", doctypeName)
	return StructureContract{}
}
