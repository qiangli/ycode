// Package memex is the umbrella for ycode's reusable memory + persistence
// toolkit.
//
// memex (a nod to Vannevar Bush's "memory extension" device) bundles three
// independently-importable subpackages:
//
//   - store   Pure-Go persistence backends (KV, SQL, search, vector).
//     Has no semantic awareness of memory or notes; safe to depend on
//     from any subsystem (sessions, MCP, config caches, etc.).
//   - memory  File-based memory store with markdown + YAML frontmatter,
//     4-backend RRF retrieval, persona, and dreamer consolidation.
//   - memos   SQLite-backed wiki notes with REST API and embedded web UI.
//
// The umbrella package itself adds:
//   - Memex   A convenience constructor (Open) that wires the three together
//     with sensible defaults, plus a VFS overlay that surfaces both
//     memories and memo notes under a unified virtual path tree.
//   - VFS     Read-write virtual filesystem for human/agent browsing of
//     both backends as if they were one wiki.
//
// Embedders that need only one piece can import the leaf directly; the
// umbrella is optional.
//
// # Stability
//
// Subpackage public surfaces are stable and covered by the memex semver
// promise. The umbrella's Memex/VFS surface is currently v0/unstable and may
// evolve. See each subpackage's doc.go for layer-specific guarantees.
package memex
