package doctype

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

// TestPackageHasNoDirectModelSDKImports 依赖扫描约束（§7.1 / design Risks）：c06 业务包不得直连模型 SDK，
// 只能经 c01 窄抽象（internal/llm）发起模型调用，杜绝绕过 c02 脱敏 / 密级路由的旁路。
func TestPackageHasNoDirectModelSDKImports(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse package dir: %v", err)
	}
	forbidden := []string{"openai", "anthropic"}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			for _, imp := range f.Imports {
				path := strings.ToLower(strings.Trim(imp.Path.Value, `"`))
				for _, bad := range forbidden {
					if strings.Contains(path, bad) {
						t.Fatalf("%s 直连模型 SDK %q：c06 仅可经 c01 窄抽象调用模型，禁止 SDK 旁路", name, imp.Path.Value)
					}
				}
			}
		}
	}
}

// TestOutboundCallsCarryContentSecurityLevel 验证 c06 的全部模型出站路径（判别 / 候选 / 要素抽取）
// 均携带 ContentSecurityLevel 供 c02 出站密级路由（§7.1/§7.3）；涉密密级原样透传以便 c02 强制走私有线。
func TestOutboundCallsCarryContentSecurityLevel(t *testing.T) {
	level := llm.ContentSecurityLevelClassified
	scene := "涉密事项的场景描述需经判别处理"

	clf1, f1 := newTestClassifier(llm.ChatResponse{Text: `{"doctype":"通知","subtype":"召开会议","direction":"downward","confidence":0.9}`}, nil)
	if _, err := clf1.Classify(context.Background(), scene, level, "u", "r"); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if f1.gotReq.ContentSecurityLevel != level {
		t.Fatalf("Classify level = %q, want classified (透传 c02)", f1.gotReq.ContentSecurityLevel)
	}

	clf2, f2 := newTestClassifier(llm.ChatResponse{Text: `[{"doctype":"通知","subtype":"召开会议","direction":"downward","confidence":0.9}]`}, nil)
	if _, err := clf2.ClassifyCandidates(context.Background(), scene, level, "u", "r", defaultThresholds()); err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if f2.gotReq.ContentSecurityLevel != level {
		t.Fatalf("ClassifyCandidates level = %q, want classified", f2.gotReq.ContentSecurityLevel)
	}

	clf3, f3 := newTestClassifier(llm.ChatResponse{Text: `{"发文单位":"区政府"}`}, nil)
	if _, err := clf3.ExtractSlots(context.Background(), scene, []RequiredSlot{SlotIssuer}, level, "u", "r"); err != nil {
		t.Fatalf("extract slots: %v", err)
	}
	if f3.gotReq.ContentSecurityLevel != level {
		t.Fatalf("ExtractSlots level = %q, want classified（澄清抽取同样纳入 c02 路由）", f3.gotReq.ContentSecurityLevel)
	}
}

// TestOutboundCallsPropagateFailClosedBlock 验证 c02 在 NER 不可用 / 无私有可用 / 涉密时返回 fail-closed 阻断错误时，
// c06 原样上抛、绝不静默吞错或回退发送原文（§7.2）。
func TestOutboundCallsPropagateFailClosedBlock(t *testing.T) {
	blockErr := errors.New("c02 fail-closed: desensitization incomplete")
	scene := "含敏感实体的场景描述需经判别处理"

	clf1, _ := newTestClassifier(llm.ChatResponse{}, blockErr)
	if _, err := clf1.Classify(context.Background(), scene, llm.ContentSecurityLevelSensitive, "u", "r"); !errors.Is(err, blockErr) {
		t.Fatalf("Classify error = %v, want propagated fail-closed block", err)
	}

	clf2, _ := newTestClassifier(llm.ChatResponse{}, blockErr)
	if _, err := clf2.ClassifyCandidates(context.Background(), scene, llm.ContentSecurityLevelSensitive, "u", "r", defaultThresholds()); !errors.Is(err, blockErr) {
		t.Fatalf("ClassifyCandidates error = %v, want propagated fail-closed block", err)
	}

	clf3, _ := newTestClassifier(llm.ChatResponse{}, blockErr)
	if _, err := clf3.ExtractSlots(context.Background(), scene, []RequiredSlot{SlotIssuer}, llm.ContentSecurityLevelSensitive, "u", "r"); !errors.Is(err, blockErr) {
		t.Fatalf("ExtractSlots error = %v, want propagated fail-closed block", err)
	}
}
