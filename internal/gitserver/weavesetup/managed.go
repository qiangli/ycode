package weavesetup

import (
	"os"
	"path/filepath"
)

// IsLoomManaged reports whether hostCWD is a project that's been set
// up with weave (i.e. has a .ycode/loom.yaml marker).
//
// Integrated tools (claude-code, codex, opencode) call this from their
// selfinit-installed startup check: if true AND YCODE_LOOM_ID is
// unset, the tool refuses to launch with a message pointing the user
// at `ycode weave start` instead. This is Defense Layer 2 from the
// design's "Defense in depth" section.
//
// Safe to call from anywhere; never errors (treats every failure as
// "not managed" — fail-open, since refusing to launch is the
// consequence of true).
func IsLoomManaged(hostCWD string) bool {
	if hostCWD == "" {
		return false
	}
	path := filepath.Join(hostCWD, configRelPath)
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !st.IsDir() && st.Size() > 0
}

// IsAttached reports whether the current process is running inside a
// loom workspace (YCODE_LOOM_ID env var set by wrap auto-attach).
// Used together with IsLoomManaged: refuse to launch when
// IsLoomManaged && !IsAttached.
func IsAttached() bool {
	return os.Getenv("YCODE_LOOM_ID") != ""
}
