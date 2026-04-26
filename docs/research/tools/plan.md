# Implementation Plan: Tools & Security Gaps

> Based on analysis of 9 prior-art projects. Prioritized by impact and feasibility.
> Generated 2026-04-11.

---

## Phase 1: Security Hardening (P0)

The most critical gaps are in security. These should be addressed first since they protect against unsafe autonomous execution.

### 1.1 Bash Command Safety Analysis
**Effort:** Medium | **Reference:** OpenCode (tree-sitter), Claw Code (intent classification)
**Files:** `internal/tools/bash.go`, new `internal/runtime/bash/`

- [ ] Implement bash command parser using Go's `mvdan.cc/sh` package (shell AST parser)
- [ ] Classify command intent: ReadOnly, Write, Destructive, Network, ProcessManagement, PackageManagement, SystemAdmin
- [ ] Block/warn on destructive commands (`rm -rf /`, `dd`, `mkfs`, etc.)
- [ ] Detect and block shell redirects (`>`, `>>`, `<`, `>&`) in restricted modes
- [ ] Detect pipe chains and validate each segment
- [ ] Validate commands against permission mode (ReadOnly blocks writes)
- [ ] Add allowlist/blocklist for specific commands

### 1.2 Platform Sandboxing
**Effort:** High | **Reference:** Codex (Seatbelt/Landlock/bwrap), Gemini CLI
**Files:** new `internal/runtime/sandbox/`

- [ ] Define `Sandbox` interface with `Wrap(cmd) -> cmd` pattern
- [ ] macOS: Implement Seatbelt (sandbox-exec) profiles
  - Read-only profile: block all writes except /tmp
  - Workspace-write profile: allow writes only under workspace root
  - Keep .git directories read-only
