package draft

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
)

func TestStructureContractReaderAssemblesAllHighFreqContracts(t *testing.T) {
	ctx := context.Background()
	templates := newRecordingPromptTemplateStore()
	if err := SeedPromptTemplateObjects(ctx, templates, DefaultPromptTemplateObjects()); err != nil {
		t.Fatalf("seed templates: %v", err)
	}
	reader := NewStructureContractReader(NewMemoryStructureContractStore(), templates)

	for _, contract := range DefaultStructureContracts() {
		got, err := reader.Get(ctx, contract.Doctype)
		if err != nil {
			t.Fatalf("get %s: %v", contract.Doctype, err)
		}
		if got.Doctype != contract.Doctype || got.Template.ObjectKey != contract.TemplateObjectKey {
			t.Fatalf("identity for %s = %#v", contract.Doctype, got)
		}
		if got.TitleRule == "" || got.SalutationRule == "" || got.RecipientRule == "" || got.SignatureRule == "" {
			t.Fatalf("%s structural scalar fields missing: %#v", contract.Doctype, got)
		}
		if len(got.BodyStructure) == 0 || len(got.RequiredSlots) == 0 {
			t.Fatalf("%s body structure or required slots missing: %#v", contract.Doctype, got)
		}
		for _, slot := range contract.RequiredSlots {
			if !containsRequiredSlot(got.RequiredSlots, slot) {
				t.Fatalf("%s missing required slot %q in %#v", contract.Doctype, slot, got.RequiredSlots)
			}
			if !strings.Contains(got.Template.Content, string(slot)) {
				t.Fatalf("%s template content missing required slot %q:\n%s", contract.Doctype, slot, got.Template.Content)
			}
		}
		if !strings.Contains(got.Template.Content, got.TitleRule) || !strings.Contains(got.Template.Content, got.SignatureRule) {
			t.Fatalf("%s template content does not carry title/signature rules:\n%s", contract.Doctype, got.Template.Content)
		}
	}
}

func TestStructureContractReaderUsesStoredTemplateObjectKey(t *testing.T) {
	ctx := context.Background()
	contract := StructureContract{
		Doctype:           "请示",
		Direction:         doctype.DirectionUpward,
		TitleRule:         "自定义标题规则",
		SalutationRule:    "自定义称谓",
		RecipientRule:     "自定义主送",
		BodyStructure:     []string{"自定义正文结构"},
		RequiredSlots:     []doctype.RequiredSlot{doctype.SlotIssuer, doctype.SlotRecipient},
		SignatureRule:     "自定义落款",
		TemplateObjectKey: "draft/templates/high-frequency/custom-qingshi/uat.md",
		TemplateVersion:   "uat",
	}
	templates := newRecordingPromptTemplateStore()
	templates.puts[contract.TemplateObjectKey] = "UAT 调整后的请示模板"
	reader := NewStructureContractReader(
		staticStructureContractStore{contracts: map[string]StructureContract{"请示": contract}},
		templates,
	)

	got, err := reader.Get(ctx, "请示")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Template.ObjectKey != contract.TemplateObjectKey || got.Template.Version != "uat" {
		t.Fatalf("template identity = %#v, want stored object key/version", got.Template)
	}
	if got.Template.Content != "UAT 调整后的请示模板" {
		t.Fatalf("template content = %q, want stored object content", got.Template.Content)
	}
}

func TestStructureContractReaderReturnsNoDeepContractForNonHighFrequencyDoctype(t *testing.T) {
	ctx := context.Background()
	templates := &countingPromptTemplateReader{}
	reader := NewStructureContractReader(NewMemoryStructureContractStore(), templates)

	_, err := reader.Get(ctx, "命令")
	if err == nil {
		t.Fatal("get non-high-frequency doctype err = nil, want no deep contract error")
	}
	if !errors.Is(err, ErrNoDeepStructureContract) {
		t.Fatalf("err = %v, want ErrNoDeepStructureContract", err)
	}
	var noDeep NoDeepStructureContractError
	if !errors.As(err, &noDeep) {
		t.Fatalf("err = %T %v, want NoDeepStructureContractError", err, err)
	}
	if noDeep.Doctype != "命令" {
		t.Fatalf("doctype in error = %q, want 命令", noDeep.Doctype)
	}
	if templates.calls != 0 {
		t.Fatalf("template reader calls = %d, want 0 for non-high-frequency doctype", templates.calls)
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "c07") || strings.Contains(message, "fallback") || strings.Contains(message, "移交") {
		t.Fatalf("no deep contract error must not decide fallback/c07 transfer: %q", err.Error())
	}
}

func containsRequiredSlot(slots []doctype.RequiredSlot, want doctype.RequiredSlot) bool {
	for _, slot := range slots {
		if slot == want {
			return true
		}
	}
	return false
}

type staticStructureContractStore struct {
	contracts map[string]StructureContract
}

func (s staticStructureContractStore) Get(_ context.Context, doctypeName string) (StructureContract, error) {
	contract, ok := s.contracts[doctypeName]
	if !ok {
		return StructureContract{}, ErrStructureContractNotFound
	}
	return copyStructureContract(contract), nil
}

func (s staticStructureContractStore) List(context.Context) ([]StructureContract, error) {
	out := make([]StructureContract, 0, len(s.contracts))
	for _, contract := range s.contracts {
		out = append(out, copyStructureContract(contract))
	}
	return out, nil
}

type countingPromptTemplateReader struct {
	calls int
}

func (r *countingPromptTemplateReader) GetTemplate(context.Context, string) ([]byte, error) {
	r.calls++
	return nil, ErrPromptTemplateNotFound
}
