# Gap Analysis: Codex — Built-in Tool System & Tool Use

**Tool:** OpenAI Codex CLI (Rust, Apache-2.0 license)
**Domain:** Built-in Tool System & Tool Use
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | Codex |
|------|-------|-------|
| Tool count | 70+ tools | ~10 core tools + MCP |
| Execution tiers | 3-tier (native Go → host exec → container) | Exec server (JSON-RPC) + direct exec |
| Browser automation | Container-based browser-use | No browser automation |
| Web search | 5-provider chain | No built-in web search |
| Shell execution | In-process mvdan/sh + security middleware | PTY subprocess with separate exec-server |
| Container tools | Generic containertool framework, auto-build | No container tools |
| File operations | Glob, grep, read, write, edit with fuzzy matching | File system abstraction + apply-patch |
| Test runner | Multi-language (Go, Python, JS, Rust) | No built-in test runner |

## Gaps Identified

| ID | Feature | Codex Implementation | ycode Status | Priority | Effort |
|----|---------|---------------------|--------------|----------|--------|
| T1 | Extended SSRF IP range blocking | Blocks CGNAT (100.64/10), multicast (224/4), broadcast, TEST-NET (192.0.2/24, 198.51.100/24, 203.0.113/24), benchmarking (198.18/15), reserved (0/8) | ycode blocks loopback, private, link-local only — missing 6+ additional ranges | High | Low |
| T2 | Argument-level command safety validation | base64 -o is unsafe; find -exec/-delete unsafe; rg --pre unsafe; git global options (-c, -C) block code execution | ycode classifies base command but doesn't validate args for "safe" commands | Medium | Medium |
| T3 | Command canonicalization for approval caching | Normalizes shell wrapper paths; handles complex scripts; stable approval keys | ycode caches by tool name only, not by normalized command | Low | Medium |
| T4 | Network proxy with policy filtering | In-process HTTP MITM + SOCKS5 with allow/deny domain rules | ycode has SSRF validation on fetch but no proxy | Low | High |
| T5 | Process hardening (pre-main) | ptrace denial, core dump disable, env cleaning (LD_*, DYLD_*) | ycode has no process hardening | Low | Medium |
| T6 | Streaming patch parser | Process large patches without loading entire content into memory | ycode's apply_patch loads full content | Low | Medium |

---

## Implementation Plan

### Phase 1: Extended SSRF Protection (T1)

**Files to modify:**
- `internal/runtime/net/ssrf.go` — Add missing IP ranges

**Design:**
- Add CGNAT (100.64.0.0/10), multicast (224.0.0.0/4), broadcast (255.255.255.255/32)
- Add TEST-NET blocks (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24)
- Add benchmarking (198.18.0.0/15), reserved (0.0.0.0/8)

### Phase 2: Argument-Level Command Safety (T2)

**Files to modify:**
- `internal/runtime/bash/safety.go` — Add argument validation for "safe" commands

**Design:**
- `find` with `-exec`, `-execdir`, `-delete` → classify as Write
- `base64` with `-o`/`--output` → classify as Write
- `git` with global `-c`, `-C`, `--git-dir` → classify as Write (prevents config hijack)

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| T3 | Command canonicalization | ycode's approval cache works at tool level; command-level would need policy engine changes |
| T4 | Network proxy | High effort, ycode's SSRF validation is adequate for CLI use |
| T5 | Process hardening | Go runtime makes ptrace/core-dump concerns less relevant than C/Rust |
| T6 | Streaming patch parser | ycode patches are typically small; no memory pressure observed |

---

## Verification

- Unit tests for new SSRF ranges
- Unit tests for argument-level safety validation
- `make build` must pass
