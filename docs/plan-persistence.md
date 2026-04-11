# Persistence Layer Architecture Plan

## Overview

ycode uses a four-backend persistence architecture with progressive initialization. The design follows a **local-first, file-primary** philosophy: file-based storage handles bootstrap and portable data, while structured backends (SQLite, Bleve, chromem-go) provide queryability, full-text search, and semantic similarity.

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────┐
│                    ycode persistence                     │
├──────────┬──────────┬───────────┬───────────┬───────────┤
│  Config  │ Sessions │ Structured│  Vectors  │    FTS    │
│  & State │ & History│   Data    │           │           │
├──────────┼──────────┼───────────┼───────────┼───────────┤
│ JSON/JSONL│  JSONL   │  SQLite   │ chromem-go│   Bleve   │
│  (files) │ (files)  │ (modernc) │           │   v2      │
├──────────┴──────────┴───────────┴───────────┴───────────┤
│              bbolt (KV cache / metadata)                  │
├─────────────────────────────────────────────────────────┤
│                 OS filesystem (foundation)                │
└─────────────────────────────────────────────────────────┘
```

## Storage Backends

### 1. bbolt (KV Store) -- `internal/storage/kv/`

- **Library**: `go.etcd.io/bbolt` (MIT, pure Go)
- **File**: `ycode.kv` (single file)
- **Purpose**: Fast key-based lookups for config cache, permission policies, approval history, embedding metadata
- **Characteristics**: Read-heavy, ACID transactions, bucket-based namespacing

### 2. SQLite -- `internal/storage/sqlite/`

- **Library**: `modernc.org/sqlite` (Unlicense, pure Go -- C transpiled via ccgo)
- **File**: `ycode.db`
- **Purpose**: Structured data with relational queries
- **Schema (v1)**: `sessions`, `messages`, `tasks`, `tool_usage`, `prompt_cache` tables
- **PRAGMAs**: WAL, synchronous=NORMAL, cache_size=-64000, foreign_keys=ON

### 3. Bleve (Full-Text Search) -- `internal/storage/search/`

- **Library**: `github.com/blevesearch/bleve/v2` (Apache-2.0, pure Go)
- **Storage**: `.bleve` directories per named index
- **Purpose**: BM25-scored full-text search with fuzzy matching
- **Indexes**: code, sessions, memory, tools

### 4. chromem-go (Vector Store) -- `internal/storage/vector/`

- **Library**: `github.com/philippgille/chromem-go` (MIT, pure Go)
- **Storage**: `vectors/` directory with GZIP-compressed GOB serialization
- **Purpose**: Semantic similarity via cosine/euclidean/dot-product distance
- **Collections**: codebase, memory, sessions, docs

## Initialization Strategy (3-Phase)

```
Phase 1 (instant, <50ms):    KV store opens synchronously → ready for config/permission lookups
Phase 2 (background, ~1-2s): SQLite opens + migrations in goroutine
Phase 3 (lazy, on demand):   Bleve + chromem-go open in goroutines when first accessed
```

Managed by `StorageManager` (`internal/storage/manager.go`) with ready channels for phase synchronization and blocking accessors that wait for backend readiness.

## Data Flow

### Session Persistence (Hybrid)

```
Message → JSONL file (primary, crash-safe append)
       → SQLite dual-writer (best-effort, for analytics/search)
       → Bleve indexer (best-effort, for full-text search)
```

- JSONL is authoritative; SQLite/Bleve are derived indexes
- JSONL rotation at 256KB, max 3 rotated files
- Background indexer catches up existing JSONL sessions into SQLite on startup

### Memory Persistence (File + Search)

```
Save → Markdown file with YAML frontmatter (primary)
     → Bleve index (keyword search)
     → chromem-go (semantic search)

Recall → Vector search (semantic hits)
       → Bleve search (keyword hits)
       → Fallback: simple keyword matching
       → Deduplicate → Decay score → Scope boost → Rank
```

### Config Persistence

```
Load → JSON files (3-tier: user > project > local)
     → Merge non-zero fields
     → Cache in bbolt with SHA256 fingerprint for staleness detection
```

### Permission Persistence

```
Policy + approval history stored in bbolt KV
Survives process restarts so users don't re-approve tools
```

## Background Services

| Service | Backend | Purpose |
|---|---|---|
| Eviction goroutine | SQLite | Deletes expired `prompt_cache` rows every 5 minutes |
| Code indexer | Bleve | Indexes source files into full-text search |
| Code embedder | chromem-go | Embeds code chunks (~2KB) for semantic search |
| Session indexer | SQLite + Bleve | Indexes JSONL sessions into SQL and search |
| Memory embedder | chromem-go | Embeds memory entries for semantic recall |
| Doc embedder | chromem-go | Embeds documentation files |

## File Layout

```
~/.ycode/projects/<project-hash>/
├── data/
│   ├── ycode.kv              # bbolt KV store
│   ├── ycode.db              # SQLite database
│   ├── vectors/              # chromem-go persistent vectors
│   └── *.bleve/              # Bleve search indexes
├── memory/                   # Markdown memory files
│   └── MEMORY.md             # Memory index
└── sessions/
    └── <session-id>/
        └── messages.jsonl    # Session transcript
```

## Dependencies

| Library | License | Purpose | Binary Impact |
|---|---|---|---|
| `go.etcd.io/bbolt` | MIT | KV cache, metadata | ~200KB |
| `modernc.org/sqlite` | Unlicense | Structured data | ~30MB |
| `github.com/blevesearch/bleve/v2` | Apache-2.0 | Full-text search | ~15MB |
| `github.com/philippgille/chromem-go` | MIT | Vector similarity | ~50KB |

All pure Go, no CGO required. Permissive licenses only.

## What's Explicitly Not Included

| Technology | Reason |
|---|---|
| PostgreSQL/MySQL | External service -- not suitable for CLI tool |
| Redis | External service -- bbolt covers KV needs |
| LanceDB | No pure Go implementation |
| S3/R2/GCS | Local-first design; cloud sync deferred |
| OS Keyring | Platform-specific CGO dependencies |

## Implementation Status

| Phase | Status | Description |
|---|---|---|
| Phase 1: Core Storage | **DONE** | All 4 backends implemented with tests |
| Phase 2: Integration | **DONE** | Wired into main.go, dual-write, caching |
| Phase 3: Search Integration | **DONE** | Bleve wired into grep, toolsearch, memory, sessions |
| Phase 4: Vector Integration | **DONE** | Embedder, semantic search, memory recall |
| Phase 5: Optimization | **DONE** | Benchmarks, connection tuning, compaction, build tags |

See [todo-persistence.md](./todo-persistence.md) for the detailed checklist.
