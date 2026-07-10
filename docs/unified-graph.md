# Unified Knowledge Graph: Filesystem + Codebase + Memory

Design for a single graph substrate that unifies the user's filesystem, codebase, and the agent's persistent memory — turning `cd`/`ls`/`grep`/`cat` workflows into graph traversals, and blurring the line between files on disk and the agent's recall.

> **Status:** Design document. Phase 0 spikes pending; no production code yet. Builds on the search/index/graph stack documented in [`docs/code-search.md`](./code-search.md) and the memory layers in [`docs/memory.md`](./memory.md).

## Motivation

Agentic coding tools (Claude Code, Cursor, Codex, Aider, Cline, etc.) drive filesystem and codebase work through the same primitives a human shell user has: `cd`, `ls`, `grep`, `find`, `cat`, `sed` — plus per-tool wrappers like `Read`/`Write`/`Edit`/`Glob`/`Grep`. This is fast to bootstrap but burns tokens, blocks structured queries, and treats every file as opaque bytes.

The endgame is to treat the filesystem, the codebase, and the agent's persistent memory as **one unified knowledge graph**. Files, directories, sections, tables, images, code symbols, memories, sessions — all become nodes. "What is in this directory?", "What tables are in this PDF?", "What memories cite this function?" all become graph traversals. Reads and writes happen *through* the graph, so retrieval and mutation share one substrate.

**The promise:** the graph becomes a semantic knowledge base for memory, blurring the line between filesystem files and agentic memory.

## State of the Art (May 2026)

| Approach | Representative | What it gives | Limitation |
|---|---|---|---|
| Plain shell + grep | Claude Code, Codex CLI, Cline, Gemini CLI | Lowest friction; ripgrep is the highest-frequency tool call in Claude Code | Token-heavy traversal; no structure |
| Tree-sitter repomap | Aider | PageRank-weighted file→symbol map; token-budgeted | Static snapshot; no cross-symbol traversal; no memory binding |
| Structural search | ast-grep + ast-grep-mcp | Syntax-aware patterns; refactor-safe edits | Per-language rules; learning curve |
| SCIP / LSIF | Sourcegraph, GitLab | Precomputed defs/refs/hovers; cross-repo | Heavy indexer; needs a build step |
| Stack graphs | GitHub | Name resolution across files | **Archived Sep 2025** — industry moved away from monolithic frameworks |
| Code KG + GraphRAG | Memgraph graph-code demo; tree-sitter KG papers | Sub-second graph queries over 49k-node code KGs | Read-only retrieval; FS + memory not unified |
| Agentic search (no RAG) | Claude Code | Team explicitly chose agentic search over embedding RAG | Quality depends entirely on tool-call planning |
| Hybrid graph + vector + FTS | Cursor codebase indexing; HippoRAG; research prototypes | Best recall; semantic + structural | Per-vendor; closed; not portable |

**Industry direction:** composable, file-incremental, tree-sitter-first indexers over monolithic frameworks; SCIP wins over LSIF; agentic search wins over embedding RAG for *code*; vectors stay useful for *memory* and *docs*; graphs win for *structure*. **No mainstream tool yet unifies FS + code + memory in one graph — this is the open seam.**

## ycode's Existing Foundation

ycode is already further along than most. The graph layer exists and is wired end-to-end; the missing piece is *filesystem as a first-class layer in the same graph*.

| Layer | Component | Notes |
|---|---|---|
| Graph store | **bonsai** (`peers/bonsai/`) | Embedded single-node Dgraph fork, Badger KV backend, Apache-2.0, pure-Go, DQL parser at `peers/bonsai/dql/dql.go`. Persisted at `~/.agents/ycode/projects/data/graph/`. |
| Graph schema | `pkg/memex/graph/schema.go` | Already hosts **two** schemas in one store: `memory.*` (name, type, scope, related_to, supersedes, derived_from) and `code.*` (label, kind, file, community, calls, uses). |
| Code KG | gfy via `internal/runtime/codegraph/` | Detect → extract → build → cluster → analyze; cache at `.agents/ycode/graph.json`; async mirror to bonsai. |
| Symbol index | `internal/runtime/indexer/symbol_indexer.go` | Bleve FTS over treesitter-extracted symbols. |
| Repomap | `internal/runtime/repomap/repomap.go` | Aider-class PageRank with 50× boost for chat-context files. |
| Fusion | `pkg/memex/memory/fusion.go` | RRF (k=60) + MMR (λ=0.7) across BM25 / vector / memos. |
| Vector store | chromem-go via `pkg/memex/` | HNSW for embeddings. Indexes memories/sessions today; not yet code symbols or document regions. |
| Memory layers | `docs/memory.md` | Five layers: Working → Short-term → Long-term → Contextual → Persistent. |

