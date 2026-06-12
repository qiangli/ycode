//go:build !short

package toolexec

// The native git implementation moved to github.com/qiangli/coreutils/git
// (shared with outpost). The e2e suite predates the move and calls the
// old nativeXxx functions directly; these shims keep those call sites
// compiling and route them through the exact adapter the executor uses,
// so the suite still covers the production path end to end.

var (
	nativeAdd         = nativeGitFunc("add")
	nativeBlame       = nativeGitFunc("blame")
	nativeBranch      = nativeGitFunc("branch")
	nativeCatFile     = nativeGitFunc("cat-file")
	nativeCheckout    = nativeGitFunc("checkout")
	nativeCherryPick  = nativeGitFunc("cherry-pick")
	nativeCommit      = nativeGitFunc("commit")
	nativeCommitTree  = nativeGitFunc("commit-tree")
	nativeDiffTree    = nativeGitFunc("diff-tree")
	nativeFormatPatch = nativeGitFunc("format-patch")
	nativeGrep        = nativeGitFunc("grep")
	nativeHashObject  = nativeGitFunc("hash-object")
	nativeLog         = nativeGitFunc("log")
	nativeLsFiles     = nativeGitFunc("ls-files")
	nativeLsTree      = nativeGitFunc("ls-tree")
	nativeMerge       = nativeGitFunc("merge")
	nativeMergeBase   = nativeGitFunc("merge-base")
	nativeRebase      = nativeGitFunc("rebase")
	nativeRemote      = nativeGitFunc("remote")
	nativeReset       = nativeGitFunc("reset")
	nativeRevList     = nativeGitFunc("rev-list")
	nativeRevParse    = nativeGitFunc("rev-parse")
	nativeRm          = nativeGitFunc("rm")
	nativeShow        = nativeGitFunc("show")
	nativeShowRef     = nativeGitFunc("show-ref")
	nativeStatus      = nativeGitFunc("status")
	nativeSymbolicRef = nativeGitFunc("symbolic-ref")
	nativeTag         = nativeGitFunc("tag")
	nativeUpdateRef   = nativeGitFunc("update-ref")
	nativeWriteTree   = nativeGitFunc("write-tree")
)
