# Memory Management in Continue (priorart/continue/)

This document summarizes how Continue (the TypeScript IDE extension) handles multi-turn conversations, context engineering, harness engineering, and memory management.

---

## 1. Architecture Overview

Continue is a multi-IDE extension (VS Code + JetBrains) that functions as a programmable coding assistant. Its distinguishing feature is an **extensible context provider architecture** with 32+ pluggable providers, codebase RAG via embeddings, and a multi-role model system.

---

## 2. Context Provider System

### Plugin Architecture

File: `core/context/providers/index.ts`

Continue uses a plugin-based system where context items are dynamically sourced from providers invoked via **@-mentions**.

**Provider Interface** (`core/context/index.ts`):
```typescript
abstract class BaseContextProvider {
  abstract getContextItems(query: string, extras: ContextProviderExtras): Promise<ContextItem[]>;
  async loadSubmenuItems(args): Promise<ContextSubmenuItem[]>;
}
```

### 32+ Context Provider Types

| Category | Providers |
|----------|-----------|
| **File-based** | `@file`, `@currentFile`, `@fileTree`, `@folder`, `@openFiles` |
| **Codebase** | `@codebase` (RAG), `@code` (structure), `@repoMap` (overview) |
| **Git** | `@gitCommit`, `@diff`, `@githubIssues`, `@gitlabMR` |
| **External** | `@docs`, `@web`, `@url`, `@http`, `@google` |
| **IDE** | `@terminal`, `@debugLocals`, `@problems`, `@clipboard`, `@os` |
| **Database** | `@postgres`, `@database` |
| **Integration** | `@jira`, `@discord`, `@greptile`, `@mcp`, `@continueProxy` |
| **Custom** | `@custom` (user-defined) |

### Provider Types

- **"normal"** — Direct query (e.g., `@file filename.ts`)
- **"query"** — Search-based retrieval (e.g., `@codebase explain auth`)
- **"submenu"** — Interactive selection (e.g., `@docs` with list)

---

## 3. Codebase Indexing / RAG

### Multi-Index Architecture

File: `core/indexing/CodebaseIndexer.ts`

Continue maintains parallel indexes:

| Index | Storage | Purpose |
|-------|---------|---------|
| **LanceDB Vector** | Apache Arrow columnar | Similarity search via embeddings |
| **SQLite FTS5** | Full-text search | Keyword-based fallback |
| **Code Snippets** | Function/class level | Semantic code units |
| **Chunk Index** | Fixed-size chunks | General chunking |

### Smart Chunking

File: `core/indexing/chunk/chunk.ts`

- **Code files**: Tree-sitter AST-aware chunking (JS/TS, Python, Java, Go, Rust, etc.)
- **Non-code**: Basic fixed-size chunking (CSS, HTML, JSON, YAML)
- **Batch size**: 200 files per embedding batch

### Retrieval Pipeline

File: `core/context/retrieval/retrieval.ts`

```
Budget: nFinal = min(25, contextLength / 512 / 2)
Step 1: Initial retrieve (2x nFinal if reranking enabled)
Step 2: Rerank (if reranker model configured)
Step 3: Format results sorted alphabetically with line numbers
```

### Embedding Providers

Configurable: OpenAI, Ollama (local), HuggingFace TEI, TransformersJS (in-process), Cohere, Voyage, Azure, etc.

---

## 4. Multi-Turn Conversation

### Session Structure

File: `core/util/history.ts`

```typescript
interface Session {
  sessionId: string;
  title: string;
  workspaceDirectory: string;
  history: ChatHistoryItem[];
  mode?: "chat" | "agent" | "plan" | "background";
  chatModelTitle?: string;
  usage?: SessionUsage;
}
```

Storage: `~/.continue/sessions/{sessionId}.json`

### 4 Message Modes

| Mode | Purpose |
|------|---------|
| `chat` | Standard conversation |
| `agent` | Agentic tool-use loop |
| `plan` | Planning mode with structured steps |
| `background` | Async background operations |

### Conversation Compaction

File: `core/util/conversationCompaction.ts`

When history grows too large:
1. Find most recent `conversationSummary` in history
2. Extract unsummarized messages since that point
3. Generate new summary via LLM
4. Store in `ChatHistoryItem.conversationSummary` field
5. Subsequent compilations use summary instead of full history

