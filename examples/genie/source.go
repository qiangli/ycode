// Package genie carries genie's source — the files its `package` target puts
// in a bundle — so a program that links it (bashy) can build the bundle with
// no ycode checkout: genie is bashy's builtin agent.
package genie

import "embed"

// Source is genie's source tree, rooted at this directory. Keep the list in
// step with dag.md's package target; dist/ (build output) is never embedded.
// The directories use all: — plain //go:embed silently drops files named
// _* or .* (the fixture's tests/__init__.py went missing that way).
//
//go:embed agent.yaml genie.bsh go.mod dag.md models.json README.md ATTRIBUTION.md LICENSE.md LICENSE-live-swe-agent.md LICENSE-mini-swe-agent.md all:lib all:prompts all:cmd all:fixture
var Source embed.FS
