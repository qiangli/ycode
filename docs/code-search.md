# Code Search Architecture

Best-in-class codebase search for ycode — combining indexed search, AST-aware structural queries, reference graphs, and graph-ranked relevance.

## Motivation

AI coding agents spend most tool calls on codebase search (grep, glob, file reads). Every competitor (Claude Code, Cline, Continue, Aider, OpenHands, OpenCode) shells out to ripgrep for text search — O(files) per query, no indexing, no structural awareness. ycode already has a Bleve full-text index and background indexer, but they were disconnected from the search tools. This initiative connects them and adds capabilities no competitor has.

## Architecture

```
LLM Agent
  |
  |-- grep_search -----> [literal extraction] -> Bleve candidate -> regex verify
  |-- glob_search -----> [shared walker + doublestar]
  |-- symbol_search ---> Bleve "symbols" index
  |-- find_references -> reference graph in bbolt
  |-- find_impact -----> reference graph traversal N levels
  |-- ast_search ------> containerized ast-grep
  |-- semantic_search -> chromem-go vector store
  |
  Background Indexer (5-min cycle + on-change notifications)
    |-- Bleve "code" index (full-text, BM25)
    |-- Bleve "symbols" index (structured symbol data)
    |-- bbolt reference graph (caller/callee edges)
    |-- [optional] trigram index (regex acceleration)
    |-- [optional] vector embeddings (semantic search)
```

## Research: Competitors & External Tools

| Source | Technique | Integration |
|--------|-----------|-------------|
| Zoekt (Apache-2.0) | Trigram indexing for sub-linear regex search | Import as Go library or build lightweight custom trigram index |
| ast-grep (MIT) | Tree-sitter structural patterns (`$FUNC($$$ARGS)`) | Containerized tool via `containertool.Tool` pattern |
| Tessera (MIT) | Impact analysis via reference graph | Go-native reference graph using go/ast |
| Aider | PageRank on define/reference graph | Port edge weighting heuristics to Go |
| Cline | ripgrep + tree-sitter AST for definitions | ycode does both natively |

All competitors shell out to ripgrep. None use indexing, structural search, or impact analysis.

## Implementation Phases

### Phase 1: Fundamentals (Complete)

- **Shared walker** (`internal/runtime/fileops/walker.go`): Unified `WalkSourceFiles()` with `DefaultSkipDirs`, `IgnoreChecker` (.gitignore/.ycodeignore), binary detection, file-size limits. Replaced 4 duplicate skip-dir lists across grep, glob, indexer, embedder.
- **Context lines**: `GrepSearch` now supports `-C/-B/-A` with merged overlapping windows. `GrepMatch.IsContext` distinguishes context from match lines.
- **Glob `**`**: Added `github.com/bmatcuk/doublestar/v4` (MIT). Patterns like `src/**/*.go` and `**/cmd/**/main.go` now work correctly.
- **Pagination**: `GrepParams.Offset` enables paging through large result sets.

### Phase 2: Bleve-Accelerated Grep (Complete)

10-100x speedup on large codebases by querying the Bleve index before walking.

- **SearchWithFilter** on Bleve Store: `ConjunctionQuery` combining text + metadata filters (language, path).
- **Two-stage indexed grep** (`grep_indexed.go`): Extract literals from regex via `regexp/syntax`, query Bleve for candidate files, regex-verify only candidates. Falls back to full walk when no literals extractable.
- **Incremental freshness**: `NotifyFileChanged()` on indexer, wired to write/edit tool hooks for instant re-indexing of modified files.

### Phase 3: Symbol Search + Reference Graph (Complete)

Structured symbol search and impact analysis — no competitor has this.

- **Symbol indexer** (`symbol_indexer.go`): Go symbols via `go/ast`, other languages via regex patterns or tree-sitter AST extraction (Python, JS/TS, Rust, Java, Ruby). Indexed into Bleve `"symbols"` index with name, kind, file, language, signature, line.
- **`symbol_search` tool**: Query symbols by name, kind, language, exported status.
- **Reference graph** (`refgraph.go`): Bidirectional caller/callee edges from `go/ast`, stored in bbolt KV. Includes `SymbolMatches` for fuzzy lookup.
- **`find_references`/`find_impact` tools**: "Who calls X?" and "If I change X, what breaks?" with N-level BFS traversal.
- **Graph-ranked RepoMap** (Aider-inspired): PageRank on define/reference graph with naming heuristics (sqrt freq scaling, 50x chat-context boost, meaningful-name detection, camelCase/snake_case boost, unexported penalty).

### Phase 4: AST-Aware Structural Search (Complete)

Queries impossible with regex become trivial with AST patterns.

