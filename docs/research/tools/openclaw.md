# OpenClaw - Tools & Security Analysis

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

- **Tool streaming:** Block streaming for progressive tool results

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Browser automation tool | Not implemented | **High** - web research capability |
| Tool profiles (full/coding/messaging/minimal) | Not implemented | **Medium** - presets |
| SSRF protection in web fetch | Not implemented | **Medium** - security |
| Content filtering (strip thinking tags, etc.) | Not implemented | **Medium** - safety |
| Image/video/music generation tools | Not implemented | Low - not core to coding |
| Device/node remote execution | Not applicable | N/A - desktop scope |
