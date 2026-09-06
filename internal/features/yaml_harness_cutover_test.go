package features

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestYAMLHarnessLiveTreeExcludesLegacyHarness scans the complete production
// dependency closure, not just internal/harness. This prevents a clean neutral
// kernel from hiding a legacy conversation loop or specialized tool registry
// behind a CLI, server, or public-API adapter.
func TestYAMLHarnessLiveTreeExcludesLegacyHarness(t *testing.T) {
	root := filepath.Join("..", "..")
	command := exec.Command("go", "list", "-deps", "-json", "./cmd/ycode", "./pkg/ycode")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			t.Fatalf("enumerate live tree: %v\n%s", err, failure.Stderr)
		}
		t.Fatal(err)
	}
	forbidden := []string{
		"github.com/qiangli/ycode/internal/runtime/builtin",
		"github.com/qiangli/ycode/internal/runtime/bash",
		"github.com/qiangli/ycode/internal/runtime/config",
		"github.com/qiangli/ycode/internal/runtime/conversation",
		"github.com/qiangli/ycode/internal/runtime/git",
		"github.com/qiangli/ycode/internal/runtime/session",
		"github.com/qiangli/ycode/internal/runtime/toolexec",
		"github.com/qiangli/ycode/internal/tools",
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	violations := make(map[string]bool)
	for {
		var pkg struct {
			ImportPath string
		}
		if err := decoder.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode go list: %v", err)
		}
		if forbiddenImport(pkg.ImportPath, forbidden) {
			violations[pkg.ImportPath] = true
		}
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "external", "priorart":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, legacy := range []string{"settings.json", "directExec", "exec.CommandContext(ctx, \"git\"", "exec.Command(\"git\""} {
				if bytes.Contains(data, []byte(legacy)) {
					relative, _ := filepath.Rel(root, path)
					violations[filepath.ToSlash(relative)+" contains "+legacy] = true
				}
			}
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imported := range parsed.Imports {
			value, unquoteErr := strconv.Unquote(imported.Path.Value)
			if unquoteErr != nil {
				return unquoteErr
			}
			if forbiddenImport(value, forbidden) {
				relative, _ := filepath.Rel(root, path)
				violations[filepath.ToSlash(relative)+" -> "+value] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		ordered := make([]string, 0, len(violations))
		for violation := range violations {
			ordered = append(ordered, violation)
		}
		sort.Strings(ordered)
		t.Fatalf("non-priorart live tree retains prohibited legacy harness packages:\n  %s", strings.Join(ordered, "\n  "))
	}
}

func forbiddenImport(value string, forbidden []string) bool {
	for _, legacy := range forbidden {
		if value == legacy || strings.HasPrefix(value, legacy+"/") {
			return true
		}
	}
	return false
}
