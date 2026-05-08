package main

import (
	"fmt"
	"os"
	"slices"

	giteacmd "code.gitea.io/gitea/cmd"
)

// maybeHandleGiteaHook short-circuits main() when ycode is invoked as
// a Gitea-generated git hook. Returns true if it handled the call
// (and main() should return immediately).
//
// Gitea writes hook scripts of the form:
//
//	<binary> hook --config=<app.ini> pre-receive
//
// These scripts run in a separate process during git push (or any
// internal Gitea git operation) and call back to the running Gitea
// via its internal HTTP API to validate permissions, update DB state,
// trigger webhooks, etc. Without this delegation:
//
//   - cobra rejects `--config=...` as an unknown flag.
//   - Even with cobra silenced, ycode has no `hook` subcommand and
//     would fall back to one-shot prompt mode.
//   - Gitea's PR/issue DB state never gets the post-receive callback,
//     so MergePR succeeds at the git layer but PR rows stay open.
//
// The fix delegates to Gitea's full CLI app (NewMainApp/RunMainApp),
// which dispatches the hook subcommand to runHookPreReceive /
// runHookUpdate / runHookPostReceive / runHookProcReceive.
//
// Detection scans argv for "hook" anywhere — the wrapping script may
// pass --config= before or after.
func maybeHandleGiteaHook() bool {
	if !slices.Contains(os.Args[1:], "hook") {
		return false
	}
	app := giteacmd.NewMainApp(giteacmd.AppVersion{Version: version})
	if err := giteacmd.RunMainApp(app, os.Args...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return true
}
