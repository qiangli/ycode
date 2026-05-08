package selfinit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// markerFilename is the dotfile inside <repo>/.ycode/ that records the
// state hash of the most recent SelfInit run. Matching hash + present
// marker ⇒ skip work.
const markerFilename = ".init-done"

// noInitFilename is the per-repo opt-out marker. When this file exists
// inside <repo>/.ycode/, SelfInit refuses to do anything in this repo.
const noInitFilename = ".no-init"

// stateHash returns a stable hash over the inputs that, if changed,
// require a SelfInit refresh. Order-independent for capabilities so
// list reordering doesn't trigger spurious work.
func stateHash(version string, caps []CapabilitySpec, tools []string) string {
	parts := []string{"v=" + version}
	sortedTools := append([]string(nil), tools...)
	sort.Strings(sortedTools)
	for _, t := range sortedTools {
		parts = append(parts, "tool="+t)
	}
	keys := make([]string, len(caps))
	for i, c := range caps {
		keys[i] = capFingerprint(c)
	}
	sort.Strings(keys)
	parts = append(parts, keys...)
	h := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(h[:])
}

func capFingerprint(c CapabilitySpec) string {
	return fmt.Sprintf("cap|%s|%s|%s|%s|%s|%s",
		c.Name, c.Transport, c.Family, c.Command,
		strings.Join(c.Args, " "), c.URL)
}

// markerPath returns <repo>/.ycode/.init-done.
func markerPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".ycode", markerFilename)
}

// optOutPath returns <repo>/.ycode/.no-init.
func optOutPath(repoRoot string) string {
	return filepath.Join(repoRoot, ".ycode", noInitFilename)
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
	dir := filepath.Join(repoRoot, ".ycode")
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
	dir := filepath.Join(repoRoot, ".ycode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(optOutPath(repoRoot),
		[]byte("# Presence of this file disables ycode SelfInit for this repo.\n"),
		0o644)
}
