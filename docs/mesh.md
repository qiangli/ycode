# Autonomous Agent Mesh

The agent mesh is a set of background agents that observe, diagnose, learn, research, fix, and train — making ycode a self-improving system.

## Architecture

```
Bus Events (tool.result, turn.error, session.update)
    │
    ▼
┌──────────┐  diagnostic.report  ┌────────┐  fix.complete   ┌─────────┐
│ Diagnoser├─────────────────────►│ Fixer  ├────────────────►│ Learner │
└──────────┘                     └───┬────┘                 └─────────┘
    │                                │ unknown error             ▲
    │ diagnostic.report              ▼                           │
    └───────────────────────►┌────────────┐  research.done ──────┘
                             │ Researcher │
                             └────────────┘
                                                  ┌─────────┐
                               CronRegistry ─────►│ Trainer │
                                                  └─────────┘
```

## Agents

### Diagnoser (`mesh/diagnoser.go`)
The "eyes" — observes and reports, never modifies anything.
- Subscribes to `tool.result` and `turn.error` bus events
- Periodically checks `QualityMonitor.DegradedTools()` for reliability drops
- Tracks context overflow frequency (compaction events)
- Emits `DiagnosticReport` with severity (info/warn/critical), category, and evidence
- Ring buffer stores last 100 reports for querying

### Fixer (`mesh/fixer.go`)
Reacts to diagnostic reports and attempts automated remediation.
- Triggered by `diagnostic.report` events with warn/critical severity
- Checks `SafetyGuard` before attempting any fix (budget + per-report limits)
- Delegates to injected `FixFunc` (typically wired to `selfheal.AIHealer`)
- Publishes `fix.start`/`fix.complete`/`fix.failed` events
- Server mode only (disabled in CLI mode)

### Learner (`mesh/learner.go`)
Post-event analysis and memory consolidation.
- Listens for `fix.complete`, `fix.failed`, `diagnostic.report` events
- Records successful fix patterns as procedural memory
- Runs periodic consolidation via the Dreamer
- Available in both CLI and server modes

### Researcher (`mesh/researcher.go`)
Background web research for unknown errors and new APIs.
- Triggered by `diagnostic.report` and `fix.failed` events
- Rate-limited: max N searches per 10-minute window (configurable)
- Truncates queries to 200 chars for privacy/efficiency
- Saves results as reference memory via injected `SaveFunc`
- Server mode only

### Trainer (`mesh/trainer.go`)
Scheduled model training pipeline orchestration.
- Runs on configurable interval (default: 24 hours)
- Delegates to injected `TrainFunc` (wired to `training/loop.SelfImproveLoop`)
- Supports on-demand training via `RunNow()`
- Publishes `train.start`/`train.complete` events
- Server mode only, requires explicit opt-in

## Safety

All autonomous actions are governed by `SafetyGuard` (`mesh/safety.go`):
- **Fix budget**: max 5 fixes per hour (configurable)
- **Per-report limit**: max 2 attempts per diagnostic report
- **Test gate**: every code fix must pass `go test -short` before commit
- **Protected paths**: inherits from `selfheal.Config.ProtectedPaths`
- **Escalation**: after failed attempts, logs and stops — never retries silently

## Configuration

```json
{
  "mesh_enabled": true,
  "mesh_mode": "server",
  "diag_interval": "2m",
  "max_fix_attempts": 5,
  "research_limit_per_10m": 3,
  "training_enabled": false,
  "training_cron": "0 2 * * *"
}
```

## Modes

| Mode | Agents | Use Case |
|------|--------|----------|
| `cli` | Diagnoser + Learner | Lightweight background monitoring during interactive sessions |
| `server` | All 5 agents | Full autonomous operation in `ycode serve` mode |

## CLI

```bash
ycode mesh status    # show agent states and mode
```

## Observability

All mesh agents emit:
- **OTEL spans**: `mesh.diagnoser.tick`, `mesh.fixer.attempt`, etc. (via `TracedAgent` wrapper)
- **Structured logs**: `mesh.diagnoser.report`, `mesh.fixer.success`, `mesh.learner.fix_pattern`, etc.
- **Bus events**: `diagnostic.report`, `fix.*`, `learn.complete`, `research.done`, `train.*`

View in the embedded Perses dashboards or Jaeger trace UI.

## Self-Improving Flywheel

```
observe errors → diagnose → fix code → learn pattern → train model → serve better → fewer errors
```
