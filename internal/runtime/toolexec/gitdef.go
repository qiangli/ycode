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
			"reset":    nativeReset,
			"show":     nativeShow,
			"stash":    nativeStash,
			// Phase 3: Complex commands
			"worktree":     nativeWorktree,
			"merge":        nativeMerge,
			"tag":          nativeTag,
			"fetch":        nativeFetch,
			"grep":         nativeGrep,
			"ls-files":     nativeLsFiles,
			"blame":        nativeBlame,
			"for-each-ref": nativeForEachRef,
			"remote":       nativeRemote,
			// Tier 2: Server/write operations
			"push":         nativePush,
			"cherry-pick":  nativeCherryPick,
			"rebase":       nativeRebase,
			"apply":        nativeApply,
			"format-patch": nativeFormatPatch,
			"rm":           nativeRm,
			// Tier 3: Plumbing commands
			"cat-file":     nativeCatFile,
			"hash-object":  nativeHashObject,
			"read-tree":    nativeReadTree,
			"write-tree":   nativeWriteTree,
			"commit-tree":  nativeCommitTree,
			"symbolic-ref": nativeSymbolicRef,
			"update-ref":   nativeUpdateRef,
			"diff-tree":    nativeDiffTree,
			"ls-tree":      nativeLsTree,
			"show-ref":     nativeShowRef,
		},
	}
}
