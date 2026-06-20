package draft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestC05BoundaryRegistryPinsReturnFlowAndOnlyOfficeOwnership(t *testing.T) {
	doc := readRepoText(t, "docs", "other", "c05-high-freq-doctype-boundary-registry.md")
	required := []string{
		"c05 仅交付高频文种初稿生成接口、结构契约、few-shot 提示编排与 SSE 流式契约",
		"在线编辑 / 核稿",
		"采纳稿回流信号产出",
		"OnlyOffice 集成与 OnlyOffice 信创认证均不由 c05 承担",
		"c08 对计入采纳的成稿产出回流信号",
		"c03 单点消费 c08 回流信号",
		"c05 不直调 c03 入库 API",
		"c05 不解决 OnlyOffice 授权、去品牌、SLA、统信 / 龙芯认证",
		"麒麟有企业版认证，统信 / 龙芯待书面确认",
		"商务 / 法务 / 信创阶段承接",
	}
	for _, want := range required {
		if !strings.Contains(doc, want) {
			t.Fatalf("boundary registry missing required statement %q", want)
		}
	}
}

func TestC05BoundaryRegistryMatchesC08AndC03Authority(t *testing.T) {
	c08Design := readRepoText(t, "openspec", "changes", "c08-online-editor", "design.md")
	c08Tasks := readRepoText(t, "openspec", "changes", "c08-online-editor", "tasks.md")
	c03Spec := readRepoText(t, "openspec", "specs", "corpus-ingestion", "spec.md")
	adr := readRepoText(t, "docs", "adr", "0001-tech-stack-and-architecture.md")

	assertContainsAll(t, "c08 design", c08Design, []string{
		"D-6：采纳决策四档登记与回流信号",
		"本 change 是采纳决策持有方",
		"不直调 c03 API",
		"由 c03 **单点**消费",
		"不解决 OnlyOffice 商业授权/去品牌/信创官方认证",
	})
	assertContainsAll(t, "c08 tasks", c08Tasks, []string{
		"7.3 实现回流决策信号产出",
		"不直调 c03 API、不直接执行向量入库",
		"入库与对账单点归 c03",
	})
	assertContainsAll(t, "c03 corpus-ingestion spec", c03Spec, []string{
		"采纳稿回流单点消费入库",
		"采纳决策由 c08 持有",
		"本 change 不提供供 c05 / c07 生成编排侧直调的回流入库 API",
	})
	assertContainsAll(t, "ADR-0001", adr, []string{
		"D8 — 在线编辑 = OnlyOffice Document Server 社区版",
		"麒麟有 2022 企业版一手认证",
		"统信 / 龙芯待书面确认",
		"OnlyOffice 统信 / 龙芯**官方兼容认证证书**",
	})
}

func TestC05ProductionCodeDoesNotOwnEditorOrAdoptionIngestion(t *testing.T) {
	forbidden := []string{
		"OnlyOffice",
		"DocsAPI",
		"callbackUrl",
		"document.open",
		"document.edit",
		"document.export",
		"review.online",
		"adopt.decide",
		"corpus_outbox_events",
		"corpus_adoption_feedback",
		"adoption_ingest",
		"internal/rag/corpus",
		"IngestAdoptionFeedback",
		"NewAdoptionOutboxConsumer",
		"AdoptionOutboxConsumer",
		"AdoptionSignal",
		"template.ingest",
		"Milvus",
		"milvus",
	}

	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				t.Fatalf("c05 production file %s contains boundary-owned token %q", path, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan c05 production code: %v", err)
	}

	migration := readRepoText(t, "backend", "migrations", "000007_high_freq_doctype_contracts.sql")
	for _, token := range forbidden {
		if strings.Contains(migration, token) {
			t.Fatalf("c05 migration contains boundary-owned token %q", token)
		}
	}
}

func assertContainsAll(t *testing.T, name, text string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing required authority statement %q", name, want)
		}
	}
}

func readRepoText(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
