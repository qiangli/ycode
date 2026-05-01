package toolexec

// gitDockerfile builds a minimal Alpine image with git and openssh.
// The image is ~8MB compressed and provides full git CLI compatibility.
const gitDockerfile = `FROM alpine:3.21
RUN apk add --no-cache git openssh-client
WORKDIR /workspace
`

// NewGitDef returns a ToolDef for git with container fallback.
// NativeFuncs provides in-process go-git implementations for common subcommands.
// When a native implementation returns ErrNotImplemented, the executor falls
// through to the host git binary (tier 2) or container (tier 3).
func NewGitDef() *ToolDef {
	return &ToolDef{
		Name:         "git",
		Binary:       "git",
		Image:        "ycode-git:latest",
		Dockerfile:   gitDockerfile,
		MountWorkDir: true,
		NativeFuncs: map[string]NativeFunc{
			// Phase 1: Read-only commands
			"rev-parse":  nativeRevParse,
			"status":     nativeStatus,
			"log":        nativeLog,
			"diff":       nativeDiff,
			"merge-base": nativeMergeBase,
			"rev-list":   nativeRevList,
			"config":     nativeConfig,
			// Phase 2: Write commands
			"add":      nativeAdd,
			"commit":   nativeCommit,
			"branch":   nativeBranch,
			"checkout": nativeCheckout,
			"stash":    nativeStash,
			// Phase 3: Complex commands (mostly ErrNotImplemented)
			"worktree":     nativeWorktree,
			"merge":        nativeMerge,
			"for-each-ref": nativeForEachRef,
			"remote":       nativeRemote,
		},
	}
}
