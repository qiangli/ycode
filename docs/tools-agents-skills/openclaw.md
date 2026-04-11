# OpenClaw - Tools, Agents, Skills & Security Analysis

**Project:** OpenClaw (multi-channel AI assistant gateway)
**Language:** TypeScript (ESM)
**Repository:** openclaw/openclaw

---

## Tools (Function Calling) - 21+ core tools

### Execution & System
| Tool | Description |
|------|-------------|
| `exec` / `process` | Shell commands on sandbox/gateway/node hosts; PTY, background, approvals |
| `code_execution` | Sandboxed remote Python analysis |

### File I/O & Patching
| Tool | Description |
|------|-------------|
| `read` | Read workspace files |
| `write` | Write workspace files |
| `edit` | Modify workspace files |
| `apply_patch` | Multi-hunk file patches |

### Web & Search
| Tool | Description |
|------|-------------|
| `web_search` | Search via 11 providers (Brave, DuckDuckGo, Perplexity, Tavily, Exa, Grok, Gemini, etc.) |
| `x_search` | Search X/Twitter posts |
| `web_fetch` | Fetch & parse web pages with SSRF protection |

### Media & Image
| Tool | Description |
|------|-------------|
| `image` / `image_tool` | Image analysis via vision models |
| `image_generate` | Image generation (OpenAI, Google, FAL) |
| `music_generate` | Music track generation |
| `video_generate` | Video generation |
| `tts` | Text-to-speech conversion |
| `pdf` | PDF document analysis |

### Browser & UI
| Tool | Description |
|------|-------------|
| `browser` | Chromium control: navigate, click, screenshot, scroll |
| `canvas` | Drive OpenClaw Canvas (A2UI push/reset/eval/snapshot) |

### Device & Hardware
| Tool | Description |
|------|-------------|
| `nodes` | Discover & target paired devices (macOS, iOS, Android) |
| `camera` | Camera snap/clip via nodes |
| `screen.record` | Screen recording via nodes |
| `location.get` | Device location via nodes |
| `system.run` | Elevated commands on macOS nodes |
| `system.notify` | Post notifications on nodes |

### Session & Orchestration
| Tool | Description |
|------|-------------|
| `sessions_spawn` | Spawn isolated sub-agent runs |
| `sessions_list` | List active sessions |
| `sessions_history` | Safety-filtered session history |
| `sessions_yield` | Pause and yield execution |
| `sessions_send` | Cross-session messaging |
| `subagents` | Inspect/control sub-agent runs |
| `agents_list` | List available agents |
| `session_status` | Status readback & model override |

### Messaging & Automation
| Tool | Description |
|------|-------------|
| `message` | Send to all connected channels |
| `cron` | Manage scheduled jobs and webhooks |
| `gateway` | Owner-only runtime control (config, update, restart) |
| `update-plan` | Plan gateway/OpenClaw updates |

---

## Agents / Subagents

### Multi-Agent Architecture
| Component | Description |
|-----------|-------------|
| **Agent isolation** | Session key: `agent:<agentId>:main` vs `agent:<agentId>:subagent:<uuid>` |
| **Per-agent config** | Workspace, skills, tool policies, models, timeouts, sandbox |
| **Spawn depth** | Configurable: depth 0 (main) → depth 1 (sub) → depth 2 (sub-sub) |
| **Concurrency** | `maxChildrenPerAgent` (default 5), `maxConcurrent` (default 8) |
| **Auto-archive** | After configurable inactivity (default 60 min) |

### ACP (Agent Client Protocol) Sessions
| Feature | Description |
|---------|-------------|
| **Supported harnesses** | Codex, Claude Code, Cursor, Gemini CLI, OpenCode |
| **Binding modes** | Current-conversation (`--bind here`) or thread (`--thread auto`) |
| **Commands** | `/acp spawn`, `/acp status`, `/acp model`, `/acp cancel`, `/acp close` |

---

## Skills (75 bundled)

