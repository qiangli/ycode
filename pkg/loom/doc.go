// Package loom is ycode's git-workspace-substrate service.
//
// loom hands each of a foreign agentic coding tool's sub-agents an
// isolated clone+branch+author identity, so N parallel sub-agents can
// attack the same repo without stepping on each other. Convergence
// happens through a separate merger/CI gate.
//
// This is infrastructure: ycode provides the substrate, the foreign
// tool keeps its own agent loop. Callers interact with five verbs:
// Lease, Push, Merge, Status, Release.
//
// loom sits in the same lineage as the other reusable substrates ycode
// exposes to foreign tools — alongside pkg/memex (memory + graph),
// podman (sandbox), ollama (local inference), otel (observability).
//
// # Backends
//
// The Service is backed by a Backend interface, abstracting the
// underlying git/forge operations. The default implementation lives in
// internal/gitserver/loom and wraps ycode's embedded Gitea + native
// agents/projects/collab/merger primitives. External Go consumers can
// supply their own Backend if they need a non-Gitea forge.
//
// # Stability
//
// The public surface (Service, types, LeaseStore) is v0/unstable and may
// evolve until loom graduates from the experimental tier.
package loom
