# Persistence Layer TODO Checklist

## Phase 1: Core Storage

- [x] Define storage interfaces (`KVStore`, `SQLStore`, `VectorStore`, `SearchIndex`)
- [x] Implement `StorageManager` with progressive 3-phase initialization
- [x] Implement bbolt KV store (`internal/storage/kv/`)
- [x] Implement SQLite store with versioned migrations (`internal/storage/sqlite/`)
- [x] Implement Bleve search index with lazy creation (`internal/storage/search/`)
- [x] Implement chromem-go vector store with persistence (`internal/storage/vector/`)
- [x] Add dependencies: `modernc.org/sqlite`, `go.etcd.io/bbolt`, `bleve/v2`, `chromem-go`
- [x] Write unit tests for all backends
- [x] Full project `go vet` and `go test -race` passing

## Phase 2: Integration

- [x] Wire `StorageManager` into `cmd/ycode/main.go` initialization
- [x] Add `StorageManager` to `App` struct and `AppOptions`
- [x] Dual-write session metadata to JSONL + SQLite (`session/sqlwriter.go`)
- [x] Index existing JSONL sessions into SQLite on first run (`session/indexer.go`)
- [x] Replace in-memory config cache with bbolt-backed cache (`config/cache.go`)
- [x] Replace in-memory permission cache with bbolt-backed cache (`permission/cache.go`)
- [x] Add prompt cache table TTL eviction background goroutine (`storage/eviction.go`)
- [x] Add tool usage metrics recording to SQLite (`tools/metrics.go`)

## Phase 3: Search Integration

- [x] Wire Bleve into `Grep` tool for natural-language fallback
- [x] Wire Bleve into `ToolSearch` deferred tool discovery
- [x] Background codebase indexer (`runtime/indexer/indexer.go`)
- [x] Index memory entries into Bleve on save (`memory/bleveindex.go`)
- [x] Index session messages into Bleve on compaction (`session/searchindex.go`)
- [x] Replace `memory.Search()` keyword matching with Bleve

## Phase 4: Vector Integration

- [x] Define embedding provider interface (`runtime/embedding/embedding.go`)
- [x] Implement OpenAI-compatible API embedding provider (`runtime/embedding/api.go`)
- [x] Wire vector store into memory `Recall()` for semantic similarity (`memory/vectorindex.go`)
- [x] Background embedder for code chunks (`runtime/embedding/embedder.go`)
- [x] Background embedder for memory entries
- [x] Background embedder for session summaries
- [x] Add semantic code search tool (`tools/semantic.go`)
- [x] Add vector-enhanced documentation retrieval

## Phase 5: Optimization

- [x] Benchmark storage backends under realistic workloads (`storage/benchmark_test.go`)
- [x] Add SQLite connection pooling tuning (`storage/sqlite/sqlite.go`)
- [x] Add Bleve index compaction scheduling (`storage/search/search.go`)
- [x] Add chromem-go persistence tuning (`storage/vector/vector.go`)
- [x] Monitor binary size impact and add build tags for optional backends
- [x] Add storage health check to `doctor` diagnostic output
