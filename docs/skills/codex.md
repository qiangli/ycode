# Codex CLI - Skills Analysis

**Project:** OpenAI Codex CLI Agent
**Language:** Rust (codex-rs core), TypeScript (codex-cli wrapper)
**Repository:** openai/codex

---

## Skills

| Component | Description |
|-----------|-------------|
| **core-skills crate** | Skill loading, management, invocation, rendering |
| **Guardian review** | Dedicated LLM agent reviewing approval requests |
| **Tool suggestion** | ML-powered tool recommendation |
| **Memory consolidation** | Multi-stage context compression |
| **Personality templates** | Configurable agent personas |

---

## Security & Guardrails (Skill-Related)

| Mechanism | Description |
|-----------|-------------|
| **Proposed amendments** | Auto-suggest policy updates after skill-based approval |

---

## Notable Patterns

- **core-skills crate:** Centralized skill management as a Rust crate
- **Personality templates:** Configurable agent personas via skills
- **ML-based tool suggestion:** Skills can recommend tools

---

## Gap Analysis vs ycode

| Feature | ycode Status | Priority |
|---------|-------------|----------|
| Personality templates | Not implemented | Low - nice-to-have |
| ML-based tool suggestion | Not implemented | Low |
| Memory consolidation skill | Implemented (session compaction) | Done |
