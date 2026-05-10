package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dhnt/dhnt/catalog"
	"github.com/qiangli/ycode/internal/runtime/builtin"
)

// RegisterSkillHandler registers the Skill and skill_list tool handlers.
func RegisterSkillHandler(r *Registry) {
	registerSkillListHandler(r)

	spec, ok := r.Get("Skill")
	if !ok {
		return
	}
	spec.Handler = func(ctx context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Skill string `json:"skill"`
			Args  string `json:"args,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse Skill input: %w", err)
		}
		return resolveSkill(ctx, params.Skill, params.Args)
	}
}

// resolveSkill is the central dispatch path:
//
//  1. Local overlays (.agents/ycode/skills, ~/.agents/ycode/skills,
//     $YCODE_SKILLS_DIR) shadow catalog entries — a project can override a
//     community skill without forking the dhnt module.
//  2. The upstream dhnt catalog (github.com/dhnt/dhnt/catalog) is consulted
//     next; entries declare an executor in their frontmatter:
//     "markdown"  → return the body for the LLM to follow,
//     "builtin"   → dispatch to builtin.GetSkillExecutor(name),
//     "cnl"       → currently unsupported (typed-AST runtime not yet wired).
//  3. Builtin executors registered without a catalog entry are still callable
//     for backwards compatibility.
//
// Every dispatch — whether successful or not — appends one event to the
// skill-usage telemetry log. See usage.go.
func resolveSkill(ctx context.Context, name, args string) (content string, err error) {
	start := time.Now()
	var source string
	defer func() {
		recordSkillUsage(name, len(args), source, err, time.Since(start))
	}()

	// 1. Local overlay first — local definitions win.
	if c, e := discoverSkill(name); e == nil {
		source = usageSourceInternal
		return c, nil
	}

	// 2. Upstream catalog.
	if s, ok := catalog.Lookup(name); ok {
		switch s.Executor {
		case "builtin":
			if exec, ok := builtin.GetSkillExecutor(s.Name); ok {
				source = usageSourceExternalBuiltin
				return exec(ctx, args)
			}
			// No matching builtin executor — degrade to the body so the
			// LLM still gets the instruction.
			source = usageSourceExternal
			return s.Body, nil
		case "cnl":
			source = usageSourceExternalCNL
			return "", fmt.Errorf("skill %q uses executor=cnl which is not yet supported", name)
		default: // "markdown" or unset
			source = usageSourceExternal
			return s.Body, nil
		}
	}

	// 3. Builtin executor without a catalog entry (legacy fallback).
	if exec, ok := builtin.GetSkillExecutor(name); ok {
		source = usageSourceBuiltin
		return exec(ctx, args)
	}

	source = usageSourceNotFound
	return "", fmt.Errorf("skill %q not found", name)
}

// DiscoverSkill is the exported entry point for resolving a skill body by
// name. It consults the local overlays and the upstream catalog in that
// order, returning the markdown body. Builtin-executor skills return their
// catalog body (the executor itself is invoked through resolveSkill, not
// this function).
func DiscoverSkill(name string) (string, error) {
	if content, err := discoverSkill(name); err == nil {
		return content, nil
	}
	if s, ok := catalog.Lookup(name); ok {
		return s.Body, nil
	}
	return "", fmt.Errorf("skill %q not found", name)
}

// LoadSkillFromPath reads a skill file directly from a filesystem path.
// Used by the `@<path>` shell sentinel.
func LoadSkillFromPath(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("load skill %q: %w", path, err)
	}
	return string(content), nil
}

// ListSkills enumerates every skill name discoverable from the current
// working directory: local overlays (with priority) merged with the upstream
// catalog. Used by tab completion in the shell.
func ListSkills() []string {
	seen := make(map[string]struct{})
	var skills []string

	for _, name := range listLocalSkills() {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			skills = append(skills, name)
		}
	}
	for _, s := range catalog.All() {
		if _, ok := seen[s.Name]; !ok {
			seen[s.Name] = struct{}{}
			skills = append(skills, s.Name)
		}
	}
	return skills
}

// listLocalSkills walks the local overlay directories and returns every
// skill discoverable on disk.
func listLocalSkills() []string {
	dirs := skillSearchDirs()
	seen := make(map[string]struct{})
	var skills []string

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				skillPath := filepath.Join(dir, name, "SKILL.md")
				if _, err := os.Stat(skillPath); err != nil {
					skillPath = filepath.Join(dir, name, "skill.md")
					if _, err := os.Stat(skillPath); err != nil {
						continue
					}
				}
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					skills = append(skills, name)
				}
			} else if skillName, ok := strings.CutSuffix(name, ".md"); ok {
				if _, dup := seen[skillName]; !dup {
					seen[skillName] = struct{}{}
					skills = append(skills, skillName)
				}
			}
		}
	}
	return skills
}

// discoverSkill searches the local overlay directories for a skill body.
// It does NOT consult the upstream catalog — callers wanting catalog
// fallback should use DiscoverSkill or resolveSkill.
func discoverSkill(name string) (string, error) {
	// Normalize name (case-insensitive, handle qualified names).
	name = strings.ToLower(name)
	parts := strings.SplitN(name, ":", 2)
	skillName := parts[len(parts)-1]

	searchDirs := skillSearchDirs()

	// Try multiple filename conventions (SKILL.md, skill.md, <name>.md).
	filenames := []string{
		filepath.Join(skillName, "SKILL.md"),
		filepath.Join(skillName, "skill.md"),
		skillName + ".md",
	}

	for _, dir := range searchDirs {
		for _, fn := range filenames {
			path := filepath.Join(dir, fn)
			content, err := os.ReadFile(path)
			if err == nil {
				return string(content), nil
			}
		}
	}

	return "", fmt.Errorf("skill %q not found", name)
}

// skillSearchDirs returns directories to search for local-overlay skills.
// The upstream catalog is *not* a directory — it is consulted via the
// catalog Go API (embedded into the binary).
func skillSearchDirs() []string {
	var dirs []string

	// Project directory — walk up from cwd looking for skill directories.
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			// .agents/ycode/skills is the canonical internal-lane path;
			// skills/ is honoured for backwards compatibility with any tree
			// that hasn't migrated yet.
			dirs = append(dirs, filepath.Join(dir, ".agents", "ycode", "skills"))
			dirs = append(dirs, filepath.Join(dir, "skills"))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Home directory.
	home, err := os.UserHomeDir()
	if err == nil {
		dirs = append(dirs, filepath.Join(home, ".agents", "ycode", "skills"))
	}

	// Environment variable.
	if envDir := os.Getenv("YCODE_SKILLS_DIR"); envDir != "" {
		dirs = append(dirs, envDir)
	}

	return dirs
}

// registerSkillListHandler registers the skill_list tool handler.
func registerSkillListHandler(r *Registry) {
	spec, ok := r.Get("skill_list")
	if !ok {
		return
	}
	spec.Handler = func(_ context.Context, input json.RawMessage) (string, error) {
		var params struct {
			Query string `json:"query,omitempty"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return "", fmt.Errorf("parse skill_list input: %w", err)
		}

		query := strings.ToLower(params.Query)
		seen := make(map[string]string) // name → "[internal]" or "[external]"

		// Local overlays (internal lane).
		for _, name := range listLocalSkills() {
			seen[name] = "[internal]"
		}
		// Upstream catalog (external lane) — only fills gaps from the local
		// overlay so that shadowed names show their internal source.
		for _, s := range catalog.All() {
			if _, ok := seen[s.Name]; !ok {
				seen[s.Name] = "[external]"
			}
		}

		if len(seen) == 0 {
			return "No skills found.", nil
		}

		// Build sorted output for stable display.
		names := make([]string, 0, len(seen))
		for name := range seen {
			if query == "" || strings.Contains(strings.ToLower(name), query) {
				names = append(names, name)
			}
		}
		// Simple insertion sort — list is small.
		for i := 1; i < len(names); i++ {
			for j := i; j > 0 && names[j-1] > names[j]; j-- {
				names[j-1], names[j] = names[j], names[j-1]
			}
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Available skills (%d):\n", len(names))
		for _, name := range names {
			fmt.Fprintf(&b, "  %-12s %s\n", seen[name], name)
		}
		return b.String(), nil
	}
}
