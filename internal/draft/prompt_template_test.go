package draft

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDefaultPromptTemplateObjectsCoverNineDoctypesAndSections(t *testing.T) {
	templates := DefaultPromptTemplateObjects()
	if len(templates) != 9 {
		t.Fatalf("default template objects len = %d, want 9", len(templates))
	}

	byDoctype := make(map[string]PromptTemplateObject)
	for _, tpl := range templates {
		if tpl.Doctype == "" || tpl.Version != DefaultPromptTemplateVersion || tpl.ObjectKey == "" {
			t.Fatalf("template object misses identity fields: %#v", tpl)
		}
		if !strings.HasPrefix(tpl.ObjectKey, "draft/templates/high-frequency/") || !strings.HasSuffix(tpl.ObjectKey, "/v1.md") {
			t.Fatalf("object key = %q, want high-frequency v1 markdown key", tpl.ObjectKey)
		}
		requiredSections := []string{
			"## 标题构成",
			"## 称谓",
			"## 主送机关",
			"## 正文段落结构与必备要素",
			"## 落款",
			"## 口吻指令",
			"## Few-shot 样例编排",
			"## 机关口径红线",
		}
		for _, section := range requiredSections {
			if !strings.Contains(tpl.Content, section) {
				t.Fatalf("template for %s missing section %q:\n%s", tpl.Doctype, section, tpl.Content)
			}
		}
		byDoctype[tpl.Doctype] = tpl
	}

	wantDoctypes := []string{"通知", "请示", "报告", "函", "会议纪要", "通报", "批复", "讲话稿", "方案"}
	for _, doctypeName := range wantDoctypes {
		tpl, ok := byDoctype[doctypeName]
		if !ok {
			t.Fatalf("missing template object for %s", doctypeName)
		}
		if !strings.Contains(tpl.Content, "# "+doctypeName+"提示模板") {
			t.Fatalf("template title for %s not present in content", doctypeName)
		}
	}
	if !strings.Contains(byDoctype["请示"].Content, "妥否，请批示。") {
		t.Fatalf("请示 template should carry upward closing rule:\n%s", byDoctype["请示"].Content)
	}
	if !strings.Contains(byDoctype["通知"].Content, "同文种") || !strings.Contains(byDoctype["通知"].Content, "同子类") {
		t.Fatalf("few-shot section should carry doctype/subtype consistency rule:\n%s", byDoctype["通知"].Content)
	}
}

func TestDefaultStructureContractsReferencePromptTemplateObjects(t *testing.T) {
	for _, contract := range DefaultStructureContracts() {
		if contract.TemplateVersion != DefaultPromptTemplateVersion {
			t.Fatalf("%s template version = %q, want %q", contract.Doctype, contract.TemplateVersion, DefaultPromptTemplateVersion)
		}
		wantKey, err := PromptTemplateObjectKey(contract.Doctype, contract.TemplateVersion)
		if err != nil {
			t.Fatalf("template key for %s: %v", contract.Doctype, err)
		}
		if contract.TemplateObjectKey != wantKey {
			t.Fatalf("%s template object key = %q, want %q", contract.Doctype, contract.TemplateObjectKey, wantKey)
		}
	}
}

func TestSeedPromptTemplateObjectsWritesVersionedObjects(t *testing.T) {
	store := newRecordingPromptTemplateStore()
	templates := DefaultPromptTemplateObjects()

	if err := SeedPromptTemplateObjects(context.Background(), store, templates); err != nil {
		t.Fatalf("seed prompt templates: %v", err)
	}
	if len(store.puts) != 9 {
		t.Fatalf("put count = %d, want 9", len(store.puts))
	}

	key, err := PromptTemplateObjectKey("请示", DefaultPromptTemplateVersion)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if store.puts[key] == "" || !strings.Contains(store.puts[key], "## Few-shot 样例编排") {
		t.Fatalf("请示 object not written with template content: %q", store.puts[key])
	}
}

func TestGetPromptTemplateObjectReadsStoredContentByDoctypeAndVersion(t *testing.T) {
	store := newRecordingPromptTemplateStore()
	if err := SeedPromptTemplateObjects(context.Background(), store, DefaultPromptTemplateObjects()); err != nil {
		t.Fatalf("seed prompt templates: %v", err)
	}
	key, err := PromptTemplateObjectKey("通知", DefaultPromptTemplateVersion)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	store.puts[key] = "调整后的通知模板措辞"

	got, err := GetPromptTemplateObject(context.Background(), store, "通知", DefaultPromptTemplateVersion)
	if err != nil {
		t.Fatalf("get prompt template: %v", err)
	}
	if got.Doctype != "通知" || got.Version != DefaultPromptTemplateVersion || got.ObjectKey != key {
		t.Fatalf("template identity = %#v, want 通知/v1/%s", got, key)
	}
	if got.Content != "调整后的通知模板措辞" {
		t.Fatalf("content = %q, want stored object content", got.Content)
	}
}

func TestGetPromptTemplateObjectRejectsUnsupportedDoctypeAndMissingObject(t *testing.T) {
	store := newRecordingPromptTemplateStore()
	if _, err := GetPromptTemplateObject(context.Background(), store, "命令", DefaultPromptTemplateVersion); !errors.Is(err, ErrPromptTemplateNotFound) {
		t.Fatalf("unsupported doctype err = %v, want ErrPromptTemplateNotFound", err)
	}
	if _, err := GetPromptTemplateObject(context.Background(), store, "通知", DefaultPromptTemplateVersion); !errors.Is(err, ErrPromptTemplateNotFound) {
		t.Fatalf("missing object err = %v, want ErrPromptTemplateNotFound", err)
	}
}

type recordingPromptTemplateStore struct {
	puts map[string]string
	err  error
}

func newRecordingPromptTemplateStore() *recordingPromptTemplateStore {
	return &recordingPromptTemplateStore{puts: make(map[string]string)}
}

func (s *recordingPromptTemplateStore) PutTemplate(_ context.Context, objectKey string, content []byte) error {
	if s.err != nil {
		return s.err
	}
	s.puts[objectKey] = string(content)
	return nil
}

func (s *recordingPromptTemplateStore) GetTemplate(_ context.Context, objectKey string) ([]byte, error) {
	content, ok := s.puts[objectKey]
	if !ok {
		return nil, ErrPromptTemplateNotFound
	}
	return []byte(content), nil
}
