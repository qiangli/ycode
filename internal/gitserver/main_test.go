package gitserver_test

import (
	"os"
	"testing"

	giteacmd "code.gitea.io/gitea/cmd"
)

// TestMain intercepts test-binary invocations that come from Gitea's
// generated git hooks. Gitea's bare repos have pre-receive /
// update / post-receive scripts that exec the binary returned by
// `os.Executable()` — in tests, our test binary; in production,
// `ycode`. Both need a `hook` subcommand that delegates to Gitea's
// real hook handlers, otherwise:
//
//   - Without delegation, Gitea's PR DB never gets the post-receive
//     callback, so MergePR succeeds at the git layer but the PR row
//     stays in state="open" — breaking integration assertions.
//   - The naive "exit 0 on hook" workaround skips the post-receive
//     call entirely, with the same effect.
//
// We delegate by running Gitea's full CLI with our argv. The cli/v3
// router dispatches the `hook` subcommand to Gitea's runHookPreReceive
// / runHookPostReceive / runHookUpdate handlers, which call back to
// the running Gitea via its internal HTTP API and update DB state
// correctly.
//
// Production has a parallel fix at cmd/ycode/hook.go that does the
// same delegation for the `ycode` binary.
func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		if arg == "hook" {
			app := giteacmd.NewMainApp(giteacmd.AppVersion{Version: "test"})
			if err := giteacmd.RunMainApp(app, os.Args...); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}
