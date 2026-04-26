# Embedding Gitea

How Gitea is embedded in ycode as the local git server for agent swarm workspace coordination. Follows the Ollama/Memos embedding pattern.

## Goal

Provide agent swarms with isolated workspaces (branches/worktrees), merge workflows, conflict detection, and a web UI for human review of agent changes. Gitea serves as the coordination point for swarm collaboration — agents push to their branches independently, merges happen through PRs.

## Why Gitea

Evaluated Gitea vs Gogs:

| Criterion | Gitea | Gogs |
|-----------|-------|------|
| Pure Go | Yes | Yes |
| License | MIT | MIT |
| Go SDK | Official (`code.gitea.io/sdk/gitea`) | None |
| Maintenance | Very active | Maintenance mode |
| CI/CD | Gitea Actions | None |
| API | Comprehensive REST | Basic REST |
| Binary size | ~180-250 MB | ~50-80 MB |

Gitea chosen for: official Go SDK (critical for agent automation), Gitea Actions (agent task orchestration), active maintenance (security patches), and comprehensive API. Size acceptable given ycode already embeds multiple large services.

## Architecture

```
ycode binary
├── external/gitea/            ← git submodule (upstream: qiangli/gitea)
│   └── embed/embed.go         ← settings wrapper for in-process init
├── internal/gitserver/
│   ├── server.go              ← Server (binary discovery, config gen, lifecycle)
│   ├── component.go           ← GitServerComponent (observability.Component)
│   ├── workspace.go           ← Agent workspace isolation (worktrees)
│   ├── api.go                 ← Typed Gitea REST API client
│   └── integration_test.go    ← Integration tests for workspace ops
└── cmd/ycode/serve.go         ← registration in buildStackManager()
```

## Submodule Pattern

```
go.mod:   replace code.gitea.io/gitea => ./external/gitea
import:   "code.gitea.io/gitea/embed" → resolves to external/gitea/embed/
```

The embed package wraps Gitea's `modules/setting` for in-process configuration. Anchor import in `internal/gitserver/server.go` preserves dependencies through `go mod tidy`.

## Server

The `Server` manages Gitea as a subprocess with auto-configuration:

1. **Binary discovery**:
   - `$YCODE_GITEA_PATH` environment variable
   - Adjacent to ycode binary: `$(dirname ycode)/gitea`
   - System PATH: `which gitea`

2. **Auto-configured `app.ini`** for zero-friction local use:
   - SQLite database (no external DB)
   - HTTP-only (SSH disabled)
   - Registration disabled
   - Mailer disabled
   - Install lock enabled (skip setup wizard)
   - Offline mode
   - Ephemeral port allocation

3. **Health check**: Polls HTTP root until responsive (10s timeout)

## GitServerComponent

Implements `observability.Component`:

```go
func (g *GitServerComponent) Name() string             // "git"
func (g *GitServerComponent) Start(ctx) error           // start Gitea subprocess
func (g *GitServerComponent) Stop(ctx) error            // graceful shutdown
func (g *GitServerComponent) Healthy() bool
func (g *GitServerComponent) HTTPHandler() http.Handler  // reverse proxy to Gitea
func (g *GitServerComponent) Port() int
```

Registered in StackManager at `/git/` path prefix. Provides OTEL tracing for start/stop lifecycle events.

## Agent Workspace Flow

Two workspace modes for different agent types:

### Read-Only (Explore, Plan agents)
```go
info, _ := PrepareWorkspace(ctx, repoDir, agentID, WorkspaceReadOnly)
// info.Path = repoDir (original), info.ReadOnly = true
// Mount as :ro in container
```

### Git Worktree (Write agents)
```go
info, _ := PrepareWorkspace(ctx, repoDir, agentID, WorkspaceWorktree)
// info.Path = repoDir-worktree-{agentID[:8]}
// info.Branch = "agent/{agentID}"
// Mount as :rw in container
```