- [ ] Linux: Implement Landlock-based sandboxing (Go's `landlock` package)
  - Filesystem access restrictions by path
  - Network access restrictions
- [ ] Linux fallback: bubblewrap (bwrap) integration
- [ ] Container detection: skip sandboxing when already in Docker/K8s
- [ ] Integration: wrap bash tool execution with sandbox

### 1.3 Sensitive File Protection
**Effort:** Low | **Reference:** OpenCode, Cline
**Files:** `internal/tools/fileops.go`, new `internal/runtime/ignore/`

- [ ] Add `.ycodeignore` support (gitignore-pattern matching)
- [ ] Block reading `.env` files without explicit permission (ask)
- [ ] Allow `.env.example` without prompting
- [ ] Detect binary files (NUL byte check) and refuse text operations
- [ ] Validate file size before reading (reject files > 50MB)

### 1.4 SSRF Protection for WebFetch
**Effort:** Low | **Reference:** OpenClaw
**Files:** `internal/tools/web.go`

- [ ] Block requests to private/internal IP ranges (127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, ::1)
- [ ] DNS pinning: resolve hostname before connecting, validate resolved IP
- [ ] Block requests to metadata endpoints (169.254.169.254)

---

## Phase 2: New Tools (P1)

### 2.1 apply_patch Tool (Multi-File Atomic)
**Effort:** Medium | **Reference:** Codex, Cline, OpenCode
**Files:** new `internal/tools/patch.go`

- [ ] Parse unified diff format
- [ ] Support multi-file patches in single invocation
- [ ] Atomic application: all-or-nothing (rollback on failure)
- [ ] Validate patch applies cleanly before writing
- [ ] Register as deferred tool

### 2.2 view_image Tool
**Effort:** Low | **Reference:** Codex
**Files:** new `internal/tools/image.go`

- [ ] Accept file path, return image content for multimodal models
- [ ] Support common formats: PNG, JPG, GIF, SVG, WebP
- [ ] Size limit enforcement (10MB max)
- [ ] Register as deferred tool

### 2.3 view_diff Tool (Git Diff)
**Effort:** Low | **Reference:** Continue, Aider
**Files:** new `internal/tools/git_diff.go`

- [ ] Expose `git diff` as LLM-callable tool
- [ ] Parameters: staged (bool), file path filter, commit range
- [ ] Return formatted diff output
- [ ] Register as deferred tool

### 2.4 Browser Automation Tool
**Effort:** High | **Reference:** Cline (Puppeteer), Gemini (Chrome), OpenHands
**Files:** new `internal/tools/browser/`

- [ ] Integrate headless Chrome via CDP (Chrome DevTools Protocol)
- [ ] Actions: navigate, click, type, scroll, screenshot, get_text
- [ ] Accessibility tree extraction for LLM consumption
- [ ] Domain allowlist for security
- [ ] Session management (launch, close)
- [ ] Register as deferred tool (requires Chrome available)
- [ ] Note: Could also be implemented as MCP server for modularity

---

## Phase 3: Security Enhancements (P1-P2)

### 3.1 Approval Caching
**Effort:** Low | **Reference:** Codex, Cline
**Files:** `internal/runtime/permission/`

- [ ] Cache user approval decisions within session
- [ ] Key by: tool name + normalized input pattern
- [ ] Options: "once" (single use), "always" (session), "always and save" (persist to policy)
- [ ] Clear cache on session end

### 3.2 Guardian LLM Review (Optional)
**Effort:** High | **Reference:** Codex
**Files:** new `internal/runtime/guardian/`

- [ ] Define `Guardian` interface
- [ ] Send compact transcript of pending tool call to reviewer LLM
- [ ] Risk assessment: LOW/MEDIUM/HIGH/CRITICAL
- [ ] Fail-closed design: deny on timeout (configurable, default 30s)
- [ ] Policy document defining risk categories
- [ ] Optional: configurable per permission mode
- [ ] Note: This is an advanced feature, implement after basics are solid

### 3.3 Doom Loop Detection
**Effort:** Low | **Reference:** OpenCode, OpenHands
**Files:** `internal/runtime/conversation/`

- [ ] Track consecutive similar tool calls (same tool + similar args)
- [ ] Detect repeated failures (same error message N times)
- [ ] After threshold (e.g., 3 consecutive similar failures), interrupt and ask user
- [ ] Track "almost stuck" state for recovery

---

## Phase 4: Nice-to-Have (P2-P3)

### 4.1 REPL Tool
**Reference:** Codex (JS), OpenHands (IPython)
- [ ] Python REPL with persistent kernel
- [ ] Useful for data exploration and testing

### 4.2 Repository Mapping
**Reference:** Aider (tree-sitter)
- [ ] AST-based code summarization for repo overview
- [ ] Token-budget-aware trimming

### 4.3 Content Filtering
**Reference:** OpenClaw
- [ ] Strip thinking tags from output
- [ ] Redact leaked control tokens
- [ ] Filter large output with intelligent truncation

---

## Implementation Order (Recommended)

```
Phase 1 (Security) ──→ Phase 2 (New Tools) ──→ Phase 3 (More Security) ──→ Phase 4 (Nice-to-have)
```

### Sprint 1: Security Foundation
1. Bash command safety analysis (1.1)
2. Sensitive file protection (1.3)
3. SSRF protection (1.4)

### Sprint 2: New Tools
4. apply_patch tool (2.1)
5. view_image tool (2.2)
6. view_diff tool (2.3)

### Sprint 3: Sandbox + Approval
7. Platform sandboxing (1.2)
8. Approval caching (3.1)
9. Doom loop detection (3.3)

### Sprint 4: Advanced Features
10. Browser automation (2.4)
11. Guardian LLM review (3.2)

---

## Dependencies

| Item | Depends On |
|------|------------|
| Platform sandboxing | Bash safety analysis (for integration) |
| Guardian LLM review | Approval caching (for UX) |
| Browser automation | None (can be MCP server) |

## Key Go Packages

| Need | Package |
|------|---------|
| Shell AST parsing | `mvdan.cc/sh/v3/syntax` |
| Landlock (Linux) | `github.com/landlock-lsm/go-landlock` |
| CDP (Chrome) | `github.com/chromedp/chromedp` |
| Gitignore patterns | `github.com/denormal/go-gitignore` |
| Diff parsing | `github.com/sourcegraph/go-diff` |
