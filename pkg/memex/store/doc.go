// Package store is the persistence cornerstone of the memex toolkit.
//
// It provides four pure-Go backends that any agentic tool can compose:
//
//   - kv      bbolt-backed key-value store (config, permissions, metadata)
//   - sqlite  modernc.org/sqlite (sessions, messages, tasks, memo notes)
//   - search  Bleve v2 full-text search (BM25, fuzzy, phrase, faceted)
//   - vector  chromem-go embeddable vector database (cosine similarity)
//
// All backends are zero-CGO and ship with permissive licenses.
//
// # Layering
//
// store has no semantic awareness of "memory", "notes", or any other domain.
// Higher-level memex subpackages (memex/memory, memex/notes) build on top of
// store; ycode subsystems such as sessions, MCP, and config use it directly
// without going through the higher layers. Embedders pull in only what they
// need:
//
//	import "github.com/qiangli/ycode/pkg/memex/store/kv"   // just bbolt
//	import "github.com/qiangli/ycode/pkg/memex/store"      // 3-phase Manager
//
// # Three-phase initialization
//
// Manager orchestrates the four backends with progressive readiness so the
// host process is not blocked by slow openers:
//
//	Phase 1 (instant):     KV opens synchronously
//	Phase 2 (background):  SQL opens + runs migrations in a goroutine
//	Phase 3 (lazy):        Search and Vector open on first use
//
// Use Manager.SQL(ctx) and Manager.Vector(ctx) to block until the
// corresponding phase completes; KV() never blocks.
//
// # Stability
//
// The interfaces, Manager, and Document types are part of the public API and
// covered by the memex semver promise. Subpackage Open() constructors are
// also stable. Internal helpers (txStore, etc.) may change without notice.
package store
