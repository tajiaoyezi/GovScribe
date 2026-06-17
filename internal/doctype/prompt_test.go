package doctype

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildClassificationPromptEmbedsRestrictedLabelSet(t *testing.T) {
	prompt := BuildClassificationPrompt(defaultMatrix())
	// 受限标签集应含 A 表文种及其代表子类，以及 B 表文种（整文种级，供判别可路由 c07）。
	for _, must := range []string{"通知", "请示", "召开会议", "组织成立", "命令", "决议", "简报"} {
		if !strings.Contains(prompt, must) {
			t.Fatalf("prompt missing label %q", must)
		}
	}
	// 应声明严格 JSON 合约字段与无法归类的兜底标签。
	for _, must := range []string{"doctype", "subtype", "confidence", "通用公文"} {
		if !strings.Contains(prompt, must) {
			t.Fatalf("prompt missing contract token %q", must)
		}
	}
}

func TestBuildClassificationPromptDeduplicatesSubtypes(t *testing.T) {
	entries := []MatrixEntry{
		{Doctype: "通知", Subtype: "召开会议", Tier: TierDeep},
		{Doctype: "通知", Subtype: "召开会议", Tier: TierDeep}, // 重复项，应被去重
		{Doctype: "通知", Subtype: "开展活动", Tier: TierDeep},
	}
	prompt := BuildClassificationPrompt(entries)
	if got := strings.Count(prompt, "召开会议"); got != 1 {
		t.Fatalf("\"召开会议\" 在受限标签集出现 %d 次, want 1（去重）", got)
	}
}

func TestParseClassificationOutputParsesStrictJSON(t *testing.T) {
	got, err := ParseClassificationOutput(`{"doctype":"请示","subtype":"组织成立","direction":"upward","confidence":0.92}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := ClassificationOutput{Doctype: "请示", Subtype: "组织成立", Direction: DirectionUpward, Confidence: 0.92}
	if got != want {
		t.Fatalf("output = %#v, want %#v", got, want)
	}
}

func TestParseClassificationOutputStripsCodeFence(t *testing.T) {
	raw := "```json\n{\"doctype\":\"通知\",\"subtype\":\"召开会议\",\"direction\":\"downward\",\"confidence\":0.8}\n```"
	got, err := ParseClassificationOutput(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Doctype != "通知" || got.Direction != DirectionDownward {
		t.Fatalf("output = %#v, want 通知/downward", got)
	}
}

func TestParseClassificationOutputRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"not json":          "这不是 JSON",
		"empty doctype":     `{"doctype":"","confidence":0.5}`,
		"confidence > 1":    `{"doctype":"通知","confidence":1.5}`,
		"confidence < 0":    `{"doctype":"通知","confidence":-0.1}`,
		"invalid direction": `{"doctype":"通知","direction":"sideways","confidence":0.5}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseClassificationOutput(raw); !errors.Is(err, ErrInvalidClassificationOutput) {
				t.Fatalf("error = %v, want ErrInvalidClassificationOutput", err)
			}
		})
	}
}

func TestParseClassificationCandidates(t *testing.T) {
	t.Run("valid array preserves order", func(t *testing.T) {
		outs, err := ParseClassificationCandidates(`[{"doctype":"报告","subtype":"年度","direction":"upward","confidence":0.7},{"doctype":"请示","subtype":"","direction":"","confidence":0.6}]`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(outs) != 2 || outs[0].Doctype != "报告" || outs[1].Doctype != "请示" {
			t.Fatalf("outs = %#v", outs)
		}
	})
	t.Run("code fence wrapped array", func(t *testing.T) {
		outs, err := ParseClassificationCandidates("```json\n[{\"doctype\":\"通知\",\"confidence\":0.9}]\n```")
		if err != nil || len(outs) != 1 {
			t.Fatalf("outs = %#v, err = %v", outs, err)
		}
	})
	t.Run("rejects object not array", func(t *testing.T) {
		if _, err := ParseClassificationCandidates(`{"doctype":"通知","confidence":0.5}`); !errors.Is(err, ErrInvalidClassificationOutput) {
			t.Fatalf("error = %v, want ErrInvalidClassificationOutput", err)
		}
	})
	t.Run("rejects empty array", func(t *testing.T) {
		if _, err := ParseClassificationCandidates(`[]`); !errors.Is(err, ErrInvalidClassificationOutput) {
			t.Fatalf("error = %v, want ErrInvalidClassificationOutput", err)
		}
	})
	t.Run("rejects when any item invalid", func(t *testing.T) {
		if _, err := ParseClassificationCandidates(`[{"doctype":"通知","confidence":0.5},{"doctype":"报告","confidence":1.5}]`); !errors.Is(err, ErrInvalidClassificationOutput) {
			t.Fatalf("error = %v, want ErrInvalidClassificationOutput", err)
		}
	})
}

func TestBuildCandidatesPrompt(t *testing.T) {
	prompt := BuildCandidatesPrompt(defaultMatrix(), 3)
	for _, must := range []string{"最多 3 个", "JSON 数组", "通知", "请示", "命令", "通用公文"} {
		if !strings.Contains(prompt, must) {
			t.Fatalf("candidates prompt missing %q", must)
		}
	}
	// TopN < 1 应被夹为 1。
	if !strings.Contains(BuildCandidatesPrompt(defaultMatrix(), 0), "最多 1 个") {
		t.Fatalf("TopN=0 should clamp to 1 in prompt")
	}
}
