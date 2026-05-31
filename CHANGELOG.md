# Changelog

All notable changes to ycode are tracked here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

For releases prior to v0.2.0 see the git tag history (`git log v0.1.1`).

## [Unreleased]

## [0.2.0] — 2026-05-31

### Added
- **Host-aware podman machine sizing.** `DefaultMachineConfig` now picks
  CPUs / Memory / Disk as a clamped fraction of host `NumCPU` / total
  memory / free disk so first-run auto-provision succeeds on modest
  hosts. Formulas: CPUs = NumCPU/2 ∈ [2,8], Mem = totalMB/4 ∈ [2048,
  16384], Disk = freeGB/4 ∈ [10,50]. Probe failure degrades to the
  historical `(2, 4096, 50)` defaults. CLI flags `--cpus / --memory /
  --disk-size` continue to override.
- **Host-resource preflight before VM provisioning.** Refuses to spawn a
  VM that would OOM the host or overflow the disk; surfaces a typed
  `PreflightError` with the shortfall and a remediation hint instead of
  silently allocating a 50 GB sparse image on a 30 GB partition.
- **Offline auto-cleanup retry on preflight refusal.** Detects orphaned
  `vfkit` / `gvproxy` processes and stale sockets from prior crashes,
  removes them, and re-runs the preflight. Disable with `--no-auto-
  cleanup`.
- **`podman run`: wait for container + propagate exit code.** `ycode
  podman run` now mirrors upstream's blocking behaviour and exits with
  the container's status code rather than returning immediately on
  start.
- **Docker-style named volumes in `podman run -v`.** Accepts the
  `name:/path` form the same way Docker / upstream podman do.
- **Platform-default machine volumes at init time.** Mounts `/Users`,
  `/private`, `/var/folders` on darwin so host bind-mounts work without
  extra `-v` flags.
- **Cpuset cgroup delegation on machine start.** Fedora CoreOS's
  systemd-user instance doesn't delegate cpuset by default; we now
  enable it one-shot per machine boot so k3s / kubelet / runc-cpu-
  pinning workloads can start inside containers.

### Changed
- **`make install`: replace symlink shims.** The shim install loop now
  `rm -f`s `~/bin/{ollama,podman,docker}` before `cp` so an existing
  Homebrew symlink (e.g. `~/bin/podman` → `/opt/homebrew/bin/podman`)
  is replaced rather than followed — previously the `cp` permission-
  denied while trying to overwrite the homebrew target.
- **Deploy scripts: retry `/healthz` instead of one-shot sleep.** Both
  `scripts/deploy-local.sh` and `scripts/deploy-remote.sh` now poll
  `/healthz` every 0.5s for up to 15s after starting `ycode serve`. The
  prior fixed `sleep 2|3` routinely under-shot serve's cold start and
  reported FAILED even though the process came up healthy moments
  later.
- **Sibling-dep resolution via `.sibling-pins`.** `go.mod` replace
  directives for `nadir`, `sh`, etc. now resolve via the
  `scripts/bootstrap-siblings.sh` helper from pinned SHAs when this
  repo is checked out standalone (i.e. not inside the `dhnt/`
  umbrella).

### Fixed
- **`make install`: unlink `~/bin/ycode` before `cp`.** Overwriting a
  signed Mach-O in place on macOS left the kernel's per-vnode
  `cs_blob` cache pointing at the previous signature; the next `exec`
  then failed validation and the process was SIGKILLed before
  `main()`.
- **Skill engine: `Registry.RecordOutcome` / `TempDir` cleanup race.**
- **Browser/live: `setupBrowserBackend` is now a process-wide
  singleton**, log capabilities-probe caller, skip capabilities probes
  from the extension audit log, record every dispatched method in the
  audit log, surface extension version in the popup title.

[Unreleased]: https://github.com/qiangli/ycode/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/qiangli/ycode/compare/v0.1.1...v0.2.0
