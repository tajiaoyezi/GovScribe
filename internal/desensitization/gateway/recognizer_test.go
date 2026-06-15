package gateway

import (
	"context"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/desensitization/dictionary"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestRegexRecognizerFindsFormattedEntities(t *testing.T) {
	text := "请依据〔2024〕12号文件向张三支付1234元、12345.67元和12,345.67元，身份证号11010519491231002X，统一社会信用代码91350211M000100Y43。"

	hits := NewRegexRecognizer().Recognize(text)

	assertHasHit(t, hits, EntityTypeDocumentNumber, SourceRegex, "〔2024〕12号")
	assertHasHit(t, hits, EntityTypeAmount, SourceRegex, "1234元")
	assertHasHit(t, hits, EntityTypeAmount, SourceRegex, "12345.67元")
	assertHasHit(t, hits, EntityTypeAmount, SourceRegex, "12,345.67元")
	assertHasHit(t, hits, EntityTypeIdentityNumber, SourceRegex, "11010519491231002X")
	assertHasHit(t, hits, EntityTypeUnifiedSocialCreditCode, SourceRegex, "91350211M000100Y43")
}

func TestDictionaryRecognizerUsesTypedEntries(t *testing.T) {
	recognizer := NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
		{Text: "张三", Type: dictionary.EntryTypePerson},
		{Text: "春风行动", Type: dictionary.EntryTypeProjectCode},
		{Text: "绝密项目", Type: dictionary.EntryTypeSecretKeywordBlacklist},
	})

	hits := recognizer.Recognize("市财政局请张三负责春风行动，不得提及绝密项目。")

	assertHasHit(t, hits, EntityTypeOrganization, SourceDictionary, "市财政局")
	assertHasHit(t, hits, EntityTypePerson, SourceDictionary, "张三")
	assertHasHit(t, hits, EntityTypeProjectCode, SourceDictionary, "春风行动")
	assertHasHit(t, hits, EntityTypeSecretKeywordBlacklist, SourceDictionary, "绝密项目")
}

func TestDictionaryRecognizerUsesLockedPetarAhoCorasickWithChineseByteOffsets(t *testing.T) {
	recognizer := NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
		{Text: "张三", Type: dictionary.EntryTypePerson},
	})
	if _, ok := recognizer.automaton.Load().(*petarAutomaton); !ok {
		t.Fatalf("default dictionary recognizer should use locked petar Aho-Corasick backend")
	}

	text := "请市财政局与张三共同反馈，市财政局负责汇总。"
	hits := recognizer.Recognize(text)

	assertHasHitAt(t, hits, EntityTypeOrganization, SourceDictionary, "市财政局", len("请"), len("请市财政局"))
	assertHasHitAt(t, hits, EntityTypePerson, SourceDictionary, "张三", len("请市财政局与"), len("请市财政局与张三"))
	assertHasHitAt(t, hits, EntityTypeOrganization, SourceDictionary, "市财政局", len("请市财政局与张三共同反馈，"), len("请市财政局与张三共同反馈，市财政局"))
}

func TestDictionaryRecognizerSwapEntriesAffectsSubsequentRequests(t *testing.T) {
	recognizer := NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
	})
	if len(recognizer.Recognize("市财政局")) != 1 {
		t.Fatal("seed entry should match before swap")
	}

	recognizer.SwapEntries([]dictionary.Entry{
		{Text: "市发展改革委", Type: dictionary.EntryTypeOrganization},
	})

	if hits := recognizer.Recognize("市财政局"); len(hits) != 0 {
		t.Fatalf("old entry still matched after swap: %#v", hits)
	}
	assertHasHit(t, recognizer.Recognize("市发展改革委"), EntityTypeOrganization, SourceDictionary, "市发展改革委")
}

