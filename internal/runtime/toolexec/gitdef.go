package toolexec

// gitDockerfile builds a minimal Alpine image with git and openssh.
// The image is ~8MB compressed and provides full git CLI compatibility.
const gitDockerfile = `FROM alpine:3.21
RUN apk add --no-cache git openssh-client
WORKDIR /workspace
`

// NewGitDef returns a ToolDef for git with container fallback.
// NativeFuncs starts empty — the autonomous loop populates it over time
// as it implements native go-git replacements for each subcommand.
func NewGitDef() *ToolDef {
	return &ToolDef{
		Name:         "git",
		Binary:       "git",
		Image:        "ycode-git:latest",
		Dockerfile:   gitDockerfile,
		MountWorkDir: true,
		NativeFuncs:  map[string]NativeFunc{},
	}
}