---

## 5. System Prompt Engineering

### Token Budget Allocation

File: `core/llm/countTokens.ts`

```
Available = contextLength - safetyBuffer - minOutputTokens - toolTokens - systemTokens - lastMessageTokens
```

Old messages pruned (FIFO) when over budget. Returns `didPrune` flag and `contextPercentage`.

### Model-Specific Templates (11+)

File: `core/llm/templates/chat.ts`

Llama2, Anthropic, ChatML, Zephyr, Alpaca, DeepSeek, Phi2, Codestral, Llava, etc.

### Rule Injection

Rules injected from multiple sources:
- `.continuerules` files in workspace
- `default-chat`, `default-plan`, `default-agent` system messages
- `json-systemMessage`, `model-options-chat`
- `colocated-markdown` (rules.md)
- Agent-specific overrides

### Multi-Role Model System

| Role | Purpose |
|------|---------|
| `chat` | Main conversation |
| `autocomplete` | Inline code completion |
| `embed` | Embeddings for RAG |
| `rerank` | Reranking results |
| `edit` | Code editing |
| `apply` | Applying edits |
| `plan` | Planning mode |
| `agent` | Agent reasoning |

---

## 6. Slash Commands / Skills

### Command Sources

| Source | Example |
|--------|---------|
| `built-in-legacy` | `/commit`, `/review`, `/draftIssue`, `/share` |
| `prompt-file-v1/v2` | Markdown prompt files with frontmatter |
| `mcp-prompt` | MCP server prompts |
| `yaml-prompt-block` | YAML config prompts |
| `invokable-rule` | Rule-based commands |
| `json-custom-command` | User-defined in config |
| `config-ts-slash-command` | TypeScript config commands |

Commands receive a `ContinueSDK` with access to: IDE, LLM, context items, history, config, abort controller.

---

## 7. Documentation Indexing

### Multi-Crawler Architecture

File: `core/indexing/docs/`

| Crawler | Use Case |
|---------|----------|
| GitHub | `github.com` URLs → README + file content |
| Default | HTTP crawling up to `maxDepth=4`, max 1000 pages |
| Chromium | JavaScript-rendered pages |
| Cheerio | HTML parsing fallback |

Storage: LanceDB for vectors + SQLite for metadata. Accessible via `@docs` context provider.

---

## 8. Key Constants

| Constant | Value | File |
|----------|-------|------|
| Default context length | 4,096 | `llm/constants.ts` |
| Default max tokens | 2,048 | `llm/constants.ts` |
| Min response tokens | 512 | `llm/constants.ts` |
| Files per index batch | 200 | `CodebaseIndexer.ts` |
| Max retrieval results | 25 | `retrieval.ts` |
| Tokens per snippet | 512 | `retrieval.ts` |
| Max crawl depth | 4 | `DocsCrawler.ts` |
| Max requests per crawl | 1,000 | `DocsCrawler.ts` |

---

## 9. Comparison with ycode

| Feature | Continue | ycode |
|---------|----------|-------|
| **Context providers** | 32+ pluggable providers | CLAUDE.md + memories |
| **RAG** | LanceDB + FTS5 + reranking | None (file-based search only) |
| **Embeddings** | Multi-provider (OpenAI, Ollama, etc.) | None |
| **Compaction** | LLM summarization to conversationSummary | 3-layer (prune → compact → flush) |
| **Session modes** | 4 modes (chat, agent, plan, background) | Single mode |
| **Model roles** | 8 roles (chat, embed, rerank, etc.) | Single model |
| **Docs indexing** | Multi-crawler + vector search | None |
| **Slash commands** | 7 sources, ContinueSDK | Skill-based |
| **Token counting** | Exact (per-model tokenizer) | Heuristic (len/4+1) |

### Key Features ycode Could Adopt

1. **Pluggable context providers** — extensible @-mention system for dynamic context
2. **Codebase RAG with embeddings** — vector + FTS hybrid search for code retrieval
3. **LLM-based conversation compaction** — use model for summarization
4. **Multi-role model configuration** — different models for chat vs embed vs rerank
5. **Documentation indexing** — crawl and index docs for @docs retrieval
6. **Session modes** — agent, plan, chat, background with different behaviors

---

*This analysis is based on the Continue codebase as of April 2025.*
