package doctype

import "testing"

// deepDoctypes 是 PRD「文种覆盖矩阵」A 表的 9 个深做文种。
var deepDoctypes = []string{"通知", "请示", "报告", "函", "会议纪要", "通报", "批复", "讲话稿", "方案"}

// starredSubtypes 是 PDF 标黄（<100 条）的稀缺子类，应降级路由 c07。
var starredSubtypes = []struct{ doctype, subtype string }{
	{"通知", "举办比赛"},
	{"函", "复函"},
	{"方案", "调研方案"},
	{"方案", "会议方案"},
}

func TestDefaultMatrixCoversNineDeepDoctypes(t *testing.T) {
	deep := make(map[string]bool)
	for _, e := range defaultMatrix() {
		if e.Tier == TierDeep {
			deep[e.Doctype] = true
		}
	}
	if len(deep) != len(deepDoctypes) {
		t.Fatalf("deep doctypes = %d, want %d (%v)", len(deep), len(deepDoctypes), deep)
	}
	for _, d := range deepDoctypes {
		if !deep[d] {
			t.Fatalf("deep doctype %q missing from matrix", d)
		}
	}
}

func TestDefaultMatrixMarksStarredRareSubtypes(t *testing.T) {
	m := NewMatrix(defaultMatrix())
	for _, want := range starredSubtypes {
		entry, target := m.Resolve(want.doctype, want.subtype)
		if entry.Tier != TierDeep {
			t.Fatalf("%s-%s tier = %q, want deep_generation", want.doctype, want.subtype, entry.Tier)
		}
		if !entry.IsStarredRare {
			t.Fatalf("%s-%s IsStarredRare = false, want true", want.doctype, want.subtype)
		}
		if target != CapabilityC07 {
			t.Fatalf("%s-%s target = %q, want c07 (starred rare downgrades)", want.doctype, want.subtype, target)
		}
	}
}

func TestDefaultMatrixEntriesHaveValidTiers(t *testing.T) {
	for _, e := range defaultMatrix() {
		if !e.Tier.Valid() {
			t.Fatalf("entry %s-%s has invalid tier %q", e.Doctype, e.Subtype, e.Tier)
		}
	}
}

func TestMatrixResolveRoutesDeepNonStarredToC05(t *testing.T) {
	m := NewMatrix(defaultMatrix())
	entry, target := m.Resolve("通知", "召开会议")
	if entry.Tier != TierDeep || entry.IsStarredRare {
		t.Fatalf("通知-召开会议 entry = %#v, want deep non-starred", entry)
	}
	if target != CapabilityC05 {
		t.Fatalf("通知-召开会议 target = %q, want c05", target)
	}
}

func TestMatrixResolveFallsBackForUnknownSubtypeToC07(t *testing.T) {
	m := NewMatrix(defaultMatrix())
	// 已知 A 表文种 + 未登记子类 → 命中文种级兜底档 → c07，无死路。
	entry, target := m.Resolve("通知", "某种未登记子类")
	if target != CapabilityC07 {
		t.Fatalf("通知-未登记子类 target = %q, want c07", target)
	}
	if entry.Tier != TierFallback {
		t.Fatalf("通知-未登记子类 tier = %q, want fallback", entry.Tier)
	}
}

func TestMatrixResolveFallsBackForUnknownDoctypeToC07(t *testing.T) {
	m := NewMatrix(defaultMatrix())
	entry, target := m.Resolve("某种未规划文种", "任意")
	if target != CapabilityC07 {
		t.Fatalf("未知文种 target = %q, want c07 (no dead end)", target)
	}
	if entry.Tier != TierFallback {
		t.Fatalf("未知文种 tier = %q, want fallback", entry.Tier)
	}
}

func TestMatrixResolveRoutesBTableToC07(t *testing.T) {
	m := NewMatrix(defaultMatrix())
	cases := []struct {
		doctype, subtype string
		wantTier         CapabilityTier
	}{
		{"命令", "任免令", TierTemplateAssist},
		{"决议", "某决议", TierFramework},
		{"简报", "工作简报", TierPlannedTraining},
	}
	for _, c := range cases {
		entry, target := m.Resolve(c.doctype, c.subtype)
		if entry.Tier != c.wantTier {
			t.Fatalf("%s tier = %q, want %q", c.doctype, entry.Tier, c.wantTier)
		}
		if target != CapabilityC07 {
			t.Fatalf("%s target = %q, want c07", c.doctype, target)
		}
	}
}