**The gaps** (what this design closes):

1. No first-class `Directory` / `File` node type. Files are referenced only as a `code.file` *attribute* — can't DQL-traverse "what's in this dir" or "what code nodes belong to this file".
2. No per-file subgraph. Markdown, PDF, notebooks, YAML are read as blobs, not as `Heading` / `Table` / `Image` / `Cell` / `Region` nodes.
3. No cross-layer edges between `Memory` and `File` / `CodeNode`. `memory.related_to` exists, but only between memories.
4. No `Region` node for line-ranges or regex-matched spans.
5. Vectors not attached to graph nodes — they index memories/sessions, not code symbols or document regions.
6. LSP not folded into the graph.
7. No graph-resident *write* tools — edits go through VFS, then the graph is rebuilt.

## Architecture

```
LLM Agent
  |
  |-- fs_walk -----------> DQL traversal over Directory + File
  |-- fs_open -----------> File manifest + child Region IDs
  |-- region_read -------> by node ID; role-specific payload (table → CSV, image → bytes)
  |-- file_read_lines ---> line range; transient Region if not indexed
  |-- node_for_query ----> RRF(BM25, HNSW vector, PPR graph walk) → ranked node IDs
  |-- memory_for_node ---> @reverse traversal of memory.mentions_*
  |-- region_replace ----> atomic file+graph edit
  |
  Bonsai (Apache-2.0, pure-Go, embedded)
    |-- Memory layer  (memory.name, memory.related_to, memory.mentions_*)
    |-- Code layer    (code.label, code.calls, code.defined_in, code.declared_at)
    |-- FS layer      (fs.path, fs.parent, fs.contains, fs.content_hash)         [NEW]
    |-- Region layer  (region.role, region.line_start/end, region.embedding)     [NEW]
    |
    Indexers (file-incremental, fsnotify-triggered)
      |-- walker.go    -> Directory + File nodes (gitignore-aware)
      |-- code.go      -> reuse treesitter + gfy → defined_in / declared_at
      |-- md.go        -> goldmark → heading/paragraph/codeblock/table/image
      |-- document.go  -> reuse read_document → page/paragraph/table/image
      |-- notebook.go  -> reuse notebook_read → cells
      |-- yaml/json    -> treesitter → key_path regions
```

## Graph DB Decision

**Stay on bonsai.** Surveying the 2026 embedded-graph landscape confirms it as the best fit:

| Option | Status | Why not |
|---|---|---|
| **bonsai** (Dgraph fork) | **Chosen** | Apache-2.0, pure-Go, embedded, DQL parser vendored, already hosts memory+code schemas, ycode controls the fork |
| KuzuDB | **Archived Oct 2025** (Apple acquisition) | No longer maintained |
| CozoDB | Development reportedly slowed | Rust + Datalog; FFI binding would need CGO |
| Memgraph | Source-available (BSL 1.1) | Not OSI open-source; in-memory only |
| NebulaGraph | Apache-2.0, distributed | Heavy ops surface — overkill for single-binary local use |
| Dgraph (upstream) | Apache-2.0 → mixed BSL | Distributed-first; bonsai already strips this down |
| ArcadeDB | Apache-2.0, multi-model | JVM dependency |
| Apache AGE | Apache-2.0 | Requires PostgreSQL |
| Neo4j Community | GPLv3 | License incompatible with permissive-only policy |

**Cloud-storage forward-compat:** Badger's KV abstraction already separates storage from the graph layer. Swapping in an S3/GCS-backed store (custom `badger.Store`, or FUSE mount under `badger.Options.Dir`) is mechanically tractable. The UID-keyed + content-hash schema is portable to any property-graph backend, so even if bonsai is later replaced the indexer pipeline survives.

## Graph Algorithms

The unified graph will be large (≥10⁵ nodes per active project, region nodes inflate this further). Algorithm selection up-front is what keeps it usable.

