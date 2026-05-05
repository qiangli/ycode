package graph

// Schema returns the canonical DQL schema applied by Open. It declares
// predicates for both the memory layer (memory.*) and the code layer
// (code.*). Embedders may apply additional Alter calls afterward to add
// their own predicates.
//
// Versioning: when this schema changes in a way that breaks existing
// data, bump the version below; downstream tools can read it via
// SchemaVersion to gate ingest.
const SchemaVersion = "1"

// Schema returns the DQL schema text. Authoring style: one predicate per
// line, types declared as `type` blocks.
func Schema() string {
	return `
# memex graph schema v` + SchemaVersion + `
#
# Memory predicates: relations between persisted memory entries.
memory.name: string @index(exact, term) @upsert .
memory.type: string @index(exact) .
memory.scope: string @index(exact) .
memory.content_hash: string @index(exact) .
memory.created_at: dateTime @index(hour) .
memory.related_to: [uid] @reverse @count .
memory.supersedes: uid @reverse .
memory.derived_from: [uid] @reverse .

# Code predicates: mirror of gfy's code knowledge graph.
code.label: string @index(exact, term, trigram) @upsert .
code.kind: string @index(exact) .
code.file: string @index(exact, term) .
code.community: int @index(int) .
code.calls: [uid] @reverse @count .
code.uses: [uid] @reverse @count .

# Type definitions.
type Memory {
  memory.name
  memory.type
  memory.scope
  memory.content_hash
  memory.created_at
  memory.related_to
  memory.supersedes
  memory.derived_from
}

type CodeNode {
  code.label
  code.kind
  code.file
  code.community
  code.calls
  code.uses
}
`
}
