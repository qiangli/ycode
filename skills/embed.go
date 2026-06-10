// Package skills embeds the skill lane shipped with the ycode binary.
//
// Every directory here (skills/ycode-<name>/skill.md) is compiled into
// the binary and applies to ALL repos. `ycode init` installs editable
// copies to ~/.config/ycode/skills/ (managed: re-synced when the
// binary's embedded content changes); resolution falls back to the
// embedded copy when no on-disk overlay shadows it, so a bare binary
// works without any install step. Per-repo overrides go in
// .agents/ycode/skills/<name>/skill.md, which shadows both.
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
// the directory form ("ycode-weave") or the bare form ("weave").
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
