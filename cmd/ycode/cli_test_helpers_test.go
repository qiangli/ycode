package main

import (
	"os"
	"testing"

	"github.com/spf13/cobra"

	harnesscli "github.com/qiangli/ycode/internal/harness/cli"
	"github.com/qiangli/ycode/internal/harness/spec"
)

func testRoot(t *testing.T) *cobra.Command {
	t.Helper()
	doc, err := spec.Load(harnessFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	root, err := harnesscli.New(doc, dispatchCLI, harnesscli.Options{
		Version: version, Commit: commit, IsTerminal: false, LookupEnv: os.LookupEnv,
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
