# Gap Analysis: GeminiCLI — Built-in Tool System & Tool Use

**Tool:** Google Gemini CLI (TypeScript, Apache-2.0 license)
**Domain:** Built-in Tool System & Tool Use
**Date:** 2026-05-03

---

## Where ycode Is Stronger

| Area | ycode | GeminiCLI |
|------|-------|-----------|
| Tool count | 70+ tools | 30+ built-in + MCP |
| Execution tiers | 3-tier (native Go → host exec → container) | Single tier (subprocess) |
| Browser automation | Container-based browser-use | No native browser |
| Web search | 5-provider chain | Google search integration |
| Shell execution | In-process mvdan/sh interpreter | Subprocess with platform sandboxing |
| Container tools | Generic containertool framework | No container tools |
| Test runner | Multi-language (Go, Python, JS, Rust) | No built-in test runner |
| Git operations | 31 native go-git functions | Minimal git utilities |

## Where GeminiCLI Excels

| Area | GeminiCLI | ycode |
|------|-----------|-------|
| OS-native sandboxing | Seatbelt (macOS), bubblewrap (Linux), restricted tokens (Windows) | VFS boundary checking only |
| IDE integration | Diff acceptance/rejection workflow via VS Code protocol | No IDE integration |
| Tool discovery | External command returning JSON tool definitions | Static tool registration |
| Plan mode | Restricted writes (only .md in plans dir) during planning | Plan mode exists but no file restriction |

## Gaps Identified

| ID | Feature | GeminiCLI Implementation | ycode Status | Priority | Effort |
|----|---------|-------------------------|--------------|----------|--------|
| T1 | Omission placeholder detection in writes | Detects `...` and `/* ... */` in write content; warns about truncated output | ycode has no truncation detection in writes | Medium | Low |
| T2 | OS-native sandboxing | Seatbelt (macOS), bubblewrap (Linux) for process isolation | ycode uses VFS (path validation) not OS-level sandboxing | Low | High |
| T3 | Tool discovery via external commands | Run configurable command returning JSON array of FunctionDeclarations | ycode discovers tools via MCP only | Low | Medium |
| T4 | IDE diff integration | Send diffs to VS Code for accept/reject before applying | ycode has no IDE integration | Low | High |

---

## Implementation Plan

### Phase 1: Omission Placeholder Detection (T1)

**Files to modify:**
- `internal/runtime/fileops/write.go` — Detect and warn about placeholder content

**Design:**
- Before writing, scan content for common omission patterns: `...`, `// ...`, `/* ... */`, `# ...existing code...`
- If detected near the start/end or in isolation on a line, emit a warning in the response
- Don't block the write (the LLM may have legitimate uses) but flag it

---

## Deferred

| ID | Feature | Reason |
|----|---------|--------|
| T2 | OS-native sandboxing | High effort; ycode's VFS + permission modes are adequate for CLI |
| T3 | Tool discovery | ycode's MCP integration already provides external tool discovery |
| T4 | IDE integration | Different deployment model; ycode is terminal-first |

---

## Verification

- Unit tests for omission detection
- `make build` must pass