### Structure discovery (offline / batch)

| Slot | Pick | Why over alternatives |
|---|---|---|
| Community detection | **Leiden** | Guaranteed well-connected communities — fixes Louvain's resolution-limit bug at comparable cost ([Traag et al., 2019](https://www.nature.com/articles/s41598-019-41695-z)). Replace gfy's current cluster step. |
| Centrality | Degree + Eigenvector + Betweenness | Degree is O(E); eigenvector catches "important neighbors"; betweenness finds bridges. Extend gfy's existing `GodNodes`. |
| Hierarchical clustering | Hierarchical Leiden (multi-resolution) | Lets queries pick coarse vs fine community boundary at runtime. |
| Surprising connections | Modularity-violation edges | Already in gfy as `SurprisingConnections`. |

### Streaming / incremental (online — the most important class)

A filesystem mutates constantly. **Naive batch reindex is a non-starter.**

| Approach | Cost | Quality |
|---|---|---|
| Naive Dynamic (ND) | Recompute everything affected | Highest |
| Delta-Screening (DS) | Re-process only vertices where delta-modularity changes | Lower, very fast |
| **Dynamic Frontier (DF)** | Incrementally expand from changed vertices until quality stabilizes | Near-ND at fraction of cost — **current SOTA** ([arXiv 2024](https://arxiv.org/html/2405.11658v1)) |

**Pick DF as primary, DS as fallback for very high-churn paths.** Implementation hook: `internal/runtime/codegraph/codegraph.go:582` (`NotifyFileChanged`) already debounces dirty events.

### Retrieval (online — every agent turn)

The token-efficiency win. Reuse and extend what's already in `pkg/memex/memory/fusion.go`:

| Algorithm | Use | Existing? |
|---|---|---|
| **Personalized PageRank (PPR)** | Seed-set expansion from a query/memory; random-walk weighting | New (HippoRAG-style) |
| Bidirectional BFS / A* | Path between memory↔file or symbol↔symbol | gfy has `ShortestPath` |
| HNSW | Vector index for `region.embedding` | chromem-go already |
| **RRF** | Merge graph PPR + vector HNSW + BM25 | `fusion.go` — extend with PPR as fourth source |
| MMR | Top-k diversity rerank | `fusion.go` (λ=0.7) |
| Frontier expansion | 2-hop walk from vector-seeded nodes | New (standard GraphRAG pattern) |

**The "GraphRAG move" mapped onto ycode:** vector search finds seed nodes via region-embedding HNSW; PPR or 2-hop frontier expansion in bonsai walks outward through `code.calls`, `region.in_file`, `memory.mentions_*`; RRF merges both rankings. This is the single most token-efficient retrieval pattern in the 2026 literature.

### Embeddings + similarity

| Algorithm | Use |
|---|---|
| LLM-text-embedding | Semantic embeddings for document/code regions; HNSW-indexed |
| node2vec / DeepWalk | Structural embeddings for code symbols at zero token cost |
| Random-walk sketches | Fast "neighborhood signature" for fuzzy region dedup |

**Ship LLM-text in v1; defer node2vec to v2.** Field `region.embedding` accommodates either.

### Maintenance + scale

| Concern | Pattern |
|---|---|
| Hot-path predicate lookups | Existing Badger LSM + bonsai indices — no change |
| Reachability | 2-hop labeling (offline precompute) if shortest-path queries become hot — not v1 |
| Graph partitioning (cloud-future) | METIS / KaHIP when bonsai sharded — out of scope for v1 |
| Compression | Badger + Snappy already — no action |
| GC of stale nodes | TTL on `Region` whose `fs.content_hash` is outdated; periodic sweep during idle |
| Watch / change detection | `fsnotify` (Go ecosystem standard) + 100ms debounce → DF processor |

## Implementation Phases

### Phase 0: Foundations (de-risk before schema changes)

Five 1-2 day spikes. If any surfaces a blocker, scope adjusts before commitment.

| Spike | Goal | Where |
|---|---|---|
| Algorithm interface abstraction | `Cluster(g) → communities` swappable Louvain↔Leiden | `internal/runtime/codegraph/` |
| Dynamic Frontier prototype | DF on synthetic graph behind feature flag | new `internal/runtime/graphalg/` |
| PPR primitive in bonsai | Personalized PageRank as DQL extension or Go-side walker | `pkg/memex/graph/` |
| RRF + PPR integration test | `fusion.go` accepts PPR as fourth source; sanity on fixture | `pkg/memex/memory/` |
| fsnotify watcher skeleton | Debounced events fire on file save in tmpdir test | new `internal/runtime/fsindexer/` |

### Phase A: Filesystem layer in the schema

Bump `SchemaVersion` to `"2"` in `pkg/memex/graph/schema.go` and add:

```
# Filesystem predicates.
fs.path:          string   @index(exact, term, trigram) @upsert .
fs.name:          string   @index(term) .
fs.kind:          string   @index(exact) .         # directory|file|symlink
fs.size:          int      @index(int) .
fs.mtime:         dateTime @index(hour) .
fs.mime:          string   @index(exact) .
fs.lang:          string   @index(exact) .
fs.parent:        uid      @reverse .
fs.contains:      [uid]    @reverse @count .
fs.gitignored:    bool     @index(bool) .
fs.content_hash:  string   @index(exact) .

# Region predicates (per-file subgraph).
region.role:      string   @index(exact) .         # heading|table|image|codeblock|cell|paragraph|symbol
region.label:     string   @index(term) .
region.line_start:int      @index(int) .
region.line_end:  int      @index(int) .
region.byte_start:int .
region.byte_end:  int .
region.lang:      string   @index(exact) .
region.alt:       string .
region.in_file:   uid      @reverse @count .
region.embedding: float32vector .

# Cross-layer edges.
memory.mentions_file:   [uid] @reverse @count .
memory.mentions_code:   [uid] @reverse @count .
memory.mentions_region: [uid] @reverse @count .
code.defined_in:        uid   @reverse .           # replaces code.file string in v3
code.declared_at:       uid   @reverse .

type Directory { fs.path fs.name fs.kind fs.parent fs.contains fs.gitignored }
type File      { fs.path fs.name fs.kind fs.size fs.mtime fs.mime fs.lang fs.parent fs.content_hash }
type Region    { region.role region.label region.line_start region.line_end region.lang region.alt region.in_file region.embedding }
```

`fs.path` is `@upsert` so re-indexing is idempotent. `region.embedding` is optional and populated only where it earns its keep.

### Phase B: Indexers (file-incremental, fsnotify-triggered)

New package `internal/runtime/fsindexer/`. One pipeline per file kind, dispatched on MIME/extension:

| Kind | Producer | Region roles |
|---|---|---|
| Directory | `walker.go` (gitignore-aware via go-git) | (none — Directory nodes only) |
| Source code | reuse `internal/runtime/treesitter/` + gfy; emit `code.defined_in → File` and `code.declared_at → Region` | `symbol` |
| Markdown | `parser/md.go` (goldmark) | `heading`, `paragraph`, `codeblock`, `table`, `image` |
| PDF | reuse `read_document` PDF extractor | `page`, `paragraph`, `table`, `image` |
| Notebook | reuse `notebook_read` | `cell` (lang=python\|markdown) |
| Office (docx/xlsx/pptx) | reuse `read_document` | `paragraph`, `table`, `slide`, `image` |
| YAML/JSON/TOML | treesitter | `key_path` (jq-addressable) |
| Plain text | line-based + optional regex | `lines` (lazy per query) |

**Cache layout:** `~/.agents/ycode/projects/<id>/fsindex/` mirrors the existing graph cache. Per-file manifest `{path, content_hash, indexed_at}` lets the walker skip unchanged files. Reuse the async batching pattern from `internal/runtime/codegraph/mirror.go` (500 nquads/batch).

### Phase C: Cross-layer wiring (memory ↔ FS)

New package `internal/runtime/memlink/`. When a memory is written:
1. Extract `fs.path` references, fenced code blocks with `lang:path` hints, and `@symbol` mentions from the memory body.
2. Look up `fs.path` / `code.label` / `region.label` in bonsai.
3. Create `memory.mentions_*` edges with confidence scores. Async, 100ms debounce.
4. Inverse retrieval ("what memories touch this file/symbol") falls out for free via `@reverse`.

This is the literal "blurring the line" — a memory and a file become two nodes in one graph, linked by typed edges, queryable in one DQL call.

### Phase D: Graph-resident tool calls

All deferred (discovered via `ToolSearch`). Add to `internal/tools/specs.go`:

| Tool | Mode | Purpose |
|---|---|---|
| `fs_walk` | ReadOnly | DQL traversal of `Directory`+`File`; depth/lang/mime filters; agentic replacement for `find`/`tree` |
| `fs_open` | ReadOnly | File node manifest: mime, lang, region role distribution, child Region IDs |
| `region_read` | ReadOnly | Single region by node ID; role-specific payload |
| `region_read_table` | ReadOnly | `table` region as columnar rows |
| `region_read_image` | ReadOnly | `image` region → resolved file path for `view_image` |
| `file_read_lines` | ReadOnly | `file:start-end`; creates transient Region if absent |
| `file_read_regex` | ReadOnly | Regex-matched spans returned as ordered regions |
| `graph_neighbors_typed` | ReadOnly | Neighbor walk with predicate filter (safe DQL templates) |
| `memory_for_node` | ReadOnly | All memories edged to a File/CodeNode/Region; RRF over edges + vector |
| `node_for_query` | ReadOnly | Mixed retrieval: RRF(BM25 × HNSW × PPR) → ranked node IDs |
| `region_replace` | WorkspaceWrite | Atomic file+Region edit; re-emits neighboring regions |

**Style rules:**
- Tools return node IDs; agents chain calls by passing IDs back, not strings.
- Hint engine (`internal/shell/agentmode/hints.go`) suggests graph tools on `find`/`tree`/`grep -r`/`cat | head/tail` patterns.

### Phase E: Unified retrieval

Extend `pkg/memex/memory/fusion.go` to accept graph-traversal results as a fourth ranked input (currently three: BM25 / vector / memos). Then `node_for_query` and `memory_for_node` use the same fusion that already works for memory recall. **The agent doesn't need to know whether a hit came from BM25 over filenames, vector over region embeddings, or DQL over `code.calls` — it just gets ranked node IDs and reads them.**

## Non-goals (v1)

- SCIP/LSIF replacement. Treesitter + LSP remain the data sources; SCIP can be a future import format.
- Full ast-grep replacement.
- Multi-repo cross-indexing.
- Removing existing `read_file`/`write_file`/`grep_search`. Graph tools are **additive**; the hint engine surfaces them when relevant.

## Bonus / follow-on

- **LSP-into-graph**: persist `definition`/`references`/`hover` as `code.defined_at → Region`, `code.references → CodeNode`.
- **ast-grep MCP wrapper**: `structural_search` tool returning Region IDs.
- **SCIP import**: one-shot importer for repos with existing SCIP indexes.
- **Embeddings on demand**: lazy-compute `region.embedding` only for regions ≥ N tokens or returned more than K times.
- **Time-travel queries**: link a `Commit` node + `fs.content_hash` → "what did this symbol look like at commit X" as a graph query.
- **External knowledge surfaces**: fetched web content, dependency READMEs, licenses — all become File+Region nodes; memories edge to them like local files.

## Validation

| # | Check | What it covers |
|---|---|---|
| 1 | Schema migration round-trip | v1→v2 apply is idempotent; existing `memory.*` / `code.*` data unaffected |
| 2 | Indexer correctness | Golden-file tests per parser kind (Markdown / PDF / notebook / Go source) under `testdata/` |
| 3 | Cross-layer round-trip | Write memory mentioning `internal/runtime/conversation/runtime.go:42-58` → confirm `memory.mentions_region` edge → `memory_for_node` returns it |
| 4 | Hot-rebuild path | Edit file → fsindexer rebuilds only touched file's regions (verify via slog) → DQL sees fresh data < 2s |
| 5 | Token-budget realism | Tokens-per-task on fixed workload: (a) grep+read, (b) repomap+read, (c) `node_for_query`+`region_read`. Expect (c) to win on large repos |
| 6 | End-to-end agent run | `bin/ycode prompt "What memories do I have about the runtime package?"` → confirm tool selects `memory_for_node`/`node_for_query` and returns linked memories |
| 7 | CI parity | `make ci-fast` green; new packages in verify-features matrix |

## Risks & mitigations

- **Schema churn breaks existing graphs.** Mitigate via `SchemaVersion` gate + migration tool (`ycode graph migrate --from=1 --to=2`) that re-applies schema and triggers full reindex.
- **Indexer cost on large repos.** Default to file-incremental. Full reindex only on `ycode serve` cold-start or explicit `ycode index rebuild`. Reuse existing dirty-flag pattern.
- **Tool surface bloat.** All new tools deferred; hint engine surfaces them only when relevant.
- **Bonsai write throughput.** Existing mirror batches at 500 nquads — reuse for fsindexer. If contention shows up, single-writer goroutine in front.
- **Vector-store cost.** Embeddings optional and lazy. Default empty; populate on second+ retrieval.
- **Backwards compat with `code.file` string.** Emit both `code.file` and the new `code.defined_in` UID edge for one release; remove in v3.

## Key Files

| File | Purpose |
|---|---|
| `pkg/memex/graph/schema.go` | Add fs.* / region.* / cross-layer predicates; bump `SchemaVersion = "2"` |
| `pkg/memex/graph/graph.go` | Schema apply, types |
| `internal/runtime/fsindexer/walker.go` *(new)* | Directory walk, gitignore-aware |
| `internal/runtime/fsindexer/dispatch.go` *(new)* | Kind detection + sub-parser routing |
| `internal/runtime/fsindexer/parser/{md,code,notebook,document,yaml}.go` *(new)* | Per-kind region extractors |
| `internal/runtime/fsindexer/manifest.go` *(new)* | Per-file cache invalidation |
| `internal/runtime/codegraph/codegraph.go` | Extend `NotifyFileChanged` to fan out to fsindexer |
| `internal/runtime/codegraph/mirror.go` | New helpers for File/Region/Cross-layer nquads |
| `internal/runtime/memlink/` *(new)* | Memory-body → graph-edge extractor |
| `internal/runtime/graphalg/` *(new)* | Leiden, Dynamic Frontier, Personalized PageRank |
| `internal/tools/specs.go` | Add ~11 new tool specs (fs_walk, fs_open, region_read*, file_read_*, graph_neighbors_typed, memory_for_node, node_for_query, region_replace) |
| `internal/tools/registry.go` | Register new tools |
| `internal/tools/fs_graph_tools.go` *(new)* | Implementations |
| `pkg/memex/memory/fusion.go` | Extend `ReciprocalRankFusion` to accept PPR as a fourth source |
| `internal/shell/agentmode/hints.go` | Hints on `find`/`tree`/`grep -r`/`cat \| head/tail` patterns |
| `internal/shell/builtins/registry.go` | Optional `yc fs` verb (walk / open / regions) |

## Open Questions

- `region.embedding` in chromem-go (new collection) or a separate per-region store? — recommend chromem-go.
- `yc fs` shell verb at v1 or wait until the tool surface settles?
- `memory.mentions_*` extraction sync (latency hit) or async (slight staleness)? — recommend async, 100ms debounce.
- Gitignore enforcement: skip on walk but allow explicit `fs_open` of any path so agents can still reach build artifacts when asked.

## References

- [Code Search Architecture](./code-search.md) — existing Phase 1-4 search/index stack this design extends
- [Memory System](./memory.md) — five-layer model that becomes the substrate of the graph
- [Strategy](./strategy.md) — feature-tier policy and graduation criteria

### External

- [From Louvain to Leiden — Nature Sci. Rep.](https://www.nature.com/articles/s41598-019-41695-z)
- [Dynamic Community Detection with Leiden — arXiv 2024](https://arxiv.org/html/2405.11658v1)
- [Towards Practical GraphRAG — arXiv 2507.03226](https://arxiv.org/html/2507.03226v3)
- [Hybrid Retrieval & GraphRAG — Medium Feb 2026](https://medium.com/@QuarkAndCode/hybrid-retrieval-graphrag-vectors-graphs-for-better-rag-c88cde12c44e)
- [KuzuDB archived — BigGo News Oct 2025](https://biggo.com/news/202510130126_KuzuDB-embedded-graph-database-archived)
- [Neo4j alternatives in 2026 — ArcadeDB](https://arcadedb.com/blog/neo4j-alternatives-in-2026-a-fair-look-at-the-open-source-options/)
- [SCIP announcement — Sourcegraph](https://sourcegraph.com/blog/announcing-scip)
- [Inside Claude Code Architecture — Zain Hasan](https://zainhas.github.io/blog/2026/inside-claude-code-architecture/)
