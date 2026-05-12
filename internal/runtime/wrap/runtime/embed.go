// Package runtime owns the language-runtime patcher payloads that
// `ycode wrap` injects into wrapped agent processes. Today it carries
// two embedded blobs:
//
//   - python/sitecustomize.py — wraps subprocess.Popen / run /
//     check_output / call / os.system / os.popen
//   - node/ycode-trace.cjs — wraps child_process.spawn / exec /
//     execFile / fork (and the sync variants)
//
// Materialize(shimDir, langs) writes the requested blobs to
// shimDir/<lang>/ so the parent wrap process can then point the
// wrapped agent's PYTHONPATH / NODE_OPTIONS at them.
//
// The blobs are embedded so the wrap shim works as a single binary
// — no on-disk dependency on the source tree at runtime, no $GOPATH
// indirection for end users.
package runtime

import (
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
)

//go:embed python/sitecustomize.py node/ycode-trace.cjs
var assets embed.FS

// Lang names the supported runtimes. Keys match what wrap.Profile
// .RuntimeHooks values must be ("python", "node").
type Lang string

const (
	LangPython Lang = "python"
	LangNode   Lang = "node"
)

// SupportedLangs returns the runtimes Materialize knows how to
// install, sorted for stable output. Useful for `--help` text and
// validation of user-supplied --runtime-hooks values.
func SupportedLangs() []string {
	return []string{string(LangNode), string(LangPython)}
}

// langAssets maps each supported runtime to the embedded files it
// should drop under shimDir/<lang>/. New runtimes register here.
var langAssets = map[Lang][]string{
	LangPython: {"python/sitecustomize.py"},
	LangNode:   {"node/ycode-trace.cjs"},
}

// Materialize writes the runtime hooks listed in langs to
// shimDir/<lang>/. Returns the map of env-var overrides the caller
// must inject into the wrapped agent's environment to actually
// activate each hook (PYTHONPATH for python, NODE_OPTIONS for node).
//
// Idempotent: re-running against the same shimDir overwrites the
// blobs verbatim. Unknown lang names are logged at debug and
// silently skipped — the caller (wrap.Run) is in the fail-open
// posture for runtime hooks per the plan.
func Materialize(shimDir string, langs []string) (map[string]string, error) {
	if shimDir == "" {
		return nil, fmt.Errorf("runtime.Materialize: empty shim dir")
	}
	if len(langs) == 0 {
		return nil, nil
	}

	envOverrides := make(map[string]string)
	seen := make(map[Lang]bool)

	// Sort for deterministic order so the PYTHONPATH / NODE_OPTIONS
	// the operator sees in `ps` is stable.
	dedup := append([]string{}, langs...)
	sort.Strings(dedup)

	for _, name := range dedup {
		lang := Lang(name)
		if seen[lang] {
			continue
		}
		seen[lang] = true
		paths, ok := langAssets[lang]
		if !ok {
			slog.Debug("wrap runtime: unknown lang; skipping", "lang", name)
			continue
		}
		if err := materializeLang(shimDir, paths); err != nil {
			return nil, fmt.Errorf("materialize %s: %w", name, err)
		}
		switch lang {
		case LangPython:
			// Prepend our dir to PYTHONPATH so the wrapped agent's
			// existing PYTHONPATH still works. The caller merges
			// against the inherited env.
			envOverrides["__PYTHONPATH_PREPEND__"] = filepath.Join(shimDir, "python")
		case LangNode:
			// NODE_OPTIONS supports a space-separated list of flags;
			// callers append to whatever the wrapped agent already has.
			envOverrides["__NODE_OPTIONS_APPEND__"] = "--require=" + filepath.Join(shimDir, "node", "ycode-trace.cjs")
		}
	}
	return envOverrides, nil
}

func materializeLang(shimDir string, embeddedPaths []string) error {
	for _, embeddedPath := range embeddedPaths {
		data, err := fs.ReadFile(assets, embeddedPath)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", embeddedPath, err)
		}
		dst := filepath.Join(shimDir, embeddedPath)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
	}
	return nil
}
