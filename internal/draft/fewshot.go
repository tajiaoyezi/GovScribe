package draft

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
	"github.com/tajiaoyezi/GovScribe/internal/rag/retrieval"
)

const DefaultFewShotTopK = 3

type FewShotInput struct {
	Doctype              string
	Subtype              string
	SceneDescription     string
	FilledSlots          map[doctype.RequiredSlot]string
	MissingSlots         []doctype.RequiredSlot
	MaxExamples          int
	C03RetrievedExamples []retrieval.TemplateExample
}

type FewShotPrompt struct {
	Content  string
	Examples []retrieval.TemplateExample
}

func AssembleFewShotPrompt(input FewShotInput) (FewShotPrompt, error) {
	topK := input.MaxExamples
	if topK <= 0 {
		topK = DefaultFewShotTopK
	}
	examples := selectFewShotExamples(input.C03RetrievedExamples, input.Doctype, topK)

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

	b.WriteString("\n\n## Few-shot 范文样例\n")
	b.WriteString("来源：c03 corpus-rag-retrieval\n")
	b.WriteString("TopK 上限：")
	b.WriteString(strconv.Itoa(topK))
	b.WriteString("\n取用规则：同文种；优先同子类；仅注入 c03 返回的脱敏范文样例。\n")
	if len(examples) == 0 {
		b.WriteString("无可注入样例。\n")
		return FewShotPrompt{Content: b.String()}, nil
	}
	for i, example := range examples {
		fmt.Fprintf(&b, "\n### 样例 %d\n", i+1)
		writeExampleLine(&b, "chunk_id", example.ChunkID)
		writeExampleLine(&b, "document_id", example.DocumentID)
		writeExampleLine(&b, "文种", example.DocumentType)
		writeExampleLine(&b, "文号", example.DocumentNumber)
		writeExampleLine(&b, "来源单位", example.OrganizationName)
		fmt.Fprintf(&b, "相似度：%.2f\n", example.Score)
		b.WriteString("正文片段：\n")
		b.WriteString(example.Text)
		b.WriteString("\n")
	}
	return FewShotPrompt{Content: b.String(), Examples: examples}, nil
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
