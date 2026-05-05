// Package graph is the memex queryable-graph layer, backed by bonsai
// (a trimmed-down embeddable build of Dgraph).
//
// Whereas pkg/memex/store provides KV/SQL/search/vector primitives and
// pkg/memex/memory provides file-based persistent memories, pkg/memex/graph
// provides relational structure with a real query language (DQL) and a
// schema. It is the canonical home for:
//
//   - relations between memory entries (related_to, supersedes, derived_from)
//   - mirrored code-knowledge graphs (function calls, type usages) for
//     ad-hoc DQL traversal alongside gfy's analytical layer
//
// Like every memex subpackage, graph has no dependencies on internal/* and
// is safe to import from any agentic tool.
//
// # Stability
//
// The Open / Close / Alter / Query / Mutate / Upsert / Export surface is
// considered stable and covered by the memex semver promise. The HTTP
// handler returned by HTTPHandler is bonsai's, and follows bonsai's
// stability story (currently v0).
package graph
