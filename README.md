# ycode

A pure Go CLI agent harness for autonomous software development. Single static binary, permissive-license dependencies only (MIT, Apache-2.0, BSD).

## Install

### Homebrew (macOS / Linux, once tap is bootstrapped)

```bash
brew tap qiangli/ycode
brew install ycode
```

(Tap bootstrap is a one-time admin step — see [docs/release.md](./docs/release.md#homebrew-tap).)

### Prebuilt binary

Latest release: <https://github.com/qiangli/ycode/releases/latest>

```bash
# macOS (Apple Silicon)
curl -sSL https://github.com/qiangli/ycode/releases/latest/download/ycode-darwin-arm64.tar.gz | tar -xz
sudo mv ycode /usr/local/bin/

# Linux (x86_64)
curl -sSL https://github.com/qiangli/ycode/releases/latest/download/ycode-linux-amd64.tar.gz | tar -xz
sudo mv ycode /usr/local/bin/
```

Each release includes a `SHA256SUMS` file alongside the archives. Verify before installing:

```bash
curl -sSLO https://github.com/qiangli/ycode/releases/latest/download/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Other platforms (darwin-amd64, linux-arm64, windows) are not yet packaged — see `.github/workflows/release.yml` for the matrix and the open issues blocking each.

### From source

```bash
git clone https://github.com/qiangli/ycode.git
cd ycode
make init   # initialize submodules (first time only)
make build  # full quality gate; binary lands at bin/ycode
```

## Quick start

```bash
export ANTHROPIC_API_KEY="sk-ant-..."  # or OPENAI_API_KEY
ycode doctor                            # health check
ycode                                   # interactive REPL
```

## Features

The feature list lives in `internal/features/registry.yaml` and is exposed via the CLI — no need to keep this README in sync.

```
ycode features list      # all features (stable, experimental, wip) with file paths
ycode features readme    # markdown bullet list of stable features
ycode features verify    # validate the registry (paths exist, no malformed entries)
```

## Prerequisites

- Go 1.26+
- One of:
  - `ANTHROPIC_API_KEY` for Anthropic models
  - `OPENAI_API_KEY` (+ optional `OPENAI_BASE_URL`) for OpenAI-compatible models

## Documentation

- [docs/strategy.md](./docs/strategy.md) -- **strategic roadmap, wedge positioning, feature-tier policy, operating principles** (read first for any planning or feature discussion)
- [docs/roadmap.md](./docs/roadmap.md) -- tactical feature-gap inventory (P0/P1/P2)
- [docs/leaderboards.md](./docs/leaderboards.md) -- benchmark targets and submission process
- [docs/release.md](./docs/release.md) -- release process, Homebrew tap bootstrap, troubleshooting
- [AGENTS.md](./AGENTS.md) -- instructions for AI coding assistants (YCODE.md symlinks here)
- [docs/usage.md](./docs/usage.md) -- CLI modes, configuration, tools, and workflows
- [docs/instructions.md](./docs/instructions.md) -- conventions, skill system, build/test/commit rules
- [docs/architecture.md](./docs/architecture.md) -- full architecture, design decisions, component details

## Prior art & acknowledgments

ycode is a ground-up rewrite of [Claw Code](https://github.com/ultraworkers/claw-code) in Go, drawing inspiration from several open-source projects included as submodules under `priorart/`:

| Project | License | Description |
|---------|---------|-------------|
| [Aider](https://github.com/aider-ai/aider) | Apache-2.0 | AI pair programming in the terminal |
| [Claw Code](https://github.com/ultraworkers/claw-code) | -- | Rust-based CLI agent harness (direct ancestor) |
| [Cline](https://github.com/cline/cline) | Apache-2.0 | Autonomous coding agent for IDEs |
| [Codex](https://github.com/openai/codex) | Apache-2.0 | OpenAI's CLI coding agent |
| [Continue](https://github.com/continuedev/continue) | Apache-2.0 | Open-source AI code assistant |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | Apache-2.0 | Google's CLI for Gemini models |
| [OpenClaw](https://github.com/openclaw/openclaw) | MIT | Open-source CLI agent harness |
| [OpenCode](https://github.com/anomalyco/opencode) | MIT | Terminal-based AI coding assistant |
| [OpenHands](https://github.com/OpenHands/OpenHands) | MIT | Platform for AI software agents |

## License

MIT
