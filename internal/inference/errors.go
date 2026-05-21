package inference

import "errors"

// ErrRunnerNotInstalled is returned when the embedded inference runner
// is not compiled into this binary. Surfaced to the user with an
// actionable "reinstall ycode" message — the runner is part of the
// shipped substrate, not something a user assembles by hand.
//
// The canonical hard-stop is in cmd/ycode/serve.go runAllServices,
// which fails before any component is constructed; the stack manager
// otherwise demotes per-component Start errors to warnings, so a
// component-level check alone wouldn't actually stop serve from
// coming up.
var ErrRunnerNotInstalled = errors.New("this ycode binary is missing the embedded inference runner; please reinstall ycode from https://github.com/qiangli/ycode/releases (or set `inference.enabled: false` in settings.json to opt out of local inference)")
