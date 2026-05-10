# docs/backlog/

Canonical task list for ycode. **One `.md` per task, slug = filename
stem.** See [`docs/backlog.md`](../backlog.md) for the source-of-truth
contract, the Boss → Foreman → Worker chain, the Boss control
protocol, and the reconciler semantics.

This `README.md` is not an issue — the reconciler skips it.

The `PAUSE` sentinel (also skipped) is a kill-switch: presence pauses
the Foreman between iterations.

## Adding a new task

```bash
ycode backlog new "Implement external_cnl executor" --priority p1
# scaffolds docs/backlog/implement-external-cnl-executor.md

ycode backlog list                  # show all
ycode backlog list --priority p1    # only top tier
ycode backlog show <slug>           # render one
```

The reconciler (running inside `ycode serve`) syncs new entries to
Gitea on its next 60s poll; force a sync with `ycode backlog reconcile`.
