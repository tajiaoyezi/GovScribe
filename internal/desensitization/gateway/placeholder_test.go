package gateway

import (
	"strings"
	"testing"
)

func TestApplyPlaceholdersKeepsSameOriginalValueConsistent(t *testing.T) {
	text := "市财政局请市财政局反馈。"
	first := strings.Index(text, "市财政局")
	second := strings.LastIndex(text, "市财政局")
	hits := []Hit{
		{Start: first, End: first + len("市财政局"), Text: "市财政局", Type: EntityTypeOrganization, Source: SourceDictionary},
		{Start: second, End: second + len("市财政局"), Text: "市财政局", Type: EntityTypeOrganization, Source: SourceDictionary},
	}

	result := ApplyPlaceholders(text, hits)

	if result.Text != "〖ORGANIZATION_01〗请〖ORGANIZATION_01〗反馈。" {
		t.Fatalf("sanitized text = %q", result.Text)
	}
	if len(result.Mappings) != 1 {
		t.Fatalf("mapping count = %d, want 1: %#v", len(result.Mappings), result.Mappings)
	}
	if result.Mappings[0].Original != "市财政局" || result.Mappings[0].Placeholder != "〖ORGANIZATION_01〗" {
		t.Fatalf("mapping = %#v, want organization placeholder for original", result.Mappings[0])
	}
}

func TestRestorePlaceholdersOnlyRestoresKnownMappings(t *testing.T) {
	result := SanitizationResult{
		Mappings: []Mapping{
			{Placeholder: "〖PERSON_01〗", Original: "张三", Type: EntityTypePerson, Source: SourceDictionary},
		},
	}

	restored := result.Restore("请〖PERSON_01〗核对，保留〖PERSON_99〗。")

	if restored != "请张三核对，保留〖PERSON_99〗。" {
		t.Fatalf("restored = %q", restored)
	}
}
