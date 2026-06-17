# Weave Failover Follow-Up

Implemented in this MVP:

- Queue-scoped orchestrator lease with TTL heartbeat in `orchestrator-lease.json`.
- `ycode weave autopilot --orchestrator-fleet ...` with fleet rotation.
- Output signature detection for `529`, `Overloaded`, and rate-limit text.
- `--standby` takeover when the lease heartbeat expires.
- Takeover/failover/failback event logging in `autopilot.log`.

Remaining hardening:

- Preferred-primary failback is currently only attempted between orchestrator process runs. A stronger version should make the orchestrator CLI cooperate with an explicit top-of-loop checkpoint so a backup can release the lease after confirming no merge/gate/fetch is in flight.
- Agent CLI argument profiles may need per-tool prompt delivery. The MVP sends the orchestration brief and queue context on stdin to the selected command.
- API-overload detection should be extended with provider-specific structured error parsing where available.
