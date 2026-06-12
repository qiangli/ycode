package toolexec

import (
	"context"
	"errors"

	shgit "github.com/qiangli/coreutils/git"
)

// gitDockerfile builds a minimal Alpine image with git and openssh.
// The image is ~8MB compressed and provides full git CLI compatibility.
const gitDockerfile = `FROM alpine:3.21
RUN apk add --no-cache git openssh-client
WORKDIR /workspace
`

// NewGitDef returns a ToolDef for git with container fallback.
//
// Tier 1 (native) is the shared pure-Go implementation in
// github.com/qiangli/coreutils/git — the same package outpost's
// `outpost git` uses. It covers the common porcelain + plumbing
// subcommands (including pull and clone); anything it can't handle
// returns git.ErrUnsupported, which maps to ErrNotImplemented here so
// the executor falls through to the host git binary (tier 2) or the
// container (tier 3).
//
// History: the native layer originally lived in this package as
// git_native*.go; it was extracted to coreutils so ycode and outpost
// share one implementation. ycode's git e2e suite (git_e2e_test.go)
// still exercises it end-to-end through this adapter.
func NewGitDef() *ToolDef {
	subs := shgit.ExecCommands()
	funcs := make(map[string]NativeFunc, len(subs))
	for _, sub := range subs {
		funcs[sub] = nativeGitFunc(sub)
	}
	return &ToolDef{
		Name:         "git",
		Binary:       "git",
		Image:        "ycode-git:latest",
		Dockerfile:   gitDockerfile,
		MountWorkDir: true,
		NativeFuncs:  funcs,
	}
}

// nativeGitFunc adapts one coreutils/git subcommand to the NativeFunc
// shape: the executor strips the subcommand from args, Exec wants it
// back, and ErrUnsupported becomes the fall-through sentinel.
func nativeGitFunc(sub string) NativeFunc {
	return func(ctx context.Context, dir string, args []string) (*Result, error) {
		res, err := shgit.Exec(ctx, dir, append([]string{sub}, args...))
		if err != nil {
			if errors.Is(err, shgit.ErrUnsupported) {
				return nil, ErrNotImplemented
			}
			return nil, err
		}
		return &Result{Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: res.ExitCode}, nil
	}
}
