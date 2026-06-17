package doctype

import "testing"

func TestResolveDirectionUsesDoctypeDefaultWhenNoClue(t *testing.T) {
	// 请示默认上行；场景无机关关系线索、模型未给方向 → 取默认且不算覆盖。
	dir, overrode := ResolveDirection("请示", DirectionUnspecified, "我要写一份请示")
	if dir != DirectionUpward {
		t.Fatalf("direction = %q, want upward", dir)
	}
	if overrode {
		t.Fatalf("overrode = true, want false (no model conflict)")
	}
}

func TestResolveDirectionClueOverridesDefaultAndFlagsConflict(t *testing.T) {
	// 函默认平行，但「向上级请求批准」线索修正为上行；模型给平行 → 冲突，以规则为准并降置信度。
	dir, overrode := ResolveDirection("函", DirectionHorizontal, "向上级请求批准更新执法车辆")
	if dir != DirectionUpward {
		t.Fatalf("direction = %q, want upward (clue correction)", dir)
	}
	if !overrode {
		t.Fatalf("overrode = false, want true (rule conflicts with model)")
	}
}

func TestResolveDirectionClueVariants(t *testing.T) {
	cases := []struct {
		scene string
		want  WritingDirection
	}{
		{"对下级单位的请示予以批复", DirectionDownward},
		{"与同级单位商洽工作", DirectionHorizontal},
		{"向上级报送材料", DirectionUpward},
	}
	for _, c := range cases {
		if dir, _ := ResolveDirection("函", DirectionUnspecified, c.scene); dir != c.want {
			t.Fatalf("scene %q direction = %q, want %q", c.scene, dir, c.want)
		}
	}
}

func TestResolveDirectionDefersToModelWhenRuleUnspecified(t *testing.T) {
	// 讲话稿无默认方向且无线索 → 采信模型线索，不算覆盖。
	dir, overrode := ResolveDirection("讲话稿", DirectionDownward, "在大会上的讲话")
	if dir != DirectionDownward {
		t.Fatalf("direction = %q, want downward (defer to model)", dir)
	}
	if overrode {
		t.Fatalf("overrode = true, want false")
	}
}
