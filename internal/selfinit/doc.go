// Package selfinit makes ycode a first-class citizen in any local git repo.
//
// On every ycode entry point's startup, SelfInit runs once:
//
//   - walks up from cwd to find the git repo root (skips if none);
//   - checks an idempotency marker (skip if state hasn't changed);
//   - writes a project-scope ycode awareness file at <repo>/.ycode/AGENTS.md;
//   - patches <repo>/AGENTS.md and/or <repo>/CLAUDE.md with a small
//     reference block — or, in greenfield repos where neither exists,
//     creates AGENTS.md as a fully ycode-owned file (no delimiter);
//   - detects installed agentic tools (claude, opencode, codex, gemini)
//     and writes their user-scope MCP config + memory files;
//   - drops a marker so subsequent invocations no-op when state hasn't
//     drifted.
//
// The same code path is reused by the explicit `ycode init` command,
// the embedded `/init` slash command, and the auto-startup hook —
// no drift between manual and automatic flows.
//
// Layered design:
//
//   - types.go       — shared types (CapabilitySpec, Tool, Markers).
//   - manifest.go    — reads ~/.agents/ycode/manifest.json + baseline
//     fallback + ycode-binary bootstrap detection.
//   - injection.go   — markdown delimited-block splicing utilities.
//   - marker.go      — <repo>/.ycode/.init-done state hash.
//   - detect.go      — git-root walker, tool detection.
//   - project.go     — project-scope writes (greenfield + brownfield).
//   - selfinit.go    — Run(ctx, opts) orchestrator.
//
// Per-tool user-scope writers (claude.go, opencode.go, …) plug in via
// the Tool registry and run only when their tool is detected.
package selfinit