### Categories
| Category | Skills |
|----------|--------|
| **System** | healthcheck, node-connect, coding-agent, taskflow, oracle |
| **Communication** | discord, slack, imsg, bluebubbles, voice-call |
| **Notes** | apple-notes, bear-notes, notion, obsidian, apple-reminders, things-mac |
| **Development** | github, gh-issues, skill-creator, clawhub |
| **Audio/Media** | openai-whisper, spotify-player, songsee, canvas, peekaboo, video-frames |
| **IoT/Hardware** | eightctl, openhue, blucli |
| **Utility** | nano-pdf, himalaya (email), gifgrep, weather, goplaces |
| **AI/Models** | gemini, model-usage, sag |

### Skill Gating System
- **Environment:** `requires.env` checks
- **Binaries:** `requires.bins` / `requires.anyBins` PATH validation
- **Config:** `requires.config` path checks
- **OS:** Platform filtering (darwin/linux/win32)
- **Install specs:** brew/node/go/uv/download
- **Load order:** Extra dirs → Bundled → Managed → Personal → Project → Workspace

---

## Security & Guardrails

### 5-Tier Trust Model (MITRE ATLAS framework)
| Tier | Description |
|------|-------------|
| **Channel Access** | DM pairing codes, AllowFrom/AllowList, Token/Tailscale auth |
| **Session Isolation** | Per-agent tool policies, transcript logging |
| **Tool Execution** | Docker sandbox, exec approvals, SSRF protection |
| **External Content** | XML wrapping, security notice injection |
| **Supply Chain** | ClawHub skill moderation, semver, VirusTotal scanning |

### Exec Tool Security
| Feature | Description |
|---------|-------------|
| **Host routing** | auto/sandbox/gateway/node |
| **Security modes** | deny, allowlist, full |
| **Approval modes** | off, on-miss, always |
| **File binding** | Approvals bound to concrete file operands |
| **Elevated access** | Per-session toggle, requires explicit enablement |
| **Command validation** | Resolved binary paths, shell chaining analysis |

### Tool Profiles
| Profile | Description |
|---------|-------------|
| `full` | No restrictions (default) |
| `coding` | fs, runtime, web, sessions, memory, cron, media |
| `messaging` | Messaging only + sessions |
| `minimal` | session_status only |

### Additional Security
| Mechanism | Description |
|-----------|-------------|
| **SSRF controls** | DNS pinning + IP blocking for internal ranges |
| **Image sanitization** | Size caps, MIME validation, dimension limits |
| **Content filtering** | Strip thinking tags, redact control tokens, truncation |
| **Sandbox inheritance** | Sub-agents must match parent sandbox state |
| **Secret management** | API keys injected via env, not in prompts/logs |
| **Dangerous code scanner** | Blocks critical findings in skill installers |

---

## Notable Patterns

- **25+ channel integrations:** WhatsApp, Telegram, Slack, Discord, Signal, iMessage, IRC, Matrix, etc.
- **Plugin architecture:** 99+ bundled plugins with manifest-driven loading
- **ACP runtime:** Interop with external agent harnesses (Codex, Claude Code, Gemini CLI)
- **Tool streaming:** Block streaming for progressive tool results
- **Config SHA-256 hashing:** Baseline drift detection for config changes
- **Thread binding:** Discord thread ↔ agent session binding

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Multi-channel messaging (25+ platforms) | Not applicable | N/A - ycode is CLI-focused |
| ACP (Agent Client Protocol) interop | Not implemented | **Medium** - future agent interop |
| Image/video/music generation tools | Not implemented | Low - not core to coding |
| Browser automation tool | Not implemented | **High** - web research capability |
| Device/node remote execution | Not applicable | N/A - desktop scope |
| 75 bundled skills | 6 skills | **High** - expand skill library |
| Skill gating (bins/env/config/OS) | Not implemented | **Medium** - conditional skills |
| Tool profiles (full/coding/messaging/minimal) | Not implemented | **Medium** - presets |
| SSRF protection in web fetch | Not implemented | **Medium** - security |
| Exec file binding (approval tied to operand) | Not implemented | Low |
| Content filtering (strip thinking tags, etc.) | Not implemented | **Medium** - safety |
| Config drift detection (SHA-256) | Not implemented | Low |
| ClawHub skill marketplace | Not applicable | N/A - different ecosystem |
