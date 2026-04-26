# OpenClaw - Skills Analysis

**Project:** OpenClaw (multi-channel AI assistant gateway)
**Language:** TypeScript (ESM)
**Repository:** openclaw/openclaw

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

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Supply Chain** | ClawHub skill moderation, semver, VirusTotal scanning |
| **Dangerous code scanner** | Blocks critical findings in skill installers |
| **Skill gating** | Skills only load when prerequisites are met |

---

## Notable Patterns

- **75 bundled skills:** Largest skill library among surveyed projects
- **Skill gating:** Prerequisites checked before skill activation
- **Plugin architecture:** 99+ bundled plugins with manifest-driven loading
- **Config SHA-256 hashing:** Baseline drift detection for config changes

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| 75 bundled skills | 6 skills | **High** - expand skill library |
| Skill gating (bins/env/config/OS) | Not implemented | **Medium** - conditional skills |
| Skill marketplace (ClawHub) | Not applicable | N/A - different ecosystem |
| Supply chain security for skills | Not implemented | **Medium** - safety |
