// Package projectid resolves a stable identifier for the logical
// project a checkout belongs to, and computes filesystem paths under
// the user-home state dir keyed by that identifier.
//
// "Logical project" means the same id is produced regardless of where
// the repo is cloned on disk — two checkouts of github.com/foo/bar at
// /tmp/a and /tmp/b resolve to the same id. ycode runs one server
// instance per OS user; backlog, foreman, and other per-project state
// live under ~/.agents/ycode/projects/<sanitized-id>/.
//
// Resolution order (mirrors origin.Resolve's ID branch):
//
//  1. caller-supplied override (typically cfg.Project.ID)
//  2. normalized git remote "origin" URL
//  3. "cwd-hash:<sha8 of abs cwd>" fallback
//
// origin.Resolve delegates to this package so attribution and on-disk
// keying stay in lockstep.
package projectid

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
)

// Resolve returns the project ID given pre-resolved inputs. Used by
// callers that already have the remote URL (e.g. origin.Resolve, which
// also needs the raw remote for ProjectName).
func Resolve(idOverride, normalizedRemote, cwdAbs string) string {
	if id := strings.TrimSpace(idOverride); id != "" {
		return id
	}
	if normalizedRemote != "" {
		return normalizedRemote
	}
	return cwdHashID(cwdAbs)
}

// ResolveFromCwd is the convenience entry point: reads the git remote
// and computes the ID in one call. Best-effort — failures (not a repo,
// no origin) fall through to the cwd-hash fallback.
func ResolveFromCwd(ctx context.Context, cwd, idOverride string) string {
	cwdAbs := absOrSame(cwd)
	remote, _ := ReadGitRemote(ctx, cwd)
	return Resolve(idOverride, NormalizeRemote(remote), cwdAbs)
}

// NormalizeRemote turns a git remote URL into a stable, secret-free
// identifier of the form "<host>/<path>". Both https and ssh shapes
// converge:
//
//	https://user:tok@github.com/foo/bar.git  →  github.com/foo/bar
//	git@github.com:foo/bar.git               →  github.com/foo/bar
//	ssh://git@gitlab.example.com:2222/x/y    →  gitlab.example.com/x/y
//	/local/path/repo                         →  /local/path/repo (unchanged)
func NormalizeRemote(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "@") && !strings.Contains(raw, "://") {
		// git@github.com:foo/bar.git
		atIdx := strings.Index(raw, "@")
		colonIdx := strings.Index(raw[atIdx:], ":")
		if colonIdx > 0 {
			host := raw[atIdx+1 : atIdx+colonIdx]
			path := raw[atIdx+colonIdx+1:]
			return strings.ToLower(host) + "/" + trimDotGit(path)
		}
	}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.ToLower(u.Host) + trimDotGit(u.Path)
	}
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, ".") {
		return trimDotGit(raw)
	}
	return strings.ToLower(trimDotGit(raw))
}

// Sanitize converts a project ID to a filesystem-safe directory name.
// Injective: percent-encodes "/", ":", "\\", "%", and control bytes so
// two distinct IDs never collapse to the same on-disk path.
func Sanitize(id string) string {
	var b strings.Builder
	b.Grow(len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		if needsEncoding(c) {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func needsEncoding(c byte) bool {
	switch c {
	case '/', '\\', ':', '%', '<', '>', '"', '|', '?', '*':
		return true
	}
	return c < 0x20 || c == 0x7F
}

// StateDir returns the per-project state directory under homeAgents
// (typically ~/.agents/ycode). The id is sanitized before joining.
func StateDir(homeAgents, id string) string {
	return filepath.Join(homeAgents, "projects", Sanitize(id))
}

// BacklogDir returns the backlog markdown directory inside a stateDir.
func BacklogDir(stateDir string) string {
	return filepath.Join(stateDir, "backlog")
}

// ForemanDir returns the foreman state directory inside a stateDir.
func ForemanDir(stateDir string) string {
	return filepath.Join(stateDir, "foreman")
}

// ProjectSettingsPath returns the per-user-per-project settings.json
// path inside a stateDir.
func ProjectSettingsPath(stateDir string) string {
	return filepath.Join(stateDir, "settings.json")
}

// ReadGitRemote returns the "origin" remote URL of the repo at dir, or
// ok=false if not a git repo / no origin configured. Errors are
// swallowed (project-id resolution is best-effort).
//
// Uses os/exec directly rather than the project's git wrapper because
// project-id resolution must work even when a PATH shim or other
// environment quirk decorates git output — only a clean exit-zero
// stdout is treated as a valid URL.
func ReadGitRemote(ctx context.Context, dir string) (string, bool) {
	if dir == "" {
		return "", false
	}
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url")
	cmd.Dir = dir
	cmd.Stdout = &stdout
	// stderr is intentionally discarded; we only trust stdout on exit zero.
	if err := cmd.Run(); err != nil {
		return "", false
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", false
	}
	return out, true
}

func trimDotGit(s string) string {
	return strings.TrimSuffix(s, ".git")
}

func cwdHashID(abs string) string {
	sum := sha256.Sum256([]byte(abs))
	return "cwd-hash:" + hex.EncodeToString(sum[:4])
}

func absOrSame(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}
