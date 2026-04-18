# Persistence Technologies in Prior Art AI Agent Tools

A comprehensive analysis of storage and persistence technologies used across all projects in `priorart/`. This document catalogs every key-value store, SQL/NoSQL database, vector database, full-text search engine, object/blob store, in-memory cache, and file-based persistence mechanism found in each tool.

---

## Table of Contents

1. [Summary Matrix](#summary-matrix)
2. [Aider](#aider) (Python)
3. [Clawcode](#clawcode) (Rust)
4. [Cline](#cline) (TypeScript / VS Code)
5. [Codex](#codex) (Rust)
6. [Continue](#continue) (TypeScript / VS Code)
7. [Gemini CLI](#gemini-cli) (TypeScript / Node.js)
8. [OpenClaw](#openclaw) (TypeScript / Node.js)
9. [OpenCode](#opencode) (TypeScript / Go)
10. [OpenHands](#openhands) (Python)
11. [Cross-Project Patterns](#cross-project-patterns)

---

## Summary Matrix

| Technology | Aider | Clawcode | Cline | Codex | Continue | Gemini CLI | OpenClaw | OpenCode | OpenHands |
|---|---|---|---|---|---|---|---|---|---|
| **SQLite** | via diskcache | - | better-sqlite3 | sqlx | sqlite3 | - | node:sqlite | Drizzle+Bun | SQLAlchemy |
| **PostgreSQL/MySQL** | - | - | - | - | - | - | - | MySQL (cloud) | PostgreSQL |
| **Redis** | - | - | - | - | - | - | - | - | redis |
| **Vector DB** | LlamaIndex | - | - | - | LanceDB | - | LanceDB + sqlite-vec | - | - |
| **FTS** | - | - | - | - | SQLite FTS5 | - | - | - | - |
| **Object/Blob (S3/R2/GCS)** | - | - | S3/R2 | - | - | - | - | S3/R2 | S3/GCS |
| **Key-Value (disk)** | diskcache | - | - | - | - | - | - | - | - |
| **In-Memory Cache** | dict | HashMap | Map | LRU | quick-lru, lru-cache | Map | ExpiringMapCache | Map, LRU | dict |
| **JSON files** | Y | Y | Y | Y | Y | Y | Y | Y | Y |
| **JSONL files** | - | Y | - | Y | Y | Y | Y | - | Y |
| **YAML/TOML config** | YAML | - | - | TOML | YAML | TOML/YAML | - | JSONC | TOML |
| **Markdown memory** | - | Y | Y | - | - | Y | - | - | - |
| **System Keyring** | - | - | - | keyring | - | @github/keytar | - | - | - |
| **Encrypted secrets** | - | - | file 0o600 | age encryption | - | AES-256-GCM | file 0o600 | - | JWE tokens |
| **Browser localStorage** | - | - | - | - | redux-persist | - | - | localStorage | - |
| **VS Code state API** | - | - | globalState | - | globalState | - | - | - | - |

---

## Aider

**Language:** Python | **Type:** CLI

### SQLite (via diskcache)

- **Package:** `diskcache==5.6.3` (SQLite-backed key-value store)
- **Data:** Repository code tags/symbols cache (file mtimes, extracted function/class tags)
- **Location:** `.aider.tags.cache.v{VERSION}/` in project root
- **Pattern:** `self.TAGS_CACHE[cache_key]` get/set; falls back to in-memory dict if SQLite unavailable
- **Component:** `aider/repomap.py` (`RepoMap.get_tags()`)

### Vector Database (LlamaIndex)

- **Package:** LlamaIndex with `HuggingFaceEmbedding` (`BAAI/bge-small-en-v1.5`)
- **Data:** Documentation/help text embeddings for semantic QA
- **Location:** `~/.aider/caches/help.{VERSION}/`
- **Pattern:** `VectorStoreIndex` with `StorageContext` persistence; retrieval via `similarity_top_k=20`
- **Component:** `aider/help.py` (`Help.ask()`)

### JSON File Storage

| File | Location | Data | TTL |
|---|---|---|---|
| `model_prices_and_context_window.json` | `~/.aider/caches/` | Model metadata, pricing, context windows | 24h |
| `openrouter_models.json` | `~/.aider/caches/` | OpenRouter model list | 24h |
| `analytics.json` | `~/.aider/` | User UUID, telemetry opt-in | - |
| `installs.json` | `~/.aider/` | Installation records | - |

### Text/History Files

- **Chat history:** Markdown-formatted conversation transcript (append-only)
- **LLM history:** Debug log of API interactions (append-only)
- **Input history:** `prompt_toolkit.history.FileHistory` for readline-like recall
- **Component:** `aider/io.py` (`InputOutput`)

### In-Memory Caches

- `tree_cache`, `tree_context_cache`, `map_cache` in `RepoMap` (dicts, session-scoped)
- `cur_messages`, `done_messages` in `base_coder.py` (conversation buffers)
- `local_model_metadata` in `models.py`

### Other

- **YAML config:** `model-settings.yml` (81KB bundled model configs) via `PyYAML`
- **OAuth keys:** `~/.aider/oauth-keys.env` via `python-dotenv`
- **Version check:** `~/.aider/caches/versioncheck` (touch-based timestamp)
- **Git:** `GitPython==3.1.46` for repository operations

---

## Clawcode

**Language:** Rust | **Type:** CLI

### File-Based Persistence (Primary -- No Databases)

Clawcode uses an entirely file-based architecture with no database dependencies.

#### JSONL Session Storage

- **Data:** Conversation messages (user, assistant, tool use, tool results), token usage, compaction summaries, fork metadata
- **Location:** `.claw/sessions/<workspace_hash>/<session_id>.jsonl`
- **Features:** Atomic writes (temp file + rename), incremental append, automatic rotation (256KB threshold, max 3 rotated files)
- **Component:** `runtime::session::Session` (`session.rs`)

#### JSON Configuration

| File | Scope | Data |
|---|---|---|
| `~/.claw/settings.json` | User | Plugins, MCP servers, hooks, OAuth, aliases, permissions, sandbox |
| `.claw.json` or `.claw/settings.json` | Project | Project-specific overrides |
| `.claw/settings.local.json` | Machine | Machine-local overrides |
| `~/.claw/credentials.json` | User | OAuth access/refresh tokens, expiration |

#### Prompt Cache

- **Location:** `~/.claude/cache/prompt-cache/<session_id>/`
- **Files:** `session-state.json`, `stats.json`, `completions/<request_hash>.json`
- **TTL:** 30s completions, 5m prompts
- **Pattern:** FNV-1a request fingerprinting, atomic writes
- **Component:** `api::prompt_cache::PromptCache`

### In-Memory Storage

- `Session::messages` (Vec<ConversationMessage>)
- `PromptCache::inner` (Arc<Mutex>)
- `RuntimeConfig::merged` (BTreeMap)

---

## Cline

**Language:** TypeScript | **Type:** VS Code Extension (also CLI, JetBrains)

### SQLite (better-sqlite3)

- **Package:** `better-sqlite3` v12.4.1
- **Data:** Multi-instance lock management (file locks, instance registry, folder locks)
- **Database:** `~/.cline/locks.db`
- **Schema:** `locks` table with indexes on `held_by`, `type`, `target`
- **Component:** `src/core/locks/SqliteLockManager.ts`

### File-Based JSON Storage

#### ClineFileStorage (Atomic Writes)

| File | Data | Security |
|---|---|---|
| `~/.cline/data/globalState.json` | Global settings, API keys, preferences | Standard |
| `~/.cline/data/secrets.json` | API keys | Mode 0o600 |
| `~/.cline/data/workspaces/<hash>/workspaceState.json` | Per-workspace state | Standard |
| `~/.cline/data/taskHistory.json` | Task history array | Standard |

- **Pattern:** Atomic writes via temp file + rename, batch `setBatch()` for multi-key writes
- **Component:** `src/shared/storage/ClineFileStorage.ts`

#### Per-Task Storage

- **Location:** `~/.cline/data/tasks/<taskId>/`
- **Files:** `api_conversation_history.json`, `ui_messages.json`, `context_history.json`, `task_metadata.json`, `settings.json`

### Cloud Blob Storage (S3/R2)

- **Library:** `aws4fetch` (AWS Signature V4)
- **Providers:** AWS S3, Cloudflare R2
- **Sync:** Async queue-based syncing via `~/.cline/data/cache/sync-queue.json`
- **Component:** `src/shared/storage/ClineBlobStorage.ts`

### In-Memory State (StateManager)

- **Architecture:** Multi-tier hierarchy: globalState > taskState > sessionOverride > remoteConfig > secrets > workspaceState
- **TTL Cache:** Model info cache (60-minute TTL) for dynamic providers
- **Model caches:** `cline_recommended_models.json`, `openrouter_models.json`, etc. in `~/.cline/data/cache/` (1h TTL)

### Other

- **Protobuf:** Type-safe cross-platform serialization (`proto/cline/`)
- **VS Code APIs:** `globalState` for extension flags (abstracted behind `StorageContext`)
- **Telemetry:** PostHog (`posthog-node`) with machine ID persistence
- **Checkpoints:** Git worktrees and file snapshots

---

## Codex

**Language:** Rust | **Type:** CLI

### SQLite (sqlx)

- **Package:** `sqlx` v0.8.6 with SQLite backend
- **Two databases:**
  - **State DB** (`$CODEX_HOME/state`): threads, stage1_outputs, jobs, agent_jobs, remote_control_enrollments
  - **Logs DB** (`$CODEX_HOME/logs`): structured trace logs with batch insertion (queue 512, batch 128, flush 2s)
- **PRAGMA:** WAL mode, synchronous NORMAL
- **Migrations:** Versioned SQL migrations (version 5)
- **Component:** `codex-rs/state/`

### System Keyring + Encrypted Secrets

- **Keyring:** `keyring` v3.6 (macOS Keychain, Windows Credential Manager, Linux DBus Secret Service)
- **Fallback:** `$CODEX_HOME/.credentials.json` (JSON BTreeMap with OAuth tokens)
- **Encrypted secrets:** `$CODEX_HOME/local.age` using `age` v0.11.1 (AES-256 scrypt)
- **Data:** OAuth tokens (access, refresh, expiration, scopes), global/env-scoped secrets
- **Modes:** Auto (keyring + file fallback), Keyring-only, File-only

### JSONL Session History

- **Location:** `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-{timestamp}-{uuid}.jsonl`
- **Index:** `$CODEX_HOME/session_index.jsonl` (append-only)
- **Data:** Conversation turns, tool calls, session metadata, memory extraction outputs
- **Pattern:** Async writer with mpsc channel, supports resume/fork
- **Component:** `codex-rs/rollout/`

### In-Memory Caches

- **LRU cache:** `lru` v0.16.3 with `BlockingLruCache<K,V>` (tokio Mutex wrapper)
- **Thread-safe stores:** `ApprovalStore`, `ProcessStore`, plugin caches (Arc<Mutex/RwLock>)

### Configuration

- **Format:** TOML (`$CODEX_HOME/config.toml`)
- **Libraries:** `toml` v0.9.5, `toml_edit` v0.24.0
- **Data:** Model selection, sandbox policies, shell env, plugin configs, network permissions

---

## Continue

**Language:** TypeScript | **Type:** VS Code/JetBrains Extension

### SQLite (sqlite3 + Drizzle)

- **Package:** `sqlite3` v5.1.7 + `sqlite` wrapper
- **Database:** `~/.continue/index/index.sqlite`
- **Tables:**
  - `tag_catalog` -- codebase index metadata (dir, branch, path, cacheKey)
  - `code_snippets` + `code_snippets_tags` -- parsed code functions/signatures
  - `fts` + `fts_metadata` -- FTS5 virtual tables with trigram tokenization
  - `lance_db_cache` -- embeddings metadata
  - `chunks` -- code chunks from indexing
  - `cache` -- tab autocomplete cache (separate `autocompleteCache.sqlite`)
  - `tokens_generated` -- token usage per model/provider (in `devdata.sqlite`)
- **PRAGMA:** WAL, busy_timeout=3000

### LanceDB (Vector Database)

- **Package:** `vectordb` v0.4.20
- **Data:** Code and documentation embeddings with metadata
- **Location:** `~/.continue/index/lancedb/`
- **Pattern:** Dynamic table creation per branch/tag, vector search with distance ranking
- **Component:** `core/indexing/LanceDbIndex.ts`, `core/indexing/docs/DocsService.ts`

### Full-Text Search (SQLite FTS5)

- **Engine:** SQLite FTS5 with trigram tokenizer
- **Table:** `fts` in `index.sqlite`
- **Features:** BM25 ranking, substring matching via trigrams

### In-Memory Caches

- **quick-lru** v7.0.0: Open files (max 20), previous edits (max 5)
- **lru-cache** v11.0.2: AST autocomplete snippets (max 100)
- **GitDiffCache:** Git diffs with 60s TTL
- **MiniSearch** v7.0.0: Client-side file search with fuzzy/prefix matching

### Browser/GUI State

- **redux-persist** with localStorage backend
- **Data:** Session state, UI settings, edit mode, tabs, profile preferences

### File-Based Storage

| Path | Format | Data |
|---|---|---|
| `~/.continue/config.yaml` or `config.json` | YAML/JSON | Models, providers, tools |
| `~/.continue/sessions/{id}.json` | JSON | Chat sessions with history |
| `~/.continue/index/globalContext.json` | JSON | Model selections, indexing state, MCP OAuth |
| `~/.continue/dev_data/{schema}/*.jsonl` | JSONL | Telemetry event logs |
| `~/.continue/logs/core.log` | Text | System logs |
| `~/.continue/.migrations/` | Marker files | Completed migration tracking |

---

## Gemini CLI

**Language:** TypeScript / Node.js | **Type:** CLI

### File-Based Storage (No Databases)

Gemini CLI uses an entirely file-based architecture with no database dependencies.

#### JSON Storage

| File | Location | Data |
|---|---|---|
| `settings.json` | `~/.gemini/` | Global settings |
| `projects.json` | `~/.gemini/` | Project registry (project-to-short-ID map) |
| `state.json` | `~/.gemini/` | Persistent UI state (banner counts, tips shown) |
| `compression_state.json` | Per-project temp | File compression metadata, SHA256 hashes |
| `{id}.json` | Per-project tasks/ | Task tracker data |

#### JSONL Chat Recording

- **Location:** `~/.gemini/tmp/{projectId}/chats/session-{sessionId}.jsonl`
- **Data:** Chat history, session summaries
- **Component:** `packages/core/src/services/chatRecordingService.ts`

#### Markdown Memory System

- **Files:** `GEMINI.md` (global, project, user-project, extension levels)
- **Hierarchical discovery** with configurable filename
- **Lock-based** concurrent access (`proper-lockfile`)
- **Component:** `packages/core/src/tools/memoryTool.ts`, `packages/core/src/utils/memoryDiscovery.ts`

### Encrypted Credentials

- **Primary:** System keyring via `@github/keytar`
- **Fallback:** `~/.gemini/gemini-credentials.json` encrypted with AES-256-GCM (Node.js `crypto`)
- **MCP tokens:** `~/.gemini/mcp-oauth-tokens.json`

### In-Memory Caches

- `CacheService` with TTL support (`packages/core/src/utils/cache.ts`)
- File search result cache, directory crawl cache with auto-expiration

### Configuration

- **TOML:** Skill definitions, policy files, command/agent definitions in `.gemini/`
- **YAML:** Config files, PR review bot config (`js-yaml`)
- **Checkpoints:** File snapshots with timestamps in project temp directory

---

## OpenClaw

**Language:** TypeScript / Node.js | **Type:** CLI + Multi-Platform Agent

### SQLite (node:sqlite)

- **Package:** `node:sqlite` (Node 22+ built-in, experimental)
- **Data:** Task registry (task runs, status, timestamps), task delivery states, parent/child relationships
- **PRAGMA:** WAL mode, synchronous NORMAL, busy_timeout 5000ms
- **Migrations:** Automatic schema evolution with prepared statements
- **Component:** `src/tasks/task-registry.store.sqlite.ts`

### sqlite-vec (Vector Extension)

- **Package:** `sqlite-vec` v0.1.9 (loadable SQLite extension)
- **Data:** Vector embeddings for memory systems
- **Features:** L2 distance-based similarity search, lazy-loaded
- **Component:** `src/memory-host-sdk/host/sqlite-vec.ts`

### LanceDB (Vector Database)

- **Package:** `@lancedb/lancedb` v0.27.2
- **Data:** Long-term memory entries with embeddings, importance scores, categories
- **Embedding providers:** OpenAI, Ollama, Mistral, Gemini, Bedrock
- **Features:** Auto-recall/capture hooks, configurable vector dimensions
- **Component:** `extensions/memory-lancedb/`

### File-Based JSON/JSONL Storage

#### Atomic JSON File Writes

- **Security:** UUID-based temp files, symlink detection, mode 0o600 for files, 0o700 for dirs, directory fsync
- **Component:** `src/infra/json-file.ts`

| Store | Location | Data |
|---|---|---|
| Sessions | `~/.openclaw/agents/<agentId>/sessions/*.jsonl` | Agent conversation transcripts |
| Config | `~/.openclaw/config.json` | Global configuration |
| Auth | `~/.openclaw/auth/<provider>.json` | OAuth credentials per provider |
| Cron | Cron store | Scheduled job definitions and status |
| Delivery queue | `~/.openclaw/<state-dir>/delivery-queue/` | Per-message JSON + delivered markers |
| Media | `~/.openclaw/<state-dir>/media/` | Downloaded media files (max 5MB, 2min TTL) |
| Device auth | Device auth store | Device tokens and identifiers |

### In-Memory Caches

- **ExpiringMapCache:** TTL-based generic cache (45s default for sessions)
- **JavaScript Map/Set:** Plugin registrations, command reservations, active connections

### Other

- **APNS:** Push notification device token storage per node
- **Secret file references:** External credential indirection (max 16KB, symlink-safe)
- **Execution approvals:** Safe binary whitelisting store

---

## OpenCode

**Language:** TypeScript | **Type:** CLI + Desktop + Web

### SQLite (Drizzle ORM)

- **Package:** Drizzle ORM with Bun SQLite / Node.js SQLite
- **Database:** `~/.local/share/opencode/opencode.db`
- **Tables:** projects, sessions, messages, parts, todos, permissions, accounts, session_shares, events, workspaces
- **PRAGMA:** WAL mode, synchronous NORMAL, busy_timeout 5s, cache 64MB, foreign_keys ON
- **Migrations:** Drizzle ORM powered schema versioning
- **Component:** `packages/opencode/src/storage/`

### MySQL (Cloud/Console)

- **Package:** Drizzle ORM with PlanetScale serverless MySQL
- **Data:** Workspaces, users, billing, subscriptions, payments, usage, authentication, API keys, providers, models, benchmarks
- **Component:** `packages/console/core/`

### Object/Blob Storage (S3/R2)

- **Library:** `aws4fetch`
- **Backends:** AWS S3, Cloudflare R2
- **Data:** Enterprise session/project data as JSON files
- **Component:** `packages/enterprise/src/core/storage.ts`

### Browser/Desktop Storage

- **localStorage:** Dual-layer with in-memory LRU cache (500 entries / 8MB max)
- **Tauri Store:** `@tauri-apps/plugin-store` for desktop persistence
- **Key prefixes:** `opencode.global.dat`, `opencode.workspace.{prefix}.{hash}.dat`

### File-Based Storage

| Path | Format | Data |
|---|---|---|
| `~/.local/share/opencode/storage/` | JSON | Projects, sessions, messages, diffs |
| `~/.local/share/opencode/state/kv.json` | JSON | TUI key-value state |
| `~/.config/opencode/` | JSONC | Configuration |
| `~/.cache/opencode/packages/` | Files | NPM package cache with file-locking |

### In-Memory Caches

- JavaScript `Map` for session instances, sync registry, share queues, VCS items, session status

### XDG Base Directory Layout

```
~/.local/share/opencode/     # Data (DB, storage, logs)
~/.cache/opencode/           # Cache (packages, bin)
~/.config/opencode/          # Config
~/.local/state/opencode/     # State
```

---

## OpenHands

**Language:** Python | **Type:** Web Platform + CLI

### PostgreSQL / SQLite (SQLAlchemy)

- **Package:** SQLAlchemy (async) + `asyncpg` / `pg8000` / Google Cloud SQL connector
- **Data:** Users, organizations, API keys, conversations, events, callbacks, integrations (Jira, Linear, Slack, GitHub, GitLab), billing (Stripe), telemetry, maintenance tasks
- **Enterprise:** 100+ SQLAlchemy model tables with 100+ Alembic migration versions
- **Pool:** pool_size=25, max_overflow=10, pool_recycle=1800s, pre_ping=True
- **Component:** `openhands/app_server/`, `enterprise/storage/`

### Redis

- **Package:** `redis` >= 5.2
- **Data:** Rate limiting, session state clustering, conversation manager state
- **Config:** `REDIS_HOST`, `REDIS_PORT`, `REDIS_PASSWORD`, `REDIS_DB`
- **Component:** `enterprise/storage/redis.py`, `enterprise/server/`

### Cloud Object Storage

| Provider | Library | Data |
|---|---|---|
| AWS S3 | `boto3` | Conversation events, metadata, backups |
| Google Cloud Storage | `google-cloud-storage` | Conversation events, metadata |

### File Store Implementations

| Store | Type | Features |
|---|---|---|
| `LocalFileStore` | Filesystem | Atomic writes (temp + rename), fsync, thread-safe |
| `InMemoryFileStore` | Memory | Dict-based, for testing |
| `S3FileStore` | S3 | Retry logic, bucket operations |
| `GoogleCloudFileStore` | GCS | Google Cloud bucket operations |
| `WebHookFileStore` | Decorator | HTTP POST/DELETE callbacks on file changes (3 retries) |
| `BatchedWebHookFileStore` | Decorator | Batched webhooks (5s timeout, 1MB limit) |

### Event Stream Storage

- **Format:** JSON files per event (JSONL-like)
- **Features:** Cache pages (default 25), subscriber callbacks (7 subscriber types), threading with background processing
- **Component:** `openhands/events/event_store.py`, `openhands/events/stream.py`

### Secrets/Encryption

- **Method:** JWE token encryption via `StoredSecretStr` SQLAlchemy TypeDecorator
- **Pattern:** Secrets encrypted before DB storage, context-aware serialization

### Configuration

- **Format:** TOML with `OH_` environment variable prefix
- **Persistence dir:** `~/.openhands` (default)
- **File store types:** local, s3, gcp, memory (configurable)

---

## Cross-Project Patterns

### Universal Patterns

1. **JSON/JSONL files** -- Every project uses JSON for configuration and/or session data
2. **In-memory caches** -- All projects maintain runtime caches (Maps, dicts, HashMaps)
3. **Atomic file writes** -- Most projects use temp-file-then-rename for crash safety

### Database Adoption

| Approach | Projects |
|---|---|
| No database (file-only) | Clawcode, Gemini CLI |
| SQLite only | Aider (via diskcache), Cline, Codex, Continue, OpenClaw, OpenCode (local) |
| PostgreSQL/MySQL (cloud) | OpenCode (console), OpenHands |
| Redis (caching) | OpenHands |

### Vector/Embedding Storage

| Technology | Projects |
|---|---|
| LanceDB | Continue, OpenClaw |
| LlamaIndex (in-memory + file persist) | Aider |
| sqlite-vec | OpenClaw |
| None | Clawcode, Cline, Codex, Gemini CLI, OpenCode, OpenHands |

### Full-Text Search

| Technology | Projects |
|---|---|
| SQLite FTS5 | Continue |
| MiniSearch (in-memory) | Continue |
| None (grep/ripgrep instead) | All others |

### Secret/Credential Storage

| Approach | Projects |
|---|---|
| OS Keyring | Codex (keyring crate), Gemini CLI (@github/keytar) |
| Encrypted file | Codex (age), Gemini CLI (AES-256-GCM), OpenHands (JWE) |
| Restricted file perms (0o600) | Cline, OpenClaw |
| Plaintext JSON | Aider, Clawcode |

### Cloud Storage

| Provider | Projects |
|---|---|
| AWS S3 | Cline, OpenCode, OpenHands |
| Cloudflare R2 | Cline, OpenCode |
| Google Cloud Storage | OpenHands |
| None (local only) | Aider, Clawcode, Codex, Continue, Gemini CLI, OpenClaw |

### Session Storage Format

| Format | Projects |
|---|---|
| JSONL (append-only) | Clawcode, Codex, Continue (dev_data), Gemini CLI, OpenClaw, OpenHands |
| JSON files | Aider, Cline, Continue (sessions), OpenCode |
| SQLite rows | Continue (index), OpenCode, OpenHands |

### Configuration Format

| Format | Projects |
|---|---|
| JSON | Aider, Cline, Clawcode, Continue, OpenClaw, OpenCode |
| YAML | Aider, Continue, Gemini CLI |
| TOML | Codex, Gemini CLI, OpenHands |
| JSONC | OpenCode |

---

## Key Takeaways

1. **SQLite dominates** as the embedded database of choice (7 of 9 projects), with WAL mode universally enabled for concurrency
2. **File-based persistence remains the foundation** -- even projects with databases use JSON/JSONL files extensively for sessions, config, and state
3. **Vector databases are emerging** but not yet universal -- only 3 projects (Continue, OpenClaw, Aider) use dedicated vector storage
4. **Full-text search is underutilized** -- only Continue implements FTS5; most tools rely on ripgrep/grep for code search
5. **Cloud storage is optional** -- most tools are designed for local-first operation with cloud as an enterprise add-on
6. **Credential security varies widely** -- from plaintext JSON (Clawcode) to OS keyring + age encryption (Codex)
7. **No project uses NoSQL databases** (MongoDB, CouchDB, etc.) -- the choice is consistently between SQLite (embedded) and PostgreSQL/MySQL (cloud)
8. **Redis appears only once** (OpenHands) for rate limiting and distributed state -- CLI tools avoid external service dependencies

---

## Recommendations for ycode

All recommendations below are **pure Go** (no CGO) with **permissive licenses** (MIT, Apache-2.0, BSD, Unlicense). The design follows a progressive enhancement strategy: file-based storage at startup, graduating to SQLite/Bleve/vector as usage grows.

### Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                    ycode persistence                     │
├──────────┬──────────┬───────────┬───────────┬───────────┤
│  Config  │ Sessions │ Structured│  Vectors  │    FTS    │
│  & State │ & History│   Data    │           │           │
├──────────┼──────────┼───────────┼───────────┼───────────┤
│ JSON/JSONL│  JSONL   │  SQLite   │ chromem-go│   Bleve   │
│  (files) │ (files)  │ (modernc) │ or SQLite │   v2      │
│          │          │           │  +HNSW    │           │
├──────────┴──────────┴───────────┴───────────┴───────────┤
│              bbolt (KV cache / metadata)                 │
├─────────────────────────────────────────────────────────┤
│                 OS filesystem (foundation)                │
└─────────────────────────────────────────────────────────┘
```

### 1. SQLite -- `modernc.org/sqlite`

| | |
|---|---|
| **Module** | `modernc.org/sqlite` |
| **License** | Unlicense (public domain equivalent) |
| **Pure Go** | Yes -- C SQLite transpiled to Go via ccgo/v4 |
| **Maturity** | Production-ready, actively maintained |

**Why modernc over ncruces:** Both are excellent. modernc has broader adoption in the Go ecosystem, a simpler dependency tree, and slightly better write performance. ncruces (MIT, WASM-based) is a strong alternative if MIT license is preferred or if VFS features are needed.

**What to store in SQLite:**

| Table | Data | Rationale |
|---|---|---|
| `sessions` | Session metadata (id, title, model, timestamps, token usage) | Structured queries, sorting, filtering |
| `messages` | Conversation messages (role, content, tool calls, timestamps) | Indexed retrieval, compaction queries |
| `tasks` | Task state (status, progress, parent/child) | Status tracking, dependency queries |
| `permissions` | Permission rules and approval history | Fast lookup during tool dispatch |
| `tool_usage` | Tool call metrics (name, duration, success/fail) | Analytics, optimization |
| `prompt_cache` | Prompt fingerprints and cached responses | TTL-based eviction queries |

**PRAGMA settings** (consistent with prior art best practices):

```sql
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -64000;  -- 64MB
PRAGMA foreign_keys = ON;
```

**Migration strategy:** Keep current JSONL sessions as the write format. SQLite indexes session metadata for fast listing/search. Messages are written to JSONL first (crash-safe append), then indexed into SQLite asynchronously. This gives us the best of both: JSONL durability and SQLite queryability.

### 2. Vector Database -- `chromem-go`

| | |
|---|---|
| **Module** | `github.com/philippgille/chromem-go` |
| **License** | MIT |
| **Pure Go** | Yes -- zero third-party runtime dependencies |
| **Maturity** | Pre-v1.0 but functional; Chroma-compatible API |

**Why chromem-go:**
- Zero external dependencies -- embeds directly into ycode binary
- Chroma-like API (familiar to anyone who's used ChromaDB)
- Persistent storage to disk (GZIP-compressed GOBS)
- 0.3ms query latency for 1K documents, ~40ms for 100K
- Cosine similarity, Euclidean distance, dot product
- Document-level metadata filtering

**Alternative considered:** `github.com/chand1012/vectorgo` (SQLite-backed vectors) is worth watching -- it would unify vector storage with our SQLite layer. However chromem-go is more mature and better documented.

**What to store as vectors:**

| Collection | Embeddings For | Use Case |
|---|---|---|
| `codebase` | Code chunks (functions, classes, files) | Semantic code search ("find auth middleware") |
| `memory` | Persistent memory entries (user, feedback, project) | Memory recall by semantic similarity |
| `sessions` | Session summaries and compaction outputs | "Find the conversation where we discussed X" |
| `docs` | Documentation chunks (CLAUDE.md, README, etc.) | Context-aware doc retrieval |

**Embedding model:** Use a small local model (e.g., `all-MiniLM-L6-v2` via a Go inference lib) or delegate to the configured LLM provider's embedding endpoint. chromem-go supports custom embedding functions.

**Progressive enhancement:** Vector indexing is opt-in and runs in the background. The tool works without it (grep/glob remain primary), but as the codebase and memory grow, vector search becomes the preferred retrieval path.

### 3. Full-Text Search -- `bleve/v2`

| | |
|---|---|
| **Module** | `github.com/blevesearch/bleve/v2` |
| **License** | Apache-2.0 |
| **Pure Go** | Yes |
| **Maturity** | Production-ready, used by Couchbase, actively maintained |

**Why Bleve over SQLite FTS5:**
- Richer query syntax (fuzzy, wildcard, phrase, boolean, regex)
- BM25 scoring with query-time boosting
- Built-in text analyzers for 15+ languages
- Faceted search for filtering by file type, directory, etc.
- Highlighting of matching terms in results
- Independent index lifecycle (rebuild without touching SQLite)

SQLite FTS5 is simpler but limited: no fuzzy matching, no language-aware stemming beyond basic tokenization, and tightly coupled to the database schema. Bleve gives us a proper search engine without external services.

**What to index in Bleve:**

| Index | Documents | Fields |
|---|---|---|
| `code` | Source files (chunked) | path, content, language, last_modified |
| `memory` | Memory entries | name, type, content, description |
| `sessions` | Session messages | role, content, tool_name, timestamp |
| `docs` | Project docs | path, title, content, section |

**Index location:** `~/.ycode/projects/<project-hash>/index/bleve/`

**Integration with tools:**
- `Grep` tool: Falls back to ripgrep for simple patterns, promotes to Bleve for natural-language queries or when result count is high
- `ToolSearch` / deferred tool discovery: Bleve indexes tool descriptions for semantic matching
- Memory recall: Bleve provides keyword search complementing chromem-go's vector similarity

### 4. Key-Value Store -- `bbolt`

| | |
|---|---|
| **Module** | `go.etcd.io/bbolt` |
| **License** | MIT |
| **Pure Go** | Yes |
| **Maturity** | Production-ready, maintained by etcd team |

**Why bbolt over BadgerDB/Pebble/NutsDB:**

| Criterion | bbolt | BadgerDB | Pebble | NutsDB |
|---|---|---|---|---|
| License | MIT | Apache-2.0 | BSD | Apache-2.0 |
| ACID transactions | Full serializable | Yes | Yes | Yes |
| Read performance | Excellent | Slower | Best | Good |
| Write performance | Good | Best | Good | Good |
| Complexity | Simple (single file) | Complex (LSM+vlog) | Complex | Medium |
| Dependency weight | Minimal | Heavy | Heavy | Medium |
| Best for | Read-heavy, simple | Write-heavy | Balanced high-perf | Data structures |

bbolt wins for ycode because:
- **Read-heavy workload** -- tool dispatch, permission checks, config lookups are reads; writes are infrequent
- **Single-file storage** -- no compaction, no WAL cleanup, no garbage collection
- **Minimal dependencies** -- etcd team maintains it with a tiny footprint
- **Bucket organization** -- natural fit for namespaced data (per-project, per-session)

**What to store in bbolt:**

| Bucket | Data | Pattern |
|---|---|---|
| `config_cache` | Merged configuration (3-tier) | Read on every tool call |
| `tool_registry` | Registered tool metadata | Read on dispatch, write on plugin load |
| `permission_rules` | Active permission policies | Read on every tool call |
| `prompt_fragments` | Cached prompt sections | Read on every API call, TTL-based write |
| `mcp_state` | MCP server connection state | Read/write on MCP operations |
| `file_metadata` | File hashes, mtimes for change detection | Read-heavy, batch write on scan |

**When NOT to use bbolt:** For anything that needs SQL queries (joins, aggregations, complex filtering) -- use SQLite instead. bbolt is for fast key-based lookups, not relational queries.

### 5. File-Based Storage (Retained)

Keep the existing file-based approach for initial bootstrap and portable data:

| Data | Format | Location | Rationale |
|---|---|---|---|
| Configuration | JSON | `~/.ycode/settings.json`, `.agents/ycode/settings.json` | Human-editable, mergeable |
| Sessions | JSONL | `.agents/ycode/sessions/<id>.jsonl` | Append-only durability, interop with clawcode |
| Memory entries | Markdown | `~/.ycode/projects/<hash>/memory/` | Human-readable, git-friendly |
| Memory index | Markdown | `MEMORY.md` | Quick scan without DB |
| Instruction files | Markdown | `CLAUDE.md` ancestry | Standard convention |
| Credentials | JSON (0o600) | `~/.ycode/credentials.json` | Simple, restricted perms |

### 6. Storage Initialization Strategy

ycode should start fast and initialize storage progressively:

```
Phase 1 (immediate, <50ms):
  - Read JSON config files
  - Read CLAUDE.md / MEMORY.md
  - Open bbolt for config/permission cache
  - Ready for first prompt

Phase 2 (background, first 1-2s):
  - Open/create SQLite database
  - Run migrations if needed
  - Index existing sessions into SQLite

Phase 3 (background, lazy):
  - Initialize Bleve index (create or open)
  - Index codebase if not yet indexed
  - Initialize chromem-go vector store
  - Compute/update embeddings for memory and code
```

This ensures the tool feels instant while building search capabilities in the background.

### 7. Dependency Summary

| Library | Module | License | Purpose | Size Impact |
|---|---|---|---|---|
| modernc.org/sqlite | `modernc.org/sqlite` | Unlicense | Structured data, metadata | ~30MB (transpiled C) |
| chromem-go | `github.com/philippgille/chromem-go` | MIT | Vector similarity search | ~50KB |
| Bleve v2 | `github.com/blevesearch/bleve/v2` | Apache-2.0 | Full-text search | ~15MB |
| bbolt | `go.etcd.io/bbolt` | MIT | KV cache, fast lookups | ~200KB |

Total additional binary size: ~45MB (dominated by SQLite transpilation and Bleve analyzers). This is acceptable for a CLI tool that already bundles significant functionality.

### 8. What We Explicitly Skip

| Technology | Why Skip |
|---|---|
| PostgreSQL/MySQL | External service dependency -- not suitable for CLI tool |
| Redis | External service dependency -- bbolt covers our KV needs |
| LanceDB | No pure Go implementation; chromem-go serves the same purpose |
| MongoDB/CouchDB | External services, overkill for embedded use |
| S3/R2/GCS | Not needed initially; can add later if cloud sync is required |
| OS Keyring | Platform-specific CGO dependencies; use encrypted file with restricted perms |

### 9. Implementation Status

The storage layer is **implemented** in `internal/storage/` with full integration into the application:

```
internal/storage/
├── storage.go          # Core interfaces: KVStore, SQLStore, VectorStore, SearchIndex
├── manager.go          # StorageManager with progressive Phase 1/2/3 init
├── eviction.go         # Background prompt cache TTL eviction
├── manager_test.go     # Manager tests (mocked backends)
├── kv/
│   ├── kv.go           # bbolt-backed KV store
│   └── kv_test.go      # CRUD, bucket, ForEach tests
├── sqlite/
│   ├── sqlite.go       # modernc.org/sqlite with migrations, WAL, PRAGMA
│   └── sqlite_test.go  # Schema, CRUD, cascade delete, transaction tests
├── search/
│   ├── search.go       # Bleve v2 full-text search (lazy index creation)
│   └── search_test.go  # Index, batch, search, delete tests
└── vector/
    ├── vector.go       # chromem-go vector store (persistent, GZIP GOB)
    └── vector_test.go  # Add, query by text/embedding, delete tests

internal/runtime/
├── config/cache.go         # bbolt-backed config cache with fingerprinting
├── embedding/
│   ├── embedding.go        # Embedding provider interface + hash provider
│   ├── api.go              # OpenAI-compatible API embedding provider
│   └── embedder.go         # Background embedder for code, memory, sessions, docs
├── indexer/indexer.go       # Background Bleve codebase indexer
├── memory/
│   ├── bleveindex.go       # Bleve-backed full-text memory search
│   └── vectorindex.go      # Vector-backed semantic memory search
├── permission/cache.go     # bbolt-backed permission policy + approval cache
├── session/
│   ├── sqlwriter.go        # SQLite dual-writer for session persistence
│   ├── indexer.go          # JSONL→SQLite session indexer
│   └── searchindex.go      # Bleve indexer for session compaction

internal/tools/
├── metrics.go              # Tool usage metrics recording to SQLite
└── semantic.go             # Semantic code search tool (vector-backed)
```

All tests pass with `go test -race ./...`.

### 10. Implementation TODO Checklist

#### Phase 1: Core Storage (DONE)

- [x] Define storage interfaces (`KVStore`, `SQLStore`, `VectorStore`, `SearchIndex`)
- [x] Implement `StorageManager` with progressive initialization
- [x] Implement bbolt KV store (`internal/storage/kv/`)
- [x] Implement SQLite store with migrations (`internal/storage/sqlite/`)
- [x] Implement Bleve search index (`internal/storage/search/`)
- [x] Implement chromem-go vector store (`internal/storage/vector/`)
- [x] Add dependencies: `modernc.org/sqlite`, `go.etcd.io/bbolt`, `bleve/v2`, `chromem-go`
- [x] Write unit tests for all backends
- [x] Full project `go vet` and `go test -race` passing

#### Phase 2: Integration (DONE)

- [x] Wire `StorageManager` into `cmd/ycode/main.go` initialization
- [x] Add `StorageManager` to `App` struct and `AppOptions`
- [x] Dual-write session metadata to JSONL + SQLite
- [x] Index existing JSONL sessions into SQLite on first run
- [x] Replace in-memory config cache with bbolt-backed cache
- [x] Replace in-memory permission cache with bbolt-backed cache
- [x] Add prompt cache table TTL eviction (background goroutine)
- [x] Add tool usage metrics recording to SQLite

#### Phase 3: Search Integration (DONE)

- [x] Wire Bleve into `Grep` tool for natural-language fallback
- [x] Wire Bleve into `ToolSearch` deferred tool discovery
- [x] Background codebase indexer (watches file changes)
- [x] Index memory entries into Bleve on save
- [x] Index session messages into Bleve on compaction
- [x] Replace `memory.Search()` keyword matching with Bleve

#### Phase 4: Vector Integration (DONE)

- [x] Define embedding provider interface (pluggable: LLM API or local model)
- [x] Wire vector store into memory `Recall()` for semantic similarity
- [x] Background embedder for code chunks (function/class level)
- [x] Background embedder for memory entries
- [x] Background embedder for session summaries
- [x] Add semantic code search tool (complement to grep/glob)
- [x] Add vector-enhanced documentation retrieval

#### Phase 5: Optimization (DONE)

- [x] Benchmark storage backends under realistic workloads (`storage/benchmark_test.go`)
- [x] Add SQLite connection pooling tuning (SetMaxOpenConns/SetMaxIdleConns)
- [x] Add Bleve index compaction scheduling (Compact method, close-reopen merge)
- [x] Add chromem-go persistence tuning (WithCompression, WithConcurrency options)
- [x] Monitor binary size impact; factory-based optional backends (build tags unnecessary)
- [x] Add storage health check to `doctor` diagnostic output
