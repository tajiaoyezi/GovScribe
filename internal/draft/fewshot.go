package draft

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/rag/retrieval"
)

const DefaultFewShotTopK = 3
const InsufficientFewShotWarning = "范文样例不足、质量可能下降"

type FewShotInput struct {
	Doctype                   string
	Subtype                   string
	SceneDescription          string
	FilledSlots               map[doctype.RequiredSlot]string
	MissingSlots              []doctype.RequiredSlot
	MaxExamples               int
	MinimumSufficientExamples int
	C03InsufficientExamples   bool
	StructureContract         StructureContract
	C03RetrievedExamples      []retrieval.TemplateExample
}

type FewShotMetadata struct {
	InsufficientExamples      bool
	Warning                   string
	StructureContractApplied  bool
	ExampleCount              int
	MinimumSufficientExamples int
}

type FewShotPrompt struct {
	Content  string
	Examples []retrieval.TemplateExample
	Metadata FewShotMetadata
}

func AssembleFewShotPrompt(input FewShotInput) (FewShotPrompt, error) {
	topK := input.MaxExamples
	if topK <= 0 {
		topK = DefaultFewShotTopK
	}
	examples := selectFewShotExamples(input.C03RetrievedExamples, input.Doctype, topK)
	metadata := FewShotMetadata{
		StructureContractApplied:  hasStructureContract(input.StructureContract),
		ExampleCount:              len(examples),
		MinimumSufficientExamples: input.MinimumSufficientExamples,
	}
	if input.C03InsufficientExamples || (input.MinimumSufficientExamples > 0 && len(examples) < input.MinimumSufficientExamples) {
		metadata.InsufficientExamples = true
		metadata.Warning = InsufficientFewShotWarning
	}

	var b strings.Builder
	b.WriteString("## 待写场景要素\n")
	b.WriteString("目标文种：")
	b.WriteString(strings.TrimSpace(input.Doctype))
	b.WriteString("\n代表子类：")
	b.WriteString(strings.TrimSpace(input.Subtype))
	b.WriteString("\n用户原始场景描述：")
	b.WriteString(strings.TrimSpace(input.SceneDescription))
	b.WriteString("\n已补齐要素：")
	b.WriteString(renderSlots(input.FilledSlots))
	b.WriteString("\n缺失 / 未确认要素：")
	b.WriteString(renderMissingSlots(input.MissingSlots))

	if metadata.StructureContractApplied {
		writeStructureContractSection(&b, input.StructureContract)
	}
	if metadata.InsufficientExamples {
		b.WriteString("\n\n## 样例状态\n")
		b.WriteString(metadata.Warning)
		b.WriteString("\n")
	}

	b.WriteString("\n\n## Few-shot 范文样例\n")
	b.WriteString("来源：c03 corpus-rag-retrieval\n")
	b.WriteString("TopK 上限：")
	b.WriteString(strconv.Itoa(topK))
	b.WriteString("\n取用规则：同文种；优先同子类；仅注入 c03 返回的脱敏范文样例。\n")
	if len(examples) == 0 {
		b.WriteString("无可注入样例。\n")
		return FewShotPrompt{Content: b.String(), Examples: examples, Metadata: metadata}, nil
	}
	for i, example := range examples {
		fmt.Fprintf(&b, "\n### 样例 %d\n", i+1)
		writeExampleLine(&b, "chunk_id", example.ChunkID)
		writeExampleLine(&b, "document_id", example.DocumentID)
		writeExampleLine(&b, "文种", example.DocumentType)
		writeExampleLine(&b, "文号", example.DocumentNumber)
		writeExampleLine(&b, "来源单位", example.OrganizationName)
		fmt.Fprintf(&b, "相似度：%.2f\n", example.Score)
		b.WriteString("脱敏后样例文本（逐字透传）：\n")
		b.WriteString(example.Text)
		b.WriteString("\n")
	}
	return FewShotPrompt{Content: b.String(), Examples: examples, Metadata: metadata}, nil
}

func selectFewShotExamples(examples []retrieval.TemplateExample, doctypeName string, topK int) []retrieval.TemplateExample {
	targetDoctype := strings.TrimSpace(doctypeName)
	out := make([]retrieval.TemplateExample, 0, min(topK, len(examples)))
	for _, example := range examples {
		if len(out) >= topK {
			break
		}
		if strings.TrimSpace(example.Text) == "" {
			continue
		}
		if strings.TrimSpace(example.DocumentType) != targetDoctype {
			continue
		}
		out = append(out, example)
	}
	return out
}

func writeExampleLine(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString(label)
	b.WriteString("：")
	b.WriteString(value)
	b.WriteString("\n")
}

func hasStructureContract(contract StructureContract) bool {
	return strings.TrimSpace(contract.Doctype) != "" ||
		strings.TrimSpace(contract.TitleRule) != "" ||
		len(contract.BodyStructure) > 0
}

func writeStructureContractSection(b *strings.Builder, contract StructureContract) {
	b.WriteString("\n\n## 结构契约\n")
	writeContractLine(b, "文种", contract.Doctype)
	if strings.TrimSpace(string(contract.Direction)) != "" {
		writeContractLine(b, "行文关系", string(contract.Direction))
	}
	writeContractLine(b, "标题构成", contract.TitleRule)
	writeContractLine(b, "称谓", contract.SalutationRule)
	writeContractLine(b, "主送机关", contract.RecipientRule)
	if len(contract.BodyStructure) > 0 {
		writeContractLine(b, "正文段落结构", strings.Join(contract.BodyStructure, "；"))
	}
	if len(contract.RequiredSlots) > 0 {
		writeContractLine(b, "必备要素", strings.Join(requiredSlotsToStrings(contract.RequiredSlots), "、"))
	}
	writeContractLine(b, "落款", contract.SignatureRule)
	if len(contract.ToneRules) > 0 {
		writeContractLine(b, "口吻指令", strings.Join(contract.ToneRules, "；"))
	}
	writeContractLine(b, "结束语约束", contract.ClosingRule)
	if len(contract.RedlineRules) > 0 {
		writeContractLine(b, "机关口径红线", strings.Join(contract.RedlineRules, "；"))
	}
}

func writeContractLine(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString(label)
	b.WriteString("：")
	b.WriteString(value)
	b.WriteString("\n")
}
