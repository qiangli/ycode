// Package skills embeds the skill lane shipped with the ycode binary
// — the skills for regular USERS of ycode, applicable in any repo.
//
// Every directory here (skills/ycode-<name>/skill.md) is compiled into
// the binary and resolves from any cwd; `ycode init` additionally
// installs editable copies to ~/.config/ycode/skills/ (a managed lane,
// re-synced whenever the binary's embedded content changes). Per-repo
// or per-user overrides go in .agents/ycode/skills/ or
// ~/.agents/ycode/skills/, which shadow both.
//
// Skills for ycode CONTRIBUTORS (build, deploy, validate, audit, …)
// deliberately live outside this package, in the repo's
// .agents/ycode/skills/ workspace overlay — available exactly when
// the cwd is the ycode source tree, never bundled, never installed.
package skills

import (
	"embed"
	"io/fs"
	"sort"
	"strings"
)

//go:embed all:ycode-*
var FS embed.FS

// Names returns every embedded skill directory name (ycode-*), sorted.
func Names() []string {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			if _, err := fs.Stat(FS, e.Name()+"/skill.md"); err == nil {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	return names
}

// Body returns the embedded skill.md body for a skill. The name may be
// the directory form ("ycode-foreman") or the bare form ("foreman").
func Body(name string) (string, bool) {
	dir := name
	if !strings.HasPrefix(dir, "ycode-") {
		dir = "ycode-" + dir
	}
	data, err := fs.ReadFile(FS, dir+"/skill.md")
	if err != nil {
		return "", false
	}
	return string(data), true
}
