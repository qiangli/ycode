package weavesetup

import (
	"fmt"
	"os"
	"path/filepath"
)

const preCommitHookContent = `#!/bin/sh
# ycode loom v2 — installed by 'ycode weave' first-run setup.
#
# Layer 3 of the defense-in-depth model: refuses any commit whose
# author identity matches an agent (email ends in @ycode.local). This
# prevents a misconfigured tool that bypassed auto-attach from
# silently writing agent-flavored commits into your working tree.
#
# Honest commits (your own git identity) pass through unaffected.

author_email=$(git var GIT_AUTHOR_EMAIL)
case "$author_email" in
	*@ycode.local)
		echo "ycode loom: refusing agent-author commit ($author_email) in your working tree." >&2
		echo "ycode loom: run agents under 'ycode weave start' so they commit inside their sandbox instead." >&2
		exit 1
		;;
esac
exit 0
`

// installPreCommitHook drops the loom-v2 pre-commit hook into the
// user's .git/hooks/. Idempotent at the byte level: if the file
// already exists with the same content, no write happens (preserves
// mtime so downstream tools relying on it don't see false changes).
// If a DIFFERENT pre-commit hook is already installed, the install
// fails fast with a clear error rather than overwriting user work.
//
// On non-git directories (no .git), errors with "not a git repo".
func installPreCommitHook(hostCWD string) error {
	hookDir := filepath.Join(hostCWD, ".git", "hooks")
	st, err := os.Stat(filepath.Join(hostCWD, ".git"))
	if err != nil || !st.IsDir() {
		return fmt.Errorf("not a git repository at %s", hostCWD)
	}
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return fmt.Errorf("mkdir hooks dir: %w", err)
	}
	target := filepath.Join(hookDir, "pre-commit")

	// Check existing file.
	existing, err := os.ReadFile(target)
	if err == nil {
		if string(existing) == preCommitHookContent {
			return nil // already installed, byte-identical
		}
		// User has a different pre-commit hook. Don't clobber.
		return fmt.Errorf("a different pre-commit hook is already installed at %s; review and remove it manually before running weave setup again", target)
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("stat existing hook: %w", err)
	}

	if err := os.WriteFile(target, []byte(preCommitHookContent), 0o755); err != nil {
		return fmt.Errorf("write hook: %w", err)
	}
	return nil
}
