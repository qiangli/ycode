# GEMINI.md - Project Instructions for ycode

This file provides context and instructions for AI agents working on the `ycode` project.

## Project Overview

**ycode** is a pure Go CLI agent harness designed for autonomous software development. It aims to be a single static binary with only permissive-license dependencies (MIT, Apache-2.0, BSD).

### Core Technologies
- **Language:** Go 1.26+
- **Database:** SQLite (embedded via Gitea/Bbolt)
- **Messaging:** NATS
- **Observability:** OpenTelemetry (Traces, Metrics, Logs), Prometheus, Jaeger, VictoriaLogs, Perses.
- **Inference:** Embedded Ollama/llama.cpp.
- **Containers:** Embedded Podman.
- **Git Server:** Embedded Gitea.

### Architecture
- **Entry Point:** `cmd/ycode/main.go` using Cobra CLI.
- **Main App Loop:** `internal/cli/app.go` (REPL) and `internal/runtime/conversation/runtime.go`.
- **Registry:** Features are defined in `internal/features/registry.yaml`.
- **Vendorized Deps:** Submodules under `external/` and read-only reference code under `priorart/`.

## Building and Running

### First-Time Setup
You **MUST** run this once to initialize submodules and generate embedded assets:
```bash
make init
```

### Key Commands
- **Build full quality gate:** `make build` (tidy → fmt → vet → compile → test → verify)
- **Quick compile:** `make compile` (binary at `bin/ycode`)
- **Install to ~/bin:** `make install`
- **Unit tests:** `make test` (runs `-short -race`)
- **All tests:** `make test-all` (unit, container, gitserver, TUI, integration, browser)
- **CI Parity:** `make ci` (runs the GitHub Actions matrix in Docker)

### Build Tags
The build system uses several auto-detected tags based on available compressed assets (`.gz` files):
- `sqlite`, `sqlite_unlock_notify`, `bindata` (default)
- `embed_runner`, `embed_vfkit`, `embed_podman`, `embed_gvproxy` (added if assets exist)

## Development Conventions

### Layered Build System
1. **Makefile:** Dependency graph only. No multi-line shell logic.
2. **scripts/:** Bash orchestration (sequencing, environment, processes). No assertions.
3. **Go:** All logic, including unit/integration tests and assertions.

### Project Structure Rules
- **`internal/`:** Implementation details.
- **`pkg/`:** Reusable packages (some are workspace members in `go.work`).
- **`external/`:** Submodules. Do not modify directly; update the SHA.
- **`priorart/`:** **READ-ONLY.** Never modify these files.
- **`peers/`:** Local clones of related repos for side-by-side development.

### Coding Standards
- **No package-level mutable state:** Use `RuntimeContext`.
- **Structured Logging:** Use the logger from `RuntimeContext`, avoid `fmt.Println` or `log.Printf`.
- **Testing:**
  - Unit tests next to source in `*_test.go`. Use `testing.Short()` to skip slow tests.
  - Integration tests in `internal/integration/` with `//go:build integration`.
  - No test logic in Bash scripts.

### Git & Commits
- **Prefixes:** Use prefixes like `fix:`, `feat:`, `docs:`, `test:`.
- **Staging:** Stage files by name. **NEVER** use `git add .` or `git add -A`.
- **Pre-commit:** Always run `make build` before committing.

## Agent-Specific Tools (`yc <verb>`)
The project includes a suite of specialized tools exposed via the `ycode` binary (often aliased as `yc` in documentation). Use these over standard Unix tools when possible:

| Command | Use For |
|---------|---------|
| `yc symbols <path>` | AST-aware symbol listing |
| `yc repomap` | High-level project orientation |
| `yc search-symbols` | Finding identifier definitions |
| `yc refs <symbol>` | Finding callers/references |
| `yc test` | Framework-aware test execution |
| `yc lsp <cmd>` | Querying Language Server Protocol |
| `yc remember/recall` | Managing agent memory |

## Documentation References
- `docs/strategy.md`: Feature-tier policy and operating principles.
- `docs/architecture.md`: Design decisions and component details.
- `docs/instructions.md`: Detailed shared conventions and skill system.
- `docs/pipeline.md`: The six-step dev pipeline (research → plan → build/test → evaluate → commit → codify).
