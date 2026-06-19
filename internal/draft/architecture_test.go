package draft

import (
	"os/exec"
	"strings"
	"testing"
)

func TestDraftImportGraphHasNoConcreteModelSDKs(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./internal/draft")
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list deps: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, forbidden := range []string{
			"github.com/openai/openai-go",
			"github.com/anthropics/anthropic-sdk-go",
			"github.com/anthropic-ai/anthropic-sdk-go",
			"litellm",
		} {
			if strings.Contains(strings.ToLower(dep), strings.ToLower(forbidden)) {
				t.Fatalf("internal/draft import graph contains concrete model SDK/client dependency %q via %q", forbidden, dep)
			}
		}
	}
}
