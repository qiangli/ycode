package selfinit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// markerFilename is the dotfile inside <repo>/.agents/ycode/ that records
// the state hash of the most recent SelfInit run. Matching hash + present
// marker ⇒ skip work.
const markerFilename = ".init-done"

// noInitFilename is the per-repo opt-out marker. When this file exists
// inside <repo>/.agents/ycode/, SelfInit refuses to do anything in this repo.
const noInitFilename = ".no-init"

// selfinitSubdir is the per-repo directory where SelfInit places its
// state and generated docs. Matches the convention used elsewhere in
// ycode (memory, plan mode, skills, settings).
var selfinitSubdir = filepath.Join(".agents", "ycode")

// stateHash returns a stable hash over the inputs that, if changed,
// require a SelfInit refresh: the ycode version (so a binary upgrade
// regenerates the docs it embeds) and the set of detected foreign
// tools. Order-independent for tools so registry reordering doesn't
// trigger spurious work.
func stateHash(version string, tools []string) string {
	parts := []string{"v=" + version}
	sortedTools := append([]string(nil), tools...)
	sort.Strings(sortedTools)
	for _, t := range sortedTools {
		parts = append(parts, "tool="+t)
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(h[:])
}

// markerPath returns <repo>/.agents/ycode/.init-done.
func markerPath(repoRoot string) string {
	return filepath.Join(repoRoot, selfinitSubdir, markerFilename)
}

// optOutPath returns <repo>/.agents/ycode/.no-init.
func optOutPath(repoRoot string) string {
	return filepath.Join(repoRoot, selfinitSubdir, noInitFilename)
}

// MarkerMatches reports whether the on-disk marker equals the expected
// state hash. Missing or unreadable marker ⇒ false.
func MarkerMatches(repoRoot, want string) bool {
	got, err := os.ReadFile(markerPath(repoRoot))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(got)) == want
}

// WriteMarker stores the state hash atomically.
func WriteMarker(repoRoot, hash string) error {
	dir := filepath.Join(repoRoot, selfinitSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := markerPath(repoRoot) + ".tmp"
	if err := os.WriteFile(tmp, []byte(hash+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, markerPath(repoRoot))
}

// IsOptedOut reports whether the per-repo opt-out marker is present.
func IsOptedOut(repoRoot string) bool {
	_, err := os.Stat(optOutPath(repoRoot))
	return err == nil
}

// WriteOptOut creates the opt-out marker. Used by `ycode init --opt-out`.
func WriteOptOut(repoRoot string) error {
	dir := filepath.Join(repoRoot, selfinitSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(optOutPath(repoRoot),
		[]byte("# Presence of this file disables ycode SelfInit for this repo.\n"),
		0o644)
}
