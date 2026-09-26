// Command ycode is the standalone front door of the ycode engine (the YAML
// agent runtime); the implementation is pkg/ycodecli, which bashy also mounts
// as `bashy ycode`.
package main

import (
	"os"

	"github.com/qiangli/ycode/pkg/ycodecli"
)

// Set via -ldflags at build time.
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	ycodecli.SetBuild(version, commit)
	os.Exit(ycodecli.Main(os.Args[1:]))
}
