// Package genie carries genie's source — the files its `package` target puts
// in a bundle — so a program that links it (bashy) can build the bundle with
// no ycode checkout: genie is bashy's builtin agent.
package genie

import "embed"

// Source is genie's source tree, rooted at this directory. Keep the list in
// step with dag.md's package target; dist/ (build output) is never embedded.
//
//go:embed agent.yaml genie.bsh go.mod dag.md models.json README.md ATTRIBUTION.md LICENSE.md LICENSE-live-swe-agent.md LICENSE-mini-swe-agent.md lib prompts cmd fixture
var Source embed.FS
