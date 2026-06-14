package llm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBusinessPackagesDoNotImportConcreteModelClients(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{
		"github.com/openai/openai-go",
		"github.com/anthropics/anthropic-sdk-go",
		"github.com/BerriAI/litellm",
	}
	allowedPrefixes := []string{
		filepath.Join(root, "internal", "llm"),
	}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == ".agents" || name == ".claude" || name == ".codex" || name == ".cursor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || hasAnyPrefix(path, allowedPrefixes) {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, importSpec := range file.Imports {
			importPath := strings.Trim(importSpec.Path.Value, `"`)
			for _, forbiddenPrefix := range forbidden {
				if strings.HasPrefix(importPath, forbiddenPrefix) {
					t.Fatalf("%s imports forbidden concrete model client %q", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

func TestOpenAIGoImportsUseV3Path(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == ".agents" || name == ".claude" || name == ".codex" || name == ".cursor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, importSpec := range file.Imports {
			importPath := strings.Trim(importSpec.Path.Value, `"`)
			if importPath == "github.com/openai/openai-go" ||
				(strings.HasPrefix(importPath, "github.com/openai/openai-go/") && !strings.HasPrefix(importPath, "github.com/openai/openai-go/v3")) {
				t.Fatalf("%s imports non-v3 OpenAI SDK path %q", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func hasAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

var _ = ast.File{}