- **In-process tree-sitter** (`internal/runtime/treesitter/`): Native Go bindings via `go-tree-sitter` (CGO). Supports Go, Python, JavaScript, TypeScript, TSX, Rust, Java, C, Ruby. Provides:
  - `Parser.Parse()` — parse source into AST
  - `ExtractSymbols()` — language-aware symbol extraction (functions, types, classes, interfaces)
  - `Search()` — S-expression tree-sitter queries
  - `SearchText()` — ast-grep-style text patterns (`$VAR`, `$$$VAR` wildcards)
  - `Analyze()` — impact analysis (find all references to a symbol across workspace)
- **Containerized ast-grep fallback** (`internal/runtime/astgrep/`): `containertool.Tool` pattern with node:22-alpine multi-stage build. Used when rewrite operations are needed (tree-sitter is read-only).
- **`ast_search` tool**: Tries in-process tree-sitter first, falls back to container for rewrites or unsupported features. No container required for basic structural search.
- **Trigram index** (`trigram.go`): Lightweight in-memory + KV-backed implementation. Per-line trigram extraction, intersection queries for regex acceleration when Bleve literal extraction falls back.

### Future: Semantic Code Search (Not yet implemented)

- Connect chromem-go vector store for natural language queries. Opt-in (requires embedding model via Ollama).

## Validation

Contract-tier tests (deterministic, no LLM) validate that the search infrastructure produces correct, complete results. Run against ycode's own codebase and synthetic projects.

```bash
go test -race -v ./internal/eval/contract/            # full suite (~20s)
go test -short -race ./internal/eval/contract/         # skip codebase tests (~2s)
go test -race -run TestFullPipeline ./internal/eval/contract/  # pipeline only
```

### Test Groups

| # | Test | What It Validates |
|---|------|-------------------|
| 1 | `TestGrepSearch_FindsKnownPatterns` | Grep finds known functions/types in ycode codebase |
| 2 | `TestGrepSearch_ContextLinesCorrectness` | `-C` context lines mark match vs context correctly |
| 3 | `TestGrepSearch_SkipsIgnoredDirs` | No results from `.git`, `node_modules`, `priorart` |
| 4 | `TestGlobSearch_DoubleStarOnRealCodebase` | `**/*.go`, `**/*_test.go` work recursively |
| 5 | `TestSymbolIndexer_RealGoFile` | Go AST extracts symbols with correct metadata |
| 6 | `TestRefGraph_RealGoCode` | Caller/callee edges correct across files |
| 7 | `TestIndexedGrep_ConsistentWithFullWalk` | Indexed path matches full-walk results |
| 8 | `TestTrigramIndex_NarrowsCandidatesCorrectly` | Trigram intersection produces correct file sets |
| 9 | `TestLiteralExtraction_QualityOnRealPatterns` | Regex decomposition works for real agent patterns |
| 10 | `TestFullPipeline_IndexSearchVerify` | End-to-end: index -> grep -> symbols -> refgraph -> trigram |

### Quality Metrics

The `TestRepoMap_GraphRankingImprovement` test logs ranking quality as a metric without hard-failing, enabling trend tracking as the ranking algorithm is tuned over time.

## Key Files

| File | Purpose |
|------|---------|
| `internal/runtime/fileops/walker.go` | Shared walk logic, DefaultSkipDirs, IgnoreChecker |
| `internal/runtime/fileops/grep.go` | Core grep with context lines, offset, index path |
| `internal/runtime/fileops/glob.go` | Glob with doublestar `**` support |
| `internal/runtime/fileops/grep_indexed.go` | Two-stage indexed grep (Phase 2) |
| `internal/runtime/indexer/indexer.go` | Background indexer with change notification |
| `internal/runtime/indexer/symbol_indexer.go` | Symbol extraction + Bleve indexing (Phase 3) |
| `internal/runtime/indexer/refgraph.go` | Reference graph via go/ast (Phase 3) |
| `internal/runtime/indexer/trigram.go` | Trigram index (Phase 4) |
| `internal/runtime/astgrep/astgrep.go` | Containerized ast-grep (Phase 4) |
| `internal/storage/search/search.go` | Bleve store with SearchWithFilter |
| `internal/tools/search.go` | Tool handler wiring |
| `internal/tools/symbol_search.go` | symbol_search tool (Phase 3) |
| `internal/tools/references.go` | find_references + find_impact tools (Phase 3) |
| `internal/runtime/repomap/repomap.go` | Graph-ranked relevance (Phase 3) |
| `internal/runtime/repomap/pagerank.go` | PageRank + naming heuristics (Phase 3) |
| `internal/tools/ast_search.go` | ast_search tool (Phase 4) |
| `internal/eval/contract/search_validation_test.go` | E2E validation tests (10 groups) |
