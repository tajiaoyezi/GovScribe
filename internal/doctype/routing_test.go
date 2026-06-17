package doctype

import "testing"

func TestRouteDeepNonStarredToC05(t *testing.T) {
	// 4.1：命中 A 表深做且非标黄稀缺 → c05；结构化标签保留文种/子类/方向（4.4）。
	result := ClassificationResult{Doctype: "通知", Subtype: "召开会议", Direction: DirectionDownward, Tier: TierDeep}
	label := Route(result)
	want := RouteLabel{TargetCapability: CapabilityC05, Doctype: "通知", Subtype: "召开会议", Direction: DirectionDownward}
	if label != want {
		t.Fatalf("label = %#v, want %#v", label, want)
	}
}

func TestRouteAllDeepDoctypesToC05(t *testing.T) {
	// 4.1 验收：全部 9 个 A 表深做文种（非标黄）一致路由到 c05。
	for _, doctype := range deepDoctypes {
		if got := Route(ClassificationResult{Doctype: doctype, Tier: TierDeep}).TargetCapability; got != CapabilityC05 {
			t.Fatalf("%s (deep) → %q, want c05", doctype, got)
		}
	}
}

func TestRoutePreservesAllDirections(t *testing.T) {
	// 4.4：四种行文方向均原样保留进路由标签。
	for _, dir := range []WritingDirection{DirectionUpward, DirectionDownward, DirectionHorizontal, DirectionUnspecified} {
		if got := Route(ClassificationResult{Doctype: "通知", Tier: TierDeep, Direction: dir}).Direction; got != dir {
			t.Fatalf("direction %q not preserved, got %q", dir, got)
		}
	}
}

func TestRouteToC07ForStarredBTableAndFallback(t *testing.T) {
	// 4.2：A 表标黄稀缺、B 表各档、兜底/未知 → 一律 c07。
	cases := []struct {
		name   string
		result ClassificationResult
	}{
		{"标黄稀缺", ClassificationResult{Doctype: "方案", Subtype: "调研方案", Tier: TierDeep, IsStarredRare: true}},
		{"B表模版辅助写", ClassificationResult{Doctype: "命令", Tier: TierTemplateAssist}},
		{"B表框架写", ClassificationResult{Doctype: "决议", Tier: TierFramework}},
		{"B表待计划训练", ClassificationResult{Doctype: "简报", Tier: TierPlannedTraining}},
		{"兜底", ClassificationResult{Doctype: "某未知文种", Tier: TierFallback}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Route(c.result).TargetCapability; got != CapabilityC07 {
				t.Fatalf("target = %q, want c07", got)
			}
		})
	}
}

func TestRouteNeverReturnsDeadEnd(t *testing.T) {
	// 4.3：任意能力档（含零值/未知）都得到 c05 或 c07，绝不出现空/「无法处理」。
	tiers := []CapabilityTier{TierDeep, TierTemplateAssist, TierFramework, TierPlannedTraining, TierFallback, CapabilityTier("")}
	for _, tier := range tiers {
		for _, starred := range []bool{false, true} {
			target := Route(ClassificationResult{Tier: tier, IsStarredRare: starred}).TargetCapability
			if target != CapabilityC05 && target != CapabilityC07 {
				t.Fatalf("tier %q starred %v → target %q, want c05 or c07 (no dead end)", tier, starred, target)
			}
		}
	}
}

func TestRoutePreservesStructuredLabelFields(t *testing.T) {
	// 4.4：路由产物为结构化标签，承载判别结果的文种/子类/方向，供随上下文移交。
	result := ClassificationResult{Doctype: "请示", Subtype: "组织成立", Direction: DirectionUpward, Tier: TierDeep, Confidence: 0.92}
	label := Route(result)
	if label.Doctype != "请示" || label.Subtype != "组织成立" || label.Direction != DirectionUpward {
		t.Fatalf("label = %#v, want structured fields preserved", label)
	}
	if label.TargetCapability != CapabilityC05 {
		t.Fatalf("target = %q, want c05", label.TargetCapability)
	}
}

// TestRouteConsistentWithMatrixTargetCapability 确认路由层与分级表层共用同一分流规则、口径一致。
func TestRouteConsistentWithMatrixTargetCapability(t *testing.T) {
	for _, e := range defaultMatrix() {
		result := ClassificationResult{Doctype: e.Doctype, Subtype: e.Subtype, Tier: e.Tier, IsStarredRare: e.IsStarredRare}
		if got, want := Route(result).TargetCapability, e.TargetCapability(); got != want {
			t.Fatalf("%s-%s route target %q != matrix target %q", e.Doctype, e.Subtype, got, want)
		}
	}
}
