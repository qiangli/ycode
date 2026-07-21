package selfinit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qiangli/ycode/skills"
)

// WriteProjectFiles writes ycode's foreign-agent breadcrumb to
// <repo>/.agents/ycode/AGENTS.md. It is the ONLY in-repo file ycode
// will touch — by design.
//
// What we deliberately do NOT do:
//   - Patch <repo>/AGENTS.md or <repo>/CLAUDE.md. Those files belong
//     to the user / their team; ycode rewriting them on first contact
//     was the intrusive behavior we backed out of.
//   - Write <repo>/docs/backlog.md. The protocol doc ships in the
//     ycode source tree; foreign repos discover it via
//     .agents/ycode/AGENTS.md (which links to the GitHub copy) and
//     via the Foreman skill body.
//   - Run at all on a non-init invocation. Only `ycode init` calls
//     here; every other entry point leaves the repo untouched.
//
// Returns the list of files written (typically one) and a slice of
// warnings for non-fatal issues.
func WriteProjectFiles(repoRoot string) ([]string, []string, error) {
	if repoRoot == "" {
		return nil, nil, ErrNoGitRoot
	}
	longPath := filepath.Join(repoRoot, selfinitSubdir, "AGENTS.md")
	longContent := buildLongFormDoc()
	if err := writeFileIfChanged(longPath, longContent); err != nil {
		return nil, nil, fmt.Errorf("write %s: %w", longPath, err)
	}
	return []string{longPath}, nil, nil
}

// RootPointerSnippet returns the one-paragraph snippet a user can
// paste into their <repo>/AGENTS.md if they want their root file to
// link to ycode's capabilities. We do NOT write this anywhere — it's
// printed by `ycode init` and the user adds it manually if they want.
func RootPointerSnippet() string {
	return `## ycode (optional)

This repo can use [ycode](https://github.com/qiangli/ycode) as local
agentic infrastructure. Capability descriptions live at
` + "`.agents/ycode/AGENTS.md`" + `. If installed, run ` + "`ycode init --refresh`" + ` to
regenerate that file after updates.`
}

// WriteUserSkills installs every skill embedded in the binary (the
// top-level skills/ package) to ~/.config/ycode/skills/<name>/skill.md.
// The lane is managed: writeFileIfChanged re-syncs a file whenever the
// binary's embedded content differs, so upgrades propagate. Users who
// want a customized copy put it in a shadowing overlay
// (.agents/ycode/skills/ per-repo, or ~/.agents/ycode/skills/
// user-wide) — those always win over the managed lane. Idempotent.
//
// Resolution order at runtime: cwd-local → project (.agents/ycode/) →
// user overlay (~/.agents/ycode/) → managed (~/.config/ycode/) →
// embedded. First match wins.
func WriteUserSkills(home string) ([]string, error) {
	if home == "" {
		return nil, fmt.Errorf("selfinit: empty home")
	}
	var written []string
	embedded := make(map[string]struct{})
	for _, name := range skills.Names() {
		embedded[name] = struct{}{}
		body, ok := skills.Body(name)
		if !ok {
			continue
		}
		skillPath := filepath.Join(home, ".config", "ycode", "skills", name, "skill.md")
		if err := writeFileIfChanged(skillPath, body); err != nil {
			return written, fmt.Errorf("write %s: %w", skillPath, err)
		}
		written = append(written, skillPath)
	}
	// Prune managed entries the binary no longer ships (renamed,
	// or reclassified as contributor-internal). The ycode- prefix is
	// the managed namespace: user-authored skills under other names
	// are never touched; customized copies belong in the overlay
	// (~/.agents/ycode/skills/), which this never reaches.
	laneDir := filepath.Join(home, ".config", "ycode", "skills")
	if entries, err := os.ReadDir(laneDir); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !e.IsDir() || !strings.HasPrefix(name, "ycode-") {
				continue
			}
			if _, ok := embedded[name]; ok {
				continue
			}
			_ = os.RemoveAll(filepath.Join(laneDir, name))
		}
	}
	return written, nil
}

// writeFileIfChanged writes content atomically if the on-disk content
// differs. Returns nil if the file was already up to date (no rewrite,
// preserves mtime).
func writeFileIfChanged(path, content string) error {
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, []byte(content)) {
			return nil
		}
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// buildLongFormDoc builds the contents of <repo>/.agents/ycode/AGENTS.md (and,
// in greenfield, <repo>/AGENTS.md without the delimiter wrapping). The
// content is the shell-built-in capability block plus a Skills
// inventory, so foreign agents (Claude Code, OpenCode, Codex, …)
// discover ycode's skills by reading the file they already pull —
// without ycode writing into their personal config dirs.
//
// It advertises no MCP servers: ycode runs none (docs/plan-remove-mcp.md).
func buildLongFormDoc() string {
	var b strings.Builder
	b.WriteString("# ycode capabilities for this project\n\n")
	b.WriteString("This file is auto-generated by `ycode init`. ycode runs locally and exposes its capabilities as `yc <verb>` shell built-ins (see the block below, or `ycode shell --manifest`). It does not run an MCP server; there is nothing to add to your tool's `mcpServers` config.\n\n")
	b.WriteString(buildInstructionsBlock())
	b.WriteString("\n\n")

	// Skills inventory — non-intrusive discovery surface for foreign
	// agents. ycode does not write into ~/.claude/skills/, ~/.codex/,
	// etc.; agents pick up the inventory when they read this file.
	if len(SkillInventory) > 0 {
		b.WriteString("## Skills available via ycode\n\n")
		b.WriteString("Universal skills shipped with the ycode binary. Invoke from any chat with a leading slash (e.g. `/foreman`); ycode resolves the body from cwd → project → user (`~/.config/ycode/skills/`) → embedded. To customize a skill globally, edit the user-global copy; to override per-repo, drop a copy at `.agents/ycode/skills/<name>/skill.md`.\n\n")
		b.WriteString("| Skill | When to use | Body |\n")
		b.WriteString("|---|---|---|\n")
		for _, s := range SkillInventory {
			fmt.Fprintf(&b, "| `%s` — %s | %s | `%s` |\n", s.Name, s.Summary, s.When, s.BodyPath)
		}
		b.WriteString("\n")
	}

	b.WriteString("## How to use\n\n")
	b.WriteString("If a tool returns *connection refused*, run `ycode serve` first; capabilities are advertised in `~/.agents/ycode/manifest.json`.\n\n")
	b.WriteString("To register ycode in a foreign agentic CLI manually:\n\n")
	b.WriteString("```\nycode init --refresh\n```\n\n")
	b.WriteString("That regenerates this file plus the relevant entries in your tool's MCP config and memory file.\n")
	return b.String()
}
