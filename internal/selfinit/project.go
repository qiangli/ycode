package selfinit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// referenceBlock is the small AGENTS.md / CLAUDE.md snippet inserted
// in brownfield repos. The body is intentionally concise — readers
// follow the link to .agents/ycode/AGENTS.md for capability detail.
const referenceBlock = `## ycode

This repo expects [ycode](https://github.com/qiangli/ycode) running locally as
agentic infrastructure. When acting as an agentic coding tool, see
[` + "`" + `.agents/ycode/AGENTS.md` + "`" + `](.agents/ycode/AGENTS.md) for capability descriptions and when to
prefer them. Run ` + "`" + `ycode init --refresh` + "`" + ` to update this section.

### Self-Bootstrap (Foreman role)

You are the **Foreman** for this session. The Boss → Foreman → Worker
protocol is universal across every ycode-aware repo. When helping the
user plan, write tasks as ` + "`docs/backlog/<slug>.md`" + ` files (frontmatter:
` + "`title`" + `, ` + "`priority: p1|p2|p3`" + `, ` + "`state: open`" + `). When starting cold with no
specific user task, follow ` + "`.agents/ycode/skills/ycode-foreman/skill.md`" + `
(invoke as ` + "`/foreman`" + `). Boss control: ` + "`ycode foreman pause/resume/stop/skip/prio/tell/status`" + `.
Full protocol: [` + "`docs/backlog.md`" + `](docs/backlog.md).`

// WriteProjectFiles regenerates <repo>/.agents/ycode/AGENTS.md (long-form
// awareness, manifest-derived) and patches <repo>/AGENTS.md and/or
// <repo>/CLAUDE.md (or creates one of them in greenfield repos) per
// the rules described in the package doc.
//
// Returns the list of files written/patched, and a slice of warnings
// for non-fatal issues (e.g. failed write to a single file when others
// succeeded).
func WriteProjectFiles(repoRoot string, caps []CapabilitySpec) ([]string, []string, error) {
	if repoRoot == "" {
		return nil, nil, ErrNoGitRoot
	}
	var written []string
	var warnings []string

	// 1. Always write .agents/ycode/AGENTS.md — the canonical long-form copy.
	longPath := filepath.Join(repoRoot, selfinitSubdir, "AGENTS.md")
	longContent := buildLongFormDoc(caps)
	if err := writeFileIfChanged(longPath, longContent); err != nil {
		return nil, nil, fmt.Errorf("write %s: %w", longPath, err)
	}
	written = append(written, longPath)

	// 2. Decide which root-level file(s) to update.
	agentsPath := filepath.Join(repoRoot, "AGENTS.md")
	claudePath := filepath.Join(repoRoot, "CLAUDE.md")
	agentsExists := fileExists(agentsPath)
	claudeExists := fileExists(claudePath)

	switch {
	case agentsExists && claudeExists:
		// Brownfield, both — patch both.
		for _, p := range []string{agentsPath, claudePath} {
			if err := patchExisting(p, caps); err != nil {
				warnings = append(warnings, fmt.Sprintf("patch %s: %v", p, err))
				continue
			}
			written = append(written, p)
		}
	case agentsExists:
		if err := patchExisting(agentsPath, caps); err != nil {
			warnings = append(warnings, fmt.Sprintf("patch %s: %v", agentsPath, err))
		} else {
			written = append(written, agentsPath)
		}
	case claudeExists:
		if err := patchExisting(claudePath, caps); err != nil {
			warnings = append(warnings, fmt.Sprintf("patch %s: %v", claudePath, err))
		} else {
			written = append(written, claudePath)
		}
	default:
		// Greenfield: ycode owns AGENTS.md outright.
		ownedContent := OwnedMarker + "\n\n" + buildLongFormDoc(caps)
		if err := writeFileIfChanged(agentsPath, ownedContent); err != nil {
			return nil, warnings, fmt.Errorf("write greenfield %s: %w", agentsPath, err)
		}
		written = append(written, agentsPath)
	}

	// 3. Install the Foreman protocol scaffolding (universal).
	foremanWritten, foremanWarnings, err := writeForemanProtocol(repoRoot)
	warnings = append(warnings, foremanWarnings...)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("foreman protocol: %v", err))
	}
	written = append(written, foremanWritten...)

	return written, warnings, nil
}

// WriteForemanUserSkill writes the canonical /foreman skill body to
// ~/.config/ycode/skills/ycode-foreman/skill.md. The skill is universal
// — same body for every repo — so it lives at the user-global level
// rather than being copied into each project. The binary embeds the
// canonical source; this function ensures it's also available on disk
// for any agent that prefers to read a file. Idempotent.
//
// Resolution order at runtime: cwd-local → project (.agents/ycode/) →
// user (~/.config/ycode/) → embedded. First match wins.
func WriteForemanUserSkill(home string) (string, error) {
	if home == "" {
		return "", fmt.Errorf("selfinit: empty home")
	}
	skillPath := filepath.Join(home, ".config", "ycode", "skills", "ycode-foreman", "skill.md")
	if err := writeFileIfChanged(skillPath, foremanSkillMD); err != nil {
		return "", fmt.Errorf("write %s: %w", skillPath, err)
	}
	return skillPath, nil
}