### Full Swarm Lifecycle

```
1. Agent spawned     → PrepareWorkspace(repoDir, agentID, WorkspaceWorktree)
2. Container created → mount worktree path read-write
3. Agent works       → commits to agent/{id} branch
4. Agent completes   → MergeWorktree(info, "main")  // or CreatePR via API
5. Cleanup           → CleanupWorkspace(info)
6. Human review      → web UI at http://localhost:58080/git/
```

### Merge Strategy

`MergeWorktree()` checks if the agent branch has commits ahead of base. If yes, performs `git merge --no-ff`. If no changes, silently returns (no-op).

For PR-based workflow, the `Client` provides:
- `CreateBranch()` — create agent branch from base ref
- `CreatePR()` — open PR from agent branch to main
- `MergePR()` — merge (supports merge, rebase, squash methods)
- `ListPRs()` — list open/closed PRs

## Gitea REST API Client

Typed Go client wrapping Gitea's REST API (`internal/gitserver/api.go`):

```go
c := NewClient("http://localhost:3000", "admin-token")

repo, _ := c.CreateRepo(ctx, "project", "Agent workspace")
branch, _ := c.CreateBranch(ctx, "admin", "project", "agent/001", "main")
pr, _ := c.CreatePR(ctx, "admin", "project", "Agent work", "agent/001", "main")
_ = c.MergePR(ctx, "admin", "project", pr.Number, "merge")

repos, _ := c.ListRepos(ctx)
branches, _ := c.ListBranches(ctx, "admin", "project")
prs, _ := c.ListPRs(ctx, "admin", "project", "open")
```

## Configuration

```json
{
  "gitServer": {
    "enabled": true,
    "dataDir": "/custom/data/gitea",
    "appName": "ycode Git",
    "httpOnly": true,
    "token": ""
  }
}
```

Three-tier merge: user > project > local. Token auto-generated on first start if empty.

## Testing

- **Unit tests**: API client with mock HTTP server (all CRUD + error handling + no-token mode), component lifecycle, config merge, workspace mode constants
- **Integration tests** (`//go:build integration`): Worktree create/commit/merge/cleanup, multiple concurrent worktrees, merge with no changes (no-op)
- **Makefile**: `make test-gitserver`

## StackManager Registration

In `cmd/ycode/serve.go` `buildStackManager()`, the `gitServerCfg` parameter is accepted but registration is prepared for when the component is wired up (similar to how Ollama is conditionally registered based on config):

```go
// Future: register GitServerComponent when gitServerCfg.Enabled
```

Proxy path: `/git/` — all requests reverse-proxied to Gitea subprocess.

## Relationship to Podman

With both Podman and Gitea available, the workspace flow for agent swarms:

- **Read-only agents**: Workspace mounted as `:ro` in container
- **Write agents**: Git worktree on unique branch, mounted `:rw` in container
- **Coordination**: Gitea is the shared truth — agents push to branches independently, no conflicts
- **Merge**: Through PRs (automated or human-reviewed) via Gitea API
- **Traceability**: OTEL traces link container operations to git operations

## Files

| File | Purpose |
|------|---------|
| `external/gitea/embed/embed.go` | Settings wrapper for in-process init |
| `internal/gitserver/server.go` | Gitea subprocess lifecycle + auto-config |
| `internal/gitserver/component.go` | `observability.Component` + OTEL |
| `internal/gitserver/workspace.go` | Worktree-based agent workspace isolation |
| `internal/gitserver/api.go` | Typed Gitea REST API client |
| `internal/gitserver/api_test.go` | Mock HTTP server tests |
| `internal/gitserver/workspace_test.go` | Worktree unit + integration tests |
| `internal/gitserver/component_test.go` | Component lifecycle tests |
| `internal/gitserver/integration_test.go` | Multi-worktree integration tests |
| `internal/runtime/config/config.go` | `GitServerConfig` struct |