func TestDictionaryServiceReloadsRecognizerAfterEntryMaintenance(t *testing.T) {
	store := dictionary.NewMemoryStore()
	recognizer := NewDictionaryRecognizer(nil)
	svc := dictionary.NewServiceWithReloader(store, dictionaryAllowAuthorizer{}, NewDictionaryRecognizerReloader(recognizer))

	created, err := svc.CreateEntry(context.Background(), dictionary.Principal{ID: "admin-1"}, dictionary.CreateEntryRequest{
		Text: "市财政局",
		Type: dictionary.EntryTypeOrganization,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}
	assertHasHit(t, recognizer.Recognize("请市财政局反馈"), EntityTypeOrganization, SourceDictionary, "市财政局")

	updated, err := svc.UpdateEntry(context.Background(), dictionary.Principal{ID: "admin-1"}, created.ID, dictionary.UpdateEntryRequest{
		Text: "市发展改革委",
		Type: dictionary.EntryTypeOrganization,
	})
	if err != nil {
		t.Fatalf("update entry: %v", err)
	}
	if hits := recognizer.Recognize("请市财政局反馈"); len(hits) != 0 {
		t.Fatalf("old entry still matched after update: %#v", hits)
	}
	assertHasHit(t, recognizer.Recognize("请市发展改革委反馈"), EntityTypeOrganization, SourceDictionary, "市发展改革委")

	if err := svc.DeleteEntry(context.Background(), dictionary.Principal{ID: "admin-1"}, updated.ID); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	if hits := recognizer.Recognize("请市发展改革委反馈"); len(hits) != 0 {
		t.Fatalf("deleted entry still matched: %#v", hits)
	}
}

func TestProcessorMergesNERHitsWithRegexAndDictionary(t *testing.T) {
	processor := NewProcessorWithNER(NewDictionaryRecognizer([]dictionary.Entry{
		{Text: "市财政局", Type: dictionary.EntryTypeOrganization},
	}), staticNERClient{hits: []Hit{
		{Start: len("请"), End: len("请张三"), Text: "张三", Type: EntityTypePerson, Source: SourceNER},
	}})

	messages, result, err := processor.SanitizeMessagesContext(context.Background(), []llm.Message{
		{Role: llm.RoleUser, Content: "请张三联系市财政局。"},
	})
	if err != nil {
		t.Fatalf("sanitize messages with ner: %v", err)
	}
	if messages[0].Content != "请〖PERSON_01〗联系〖ORGANIZATION_01〗。" {
		t.Fatalf("sanitized content = %q", messages[0].Content)
	}
	if restored := result.Restore(messages[0].Content); restored != "请张三联系市财政局。" {
		t.Fatalf("restored content = %q", restored)
	}
}

func TestMergeHitsPrefersBlacklistAndLongerContainingSpan(t *testing.T) {
	hits := []Hit{
		{Start: 0, End: len("绝密"), Text: "绝密", Type: EntityTypeOrganization, Source: SourceDictionary},
		{Start: 0, End: len("绝密项目"), Text: "绝密项目", Type: EntityTypeSecretKeywordBlacklist, Source: SourceDictionary},
		{Start: len("绝密项目"), End: len("绝密项目〔2024〕12号"), Text: "〔2024〕12号", Type: EntityTypeDocumentNumber, Source: SourceRegex},
	}

	merged := MergeHits(hits)

	if len(merged) != 2 {
		t.Fatalf("merged count = %d, want 2: %#v", len(merged), merged)
	}
	if merged[0].Text != "绝密项目" || merged[0].Type != EntityTypeSecretKeywordBlacklist {
		t.Fatalf("first merged hit = %#v, want blacklist longer span", merged[0])
	}
	if merged[1].Text != "〔2024〕12号" || merged[1].Type != EntityTypeDocumentNumber {
		t.Fatalf("second merged hit = %#v, want document number", merged[1])
	}
}

type dictionaryAllowAuthorizer struct{}

func (dictionaryAllowAuthorizer) Authorize(context.Context, dictionary.Principal, dictionary.Permission) error {
	return nil
}

type staticNERClient struct {
	hits []Hit
	err  error
}

func (c staticNERClient) Recognize(context.Context, string) ([]Hit, error) {
	return c.hits, c.err
}

func assertHasHit(t *testing.T, hits []Hit, entityType EntityType, source Source, text string) {
	t.Helper()
	for _, hit := range hits {
		if hit.Type == entityType && hit.Source == source && hit.Text == text {
			return
		}
	}
	t.Fatalf("missing hit type=%q source=%q text=%q in %#v", entityType, source, text, hits)
}

func assertHasHitAt(t *testing.T, hits []Hit, entityType EntityType, source Source, text string, start, end int) {
	t.Helper()
	for _, hit := range hits {
		if hit.Type == entityType && hit.Source == source && hit.Text == text && hit.Start == start && hit.End == end {
			return
		}
	}
	t.Fatalf("missing hit type=%q source=%q text=%q span=%d-%d in %#v", entityType, source, text, start, end, hits)
}