// writeForemanProtocol drops the per-project Boss → Foreman → Worker
// scaffolding into <repo>: docs/backlog.md (protocol doc) and an empty
// docs/backlog/ with a README. The /foreman skill itself is NOT
// written here — it lives at the user-global level via
// WriteForemanUserSkill so the same body serves every repo. All writes
// are idempotent. Existing files are not overwritten unless content
// drifted from the embedded canonical.
func writeForemanProtocol(repoRoot string) ([]string, []string, error) {
	var written []string
	var warnings []string

	protocolPath := filepath.Join(repoRoot, "docs", "backlog.md")
	if err := writeFileIfChanged(protocolPath, backlogProtocolMD); err != nil {
		warnings = append(warnings, fmt.Sprintf("write %s: %v", protocolPath, err))
	} else {
		written = append(written, protocolPath)
	}

	backlogDir := filepath.Join(repoRoot, "docs", "backlog")
	if err := os.MkdirAll(backlogDir, 0o755); err != nil {
		warnings = append(warnings, fmt.Sprintf("mkdir %s: %v", backlogDir, err))
	} else {
		readmePath := filepath.Join(backlogDir, "README.md")
		// Only seed README if the dir is otherwise empty — don't clobber
		// a user-authored backlog README.
		if _, err := os.Stat(readmePath); os.IsNotExist(err) {
			if err := writeFileIfChanged(readmePath, backlogReadme); err != nil {
				warnings = append(warnings, fmt.Sprintf("write %s: %v", readmePath, err))
			} else {
				written = append(written, readmePath)
			}
		}
	}

	return written, warnings, nil
}

// backlogReadme is the seed README dropped into a fresh docs/backlog/.
const backlogReadme = `# docs/backlog/

Canonical task list. **One ` + "`.md`" + ` per task, slug = filename stem.**
See [` + "`docs/backlog.md`" + `](../backlog.md) for the source-of-truth
contract, the Boss → Foreman → Worker chain, the Boss control
protocol, and the reconciler semantics.

This ` + "`README.md`" + ` is not an issue — the reconciler skips it.

## Adding a new task

` + "```bash" + `
ycode backlog new "Implement <feature>" --priority p1
ycode backlog list                  # show all
ycode backlog list --priority p1    # only top tier
ycode backlog show <slug>           # render one
` + "```" + `

The reconciler (running inside ` + "`ycode serve`" + `) syncs new entries to
Gitea on its next 60s poll; force a sync with ` + "`ycode backlog reconcile`" + `.
`

// patchExisting reads path, splices/replaces the YCODE delimited block,
// and writes back if changed. If the file's first non-empty line is
// the OwnedMarker, ycode owns the whole file and we regenerate it
// fully. Otherwise the file is brownfield: splice in the delimited
// block.
//
// Note: when a previously-greenfield AGENTS.md has had the OwnedMarker
// removed by the user, IsOwnedFile returns false and we treat the
// file as brownfield; the next refresh appends a delimited block, and
// the user's manual edits are preserved.
func patchExisting(path string, caps []CapabilitySpec) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(body)

	if IsOwnedFile(s) {
		ownedContent := OwnedMarker + "\n\n" + buildLongFormDoc(caps)
		return writeFileIfChanged(path, ownedContent)
	}

	new := SpliceBlock(s, referenceBlock)
	if new == s {
		return nil
	}
	return writeFileIfChanged(path, new)
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// buildLongFormDoc builds the contents of <repo>/.agents/ycode/AGENTS.md (and,
// in greenfield, <repo>/AGENTS.md without the delimiter wrapping). The
// content is fully manifest-derived: one bullet per capability family
// with a human description.
func buildLongFormDoc(caps []CapabilitySpec) string {
	var b strings.Builder
	b.WriteString("# ycode capabilities for this project\n\n")
	b.WriteString("This file is auto-generated by `ycode init`. ycode is running locally and exposes services over MCP. When acting as an agentic coding tool, prefer these capabilities in the situations described:\n\n")
	for _, c := range caps {
		fmt.Fprintf(&b, "- **`%s`** — %s\n", c.Name, FamilyDescription(c.Family))
	}
	b.WriteString("\n")
	b.WriteString("## How to use\n\n")
	b.WriteString("If a tool returns *connection refused*, run `ycode serve` first; capabilities are advertised in `~/.agents/ycode/manifest.json`.\n\n")
	b.WriteString("To register ycode in a foreign agentic CLI manually:\n\n")
	b.WriteString("```\nycode init --refresh\n```\n\n")
	b.WriteString("That regenerates this file plus the relevant entries in your tool's MCP config and memory file.\n")
	return b.String()
}
