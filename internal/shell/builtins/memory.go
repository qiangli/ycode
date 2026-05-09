package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qiangli/ycode/pkg/memex/memory"
)

func init() {
	Register(&rememberVerb{})
	Register(&recallVerb{})
}

// memoryManager opens (or creates) the memex memory manager rooted at
// the standard ycode memory directories. Used by both yc remember and
// yc recall.
func memoryManager() (*memory.Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("user home: %w", err)
	}
	cwd, _ := os.Getwd()
	globalDir := filepath.Join(home, ".agents", "ycode", "memory")
	projectDir := filepath.Join(cwd, ".agents", "ycode", "memory")
	return memory.NewManagerWithGlobal(globalDir, projectDir)
}

// ----- yc remember -----

type rememberVerb struct{}

func (rememberVerb) Name() string { return "remember" }
func (rememberVerb) Description() string {
	return "Save a fact / preference to memex semantic memory"
}
func (rememberVerb) Usage() string {
	return `yc remember "<text>" [--name=<id>] [--scope=project|user] [--type=user|project|reference|feedback]`
}

func (rememberVerb) Run(_ context.Context, args []string, stdio Stdio, _ string) (int, error) {
	var (
		text    string
		name    = fmt.Sprintf("note-%d", time.Now().Unix())
		scope   = memory.ScopeProject
		memType = memory.TypeReference
	)
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--name="):
			name = a[len("--name="):]
		case strings.HasPrefix(a, "--scope="):
			s := strings.ToLower(a[len("--scope="):])
			switch s {
			case "user", "global":
				scope = memory.ScopeUser
			default:
				scope = memory.ScopeProject
			}
		case strings.HasPrefix(a, "--type="):
			memType = memory.Type(a[len("--type="):])
		default:
			if text == "" {
				text = a
			}
		}
	}
	if text == "" {
		fmt.Fprintln(stdio.Stderr, "yc remember: missing text")
		return 2, nil
	}

	mgr, err := memoryManager()
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc remember: %v\n", err)
		return 1, nil
	}
	mem := &memory.Memory{
		Name:        name,
		Description: firstLine(text, 80),
		Type:        memType,
		Scope:       scope,
		Content:     text,
		Importance:  0.5,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := mgr.Save(mem); err != nil {
		fmt.Fprintf(stdio.Stderr, "yc remember: save: %v\n", err)
		return 1, nil
	}
	fmt.Fprintf(stdio.Stdout, "saved %s (scope=%s type=%s)\n", mem.Name, mem.EffectiveScope(), mem.Type)
	return 0, nil
}

func firstLine(s string, max int) string {
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// ----- yc recall -----

type recallVerb struct{}

func (recallVerb) Name() string { return "recall" }
func (recallVerb) Description() string {
	return "Search semantic memory for matching memories (RRF fusion across backends)"
}
func (recallVerb) Usage() string { return "yc recall <query> [--limit=N] [--json]" }

func (recallVerb) Run(_ context.Context, args []string, stdio Stdio, _ string) (int, error) {
	asJSON := false
	limit := 5
	var query string
	for _, a := range args {
		switch {
		case a == "--json":
			asJSON = true
		case strings.HasPrefix(a, "--limit="):
			fmt.Sscanf(a[len("--limit="):], "%d", &limit)
		default:
			if query == "" {
				query = a
			} else {
				query += " " + a
			}
		}
	}
	if query == "" {
		fmt.Fprintln(stdio.Stderr, "yc recall: missing query")
		return 2, nil
	}
	mgr, err := memoryManager()
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc recall: %v\n", err)
		return 1, nil
	}
	results, err := mgr.Recall(query, limit)
	if err != nil {
		fmt.Fprintf(stdio.Stderr, "yc recall: %v\n", err)
		return 1, nil
	}
	if asJSON {
		enc := json.NewEncoder(stdio.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(results)
		return 0, nil
	}
	if len(results) == 0 {
		fmt.Fprintln(stdio.Stderr, "(no matching memories)")
		return 1, nil
	}
	for _, r := range results {
		fmt.Fprintf(stdio.Stdout, "%s [%s] %s\n", r.Memory.Name, r.Memory.Type, r.Memory.Description)
		if r.Memory.Content != "" && r.Memory.Content != r.Memory.Description {
			fmt.Fprintf(stdio.Stdout, "    %s\n", firstLine(r.Memory.Content, 200))
		}
	}
	return 0, nil
}
