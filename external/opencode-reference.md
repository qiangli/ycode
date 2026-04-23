# opencode Architecture Reference

Extracted from `priorart/opencode/packages/opencode/src/` -- TypeScript codebase.
This is a cached summary of key modules, types, and function signatures relevant
to instruction discovery, prompt assembly, tool definitions, agents, config, and skills.

Source commit: snapshot as of 2026-04-23. Read-only reference; do not modify priorart/.

---

## Table of Contents

1. [Instruction Discovery](#1-instruction-discovery)
2. [System Prompt Assembly](#2-system-prompt-assembly)
3. [Session Prompt (orchestrator)](#3-session-prompt-orchestrator)
4. [Tool Framework](#4-tool-framework)
5. [Tool: read](#5-tool-read)
6. [Tool: grep](#6-tool-grep)
7. [Tool: glob](#7-tool-glob)
8. [Tool Registry](#8-tool-registry)
9. [Agent Definitions](#9-agent-definitions)
10. [Configuration](#10-configuration)
11. [Skill System](#11-skill-system)
12. [Permission System](#12-permission-system)
13. [Cross-Module Wiring](#13-cross-module-wiring)

---

## 1. Instruction Discovery

**File:** `session/instruction.ts`

Discovers and loads instruction files (AGENTS.md, CLAUDE.md, CONTEXT.md) from the
project tree and global config, then resolves per-directory instruction files when
the read tool accesses files in subdirectories.

### Constants

```ts
const FILES = [
  "AGENTS.md",
  // CLAUDE.md included unless OPENCODE_DISABLE_CLAUDE_CODE_PROMPT flag is set
  "CONTEXT.md",  // deprecated
]
```

### Key Functions

```ts
// Returns global instruction file paths (from OPENCODE_CONFIG_DIR or ~/.config/opencode/)
function globalFiles(): string[]

// Extracts file paths from tool-call results that have already been loaded
// (prevents re-attaching instruction files the read tool already surfaced)
function extract(messages: MessageV2.WithParts[]): Set<string>

// Re-export of extract() for external use
export function loaded(messages: MessageV2.WithParts[]): Set<string>
```

### Service Interface

```ts
export interface Interface {
  // Remove claim tracking for a given message
  readonly clear: (messageID: MessageID) => Effect.Effect<void>

  // Return the resolved set of system-level instruction file paths
  readonly systemPaths: () => Effect.Effect<Set<string>, AppFileSystem.Error>

  // Read and format all system instruction files into prefixed strings
  // Returns: ["Instructions from: /path\n<content>", ...]
  readonly system: () => Effect.Effect<string[], AppFileSystem.Error>

  // Find the first matching instruction file (AGENTS.md/CLAUDE.md/CONTEXT.md) in a dir
  readonly find: (dir: string) => Effect.Effect<string | undefined, AppFileSystem.Error>

  // Walk upward from a file being read, attach nearby instruction files once per message.
  // Deduplicates against system paths, already-loaded paths, and per-message claims.
  readonly resolve: (
    messages: MessageV2.WithParts[],
    filepath: string,
    messageID: MessageID,
  ) => Effect.Effect<{ filepath: string; content: string }[], AppFileSystem.Error>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/Instruction") {}
```

### Discovery Algorithm (systemPaths)

1. **Project-level**: `findUp()` from `ctx.directory` to `ctx.worktree` for each file in `FILES`. First match wins (no stacking).
2. **Global-level**: Check `OPENCODE_CONFIG_DIR/AGENTS.md`, then `~/.config/opencode/AGENTS.md`, then `~/.claude/CLAUDE.md`. First found wins.
3. **Config `instructions` array**: Each entry is either a URL (fetched later), absolute path, `~/`-prefixed path, or relative glob walked upward from project dir.

### Resolve Algorithm (per-read-tool call)

When `read` tool accesses a file, `resolve()` walks from the file's directory upward
to the project root, looking for AGENTS.md/CLAUDE.md in each directory. It skips:
- The file being read itself
- System-level instruction paths
- Paths already loaded by previous tool calls
- Paths already claimed for this assistant message

Found instructions are appended as `<system-reminder>` blocks in the read output.

### Dependencies

- `Config.Service` -- reads `config.instructions` array
- `AppFileSystem.Service` -- file existence, globbing, reading
- `HttpClient` -- fetches URL-based instructions

---

## 2. System Prompt Assembly

**File:** `session/system.ts`

Selects the appropriate base system prompt per model family and assembles
environment context and skill listings.

### Provider Prompt Selection

```ts
// Returns the appropriate system prompt text based on model ID pattern matching
export function provider(model: Provider.Model): string[]
```

Mapping:
- `gpt-4*`, `o1*`, `o3*` -> `PROMPT_BEAST`
- `*codex*` -> `PROMPT_CODEX`
- `gpt*` -> `PROMPT_GPT`
- `gemini-*` -> `PROMPT_GEMINI`
- `claude*` -> `PROMPT_ANTHROPIC`
- `*trinity*` -> `PROMPT_TRINITY`
- `*kimi*` -> `PROMPT_KIMI`
- default -> `PROMPT_DEFAULT`

Prompt texts are imported from `session/prompt/*.txt` files.

### Service Interface

```ts
export interface Interface {
  // Generates environment context block with model info, working dir, platform, date
  readonly environment: (model: Provider.Model) => string[]

  // Generates skill listing for system prompt (returns undefined if skills are denied)
  readonly skills: (agent: Agent.Info) => Effect.Effect<string | undefined>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/SystemPrompt") {}
```

### Environment Block Format

```
You are powered by the model named {model.api.id}. The exact model ID is {providerID}/{model.api.id}
Here is some useful information about the environment you are running in:
<env>
  Working directory: {Instance.directory}
  Workspace root folder: {Instance.worktree}
  Is directory a git repo: {yes|no}
  Platform: {process.platform}
  Today's date: {date}
</env>
```

### Dependencies

- `Skill.Service` -- lists available skills for the agent
- `Permission` -- checks if skill permission is denied for the agent

---

## 3. Session Prompt (orchestrator)

**File:** `session/prompt.ts` (~1800 lines)

The main orchestration module that drives the conversation loop: assembles system
prompt, resolves user input, manages tools, and streams LLM responses.

### Service Interface

```ts
export interface Interface {
  readonly cancel: (sessionID: SessionID) => Effect.Effect<void>
  readonly prompt: (input: PromptInput) => Effect.Effect<MessageV2.WithParts>
  readonly loop: (input: LoopInput) => Effect.Effect<MessageV2.WithParts>
  readonly shell: (input: ShellInput) => Effect.Effect<MessageV2.WithParts>
  readonly command: (input: CommandInput) => Effect.Effect<MessageV2.WithParts>
  readonly resolvePromptParts: (template: string) => Effect.Effect<PromptInput["parts"]>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/SessionPrompt") {}
```

### Input Types

```ts
export const PromptInput = Schema.Struct({
  sessionID: SessionID,
  messageID: Schema.optional(MessageID),
  model: Schema.optional(Schema.Struct({ providerID: ProviderID, modelID: ModelID })),
  agent: Schema.optional(Schema.String),
  noReply: Schema.optional(Schema.Boolean),
  tools: Schema.optional(Schema.Record(Schema.String, Schema.Boolean)),  // deprecated
  format: Schema.optional(MessageV2.Format),
  system: Schema.optional(Schema.String),
  variant: Schema.optional(Schema.String),
  parts: Schema.Array(Schema.Union([TextPartInput, FilePartInput, AgentPartInput, SubtaskPartInput])),
})
export type PromptInput = Omit<Schema.Schema.Type<typeof PromptInput>, "parts"> & {
  parts: (TextPartInput | FilePartInput | AgentPartInput | SubtaskPartInput)[]
}

export class LoopInput extends Schema.Class<LoopInput>("SessionPrompt.LoopInput")({
  sessionID: SessionID,
}) {}

export const ShellInput = Schema.Struct({
  sessionID: SessionID,
  messageID: Schema.optional(MessageID),
  agent: Schema.String,
  model: Schema.optional(ModelRef),
  command: Schema.String,
})

export const CommandInput = Schema.Struct({
  messageID: Schema.optional(MessageID),
  sessionID: SessionID,
  agent: Schema.optional(Schema.String),
  model: Schema.optional(Schema.String),
  arguments: Schema.String,
  command: Schema.String,
  variant: Schema.optional(Schema.String),
  parts: Schema.optional(Schema.Array(/* file parts */)),
})
```

### Key Internal Functions

```ts
// Resolves @-mentions in prompt template to file/agent parts
resolvePromptParts(template: string) => Effect.Effect<PromptInput["parts"]>

// Generates session title using a small model after first user message
title(input: { session, history, providerID, modelID }) => Effect.Effect<void>

// Injects plan-mode reminders and build-switch prompts into message history
insertReminders(input: { messages, agent, session }) => Effect.Effect<MessageV2.WithParts[]>

// Creates a structured output tool for JSON-schema-constrained responses
createStructuredOutputTool(input: { schema, onSuccess }) => AITool
```

### System Prompt Assembly Order (within prompt/loop)

1. Provider-specific base prompt (from `system.ts`)
2. Environment block (model info, working dir, platform, date)
3. Instruction files (from `instruction.ts` -- AGENTS.md, etc.)
4. Skill listing (from `system.ts`)
5. Agent-specific prompt (if `agent.prompt` is set)
6. Plugin system transforms
7. Plan-mode / build-switch reminders (injected into message history)

### Dependencies

~20 services injected: Bus, SessionStatus, Session, Agent, Provider, Processor,
Compaction, Plugin, Command, Permission, MCP, LSP, ToolRegistry, Truncate,
Instruction, SystemPrompt, LLM, AppFileSystem, etc.

---

## 4. Tool Framework

**File:** `tool/tool.ts`

Base types and factory for defining tools with automatic truncation and validation.

### Types

```ts
export type Context<M extends Metadata = Metadata> = {
  sessionID: SessionID
  messageID: MessageID
  agent: string
  abort: AbortSignal
  callID?: string
  extra?: { [key: string]: unknown }
  messages: MessageV2.WithParts[]
  metadata(input: { title?: string; metadata?: M }): Effect.Effect<void>
  ask(input: Omit<Permission.Request, "id" | "sessionID" | "tool">): Effect.Effect<void>
}

export interface ExecuteResult<M extends Metadata = Metadata> {
  title: string
  metadata: M
  output: string
  attachments?: Omit<MessageV2.FilePart, "id" | "sessionID" | "messageID">[]
}

export interface Def<Parameters extends z.ZodType = z.ZodType, M extends Metadata = Metadata> {
  id: string
  description: string
  parameters: Parameters
  execute(args: z.infer<Parameters>, ctx: Context): Effect.Effect<ExecuteResult<M>>
  formatValidationError?(error: z.ZodError): string
}

// Without ID -- returned from init functions
export type DefWithoutID<P, M> = Omit<Def<P, M>, "id">

// Tool registration info -- lazy init
export interface Info<P extends z.ZodType = z.ZodType, M extends Metadata = Metadata> {
  id: string
  init: () => Effect.Effect<DefWithoutID<P, M>>
}
```

### Factory Function

```ts
// Creates a tool with automatic truncation wrapping and validation
export function define<P extends z.ZodType, Result extends Metadata, R, ID extends string>(
  id: ID,
  init: Effect.Effect<Init<P, Result>, never, R>,
): Effect.Effect<Info<P, Result>, never, R | Truncate.Service | Agent.Service> & { id: ID }

// Initializes a tool Info into a full Def (calls init() and attaches id)
export function init<P extends z.ZodType, M extends Metadata>(info: Info<P, M>): Effect.Effect<Def<P, M>>
```

### Truncation Middleware

The `wrap()` function intercepts tool execution results. If `metadata.truncated` is
already set, the result passes through. Otherwise, `truncate.output()` is called to
cap output size, writing overflow to a temp file and returning a truncated version
with `outputPath` metadata.

### Type Inference Helpers

```ts
export type InferParameters<T> = /* extracts z.infer<P> from Info or Effect<Info> */
export type InferMetadata<T> = /* extracts M from Info or Effect<Info> */
export type InferDef<T> = /* extracts Def<P,M> from Info or Effect<Info> */
```

---

## 5. Tool: read

**File:** `tool/read.ts`

Reads files and directories with line numbering, pagination, binary detection,
image/PDF handling, and automatic instruction file discovery.

### Parameters

```ts
z.object({
  filePath: z.string(),       // Absolute path to file or directory
  offset: z.coerce.number(),  // Line number to start reading from (1-indexed), optional
  limit: z.coerce.number(),   // Max lines to read (default 2000), optional
})
```

### Constants

```ts
const DEFAULT_READ_LIMIT = 2000
const MAX_LINE_LENGTH = 2000
const MAX_BYTES = 50 * 1024   // 50 KB cap per read
const SAMPLE_BYTES = 4096     // bytes sampled for binary detection
```

### Behavior

1. Resolves relative paths against `Instance.directory`
2. Checks external directory permission via `assertExternalDirectoryEffect`
3. Asks permission via `ctx.ask({ permission: "read", ... })`
4. **Directory**: lists entries with pagination support
5. **Image/PDF**: reads as base64 attachment
6. **Binary**: rejects with error
7. **Text file**: reads with line numbers (`N: content`), byte-capped, with pagination
8. **Instruction resolution**: calls `instruction.resolve()` to find nearby AGENTS.md files;
   appends them as `<system-reminder>` blocks

### Output Format

```xml
<path>/abs/path</path>
<type>file</type>
<content>
1: first line
2: second line
...
(End of file - total N lines)
</content>

<system-reminder>
Instructions from: /path/to/AGENTS.md
...content...
</system-reminder>
```

### Dependencies

- `AppFileSystem.Service` -- file I/O
- `Instruction.Service` -- resolves nearby instruction files
- `LSP.Service` -- warms LSP index on read

---

## 6. Tool: grep

**File:** `tool/grep.ts`

Searches file contents using ripgrep with regex patterns.

### Parameters

```ts
z.object({
  pattern: z.string(),          // Regex pattern to search for
  path: z.string().optional(),  // Directory to search (default: cwd)
  include: z.string().optional(), // File glob filter (e.g. "*.js", "*.{ts,tsx}")
})
```

### Behavior

1. Resolves path relative to `InstanceState.context.directory`
2. Checks external directory permission
3. Delegates to `Ripgrep.Service.search()` with pattern, glob, and abort signal
4. Results sorted by file modification time (most recent first)
5. Output capped at 100 matches; lines truncated at 2000 chars

### Output Format

```
Found N matches
/path/to/file.ts:
  Line 42: matching content here
  Line 87: another match

(Results truncated: showing 100 of N matches)
```

### Dependencies

- `AppFileSystem.Service` -- stat for mtime sorting
- `Ripgrep.Service` -- actual search execution

---

## 7. Tool: glob

**File:** `tool/glob.ts`

Finds files matching glob patterns, sorted by modification time.

### Parameters

```ts
z.object({
  pattern: z.string(),          // Glob pattern (e.g. "**/*.ts")
  path: z.string().optional(),  // Directory to search (default: cwd)
})
```

### Behavior

1. Resolves path, validates it is a directory
2. Checks external directory permission
3. Uses `Ripgrep.Service.files()` to stream matching file paths
4. Collects up to 101 results (cap at 100, detect truncation)
5. Stats each file for mtime, sorts most-recent-first

### Output Format

```
/path/to/matching/file1.ts
/path/to/matching/file2.ts
...
(Results are truncated: showing first 100 results.)
```

### Dependencies

- `Ripgrep.Service` -- file listing
- `AppFileSystem.Service` -- stat for mtime

---

## 8. Tool Registry

**File:** `tool/registry.ts`

Central registry that collects all built-in tools, plugin tools, and MCP tools,
and filters them per-agent based on permissions.

### Built-in Tools

```
BashTool, EditTool, GlobTool, GrepTool, ReadTool, WriteTool,
TaskTool, TodoWriteTool, QuestionTool, PlanExitTool, SkillTool,
WebFetchTool, WebSearchTool, CodeSearchTool, LspTool, ApplyPatchTool,
InvalidTool
```

### Service Interface

```ts
export interface Interface {
  readonly ids: () => Effect.Effect<string[]>
  readonly all: () => Effect.Effect<Tool.Def[]>
  readonly named: () => Effect.Effect<{ task: TaskDef; read: ReadDef }>
  readonly tools: (model: {
    providerID: ProviderID;
    modelID: ModelID;
    agent: Agent.Info
  }) => Effect.Effect<Tool.Def[]>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/ToolRegistry") {}
```

### Tool Filtering

`tools()` filters the full tool list based on the agent's permission ruleset.
Tools whose permission evaluates to "deny" for the agent are excluded.
Provider-specific tool transforms may also apply (e.g., some providers get
`ApplyPatch` instead of `Edit`).

### Exports (`tool/index.ts`)

```ts
export * as Truncate from "./truncate"
export * as ToolRegistry from "./registry"
export * as Tool from "./tool"
```

---

## 9. Agent Definitions

**File:** `agent/agent.ts`

Defines agent configurations that control behavior, permissions, and model selection.

### Info Schema (Zod)

```ts
export const Info = z.object({
  name: z.string(),
  description: z.string().optional(),
  mode: z.enum(["subagent", "primary", "all"]),
  native: z.boolean().optional(),       // true for built-in agents
  hidden: z.boolean().optional(),       // hidden from UI listing
  topP: z.number().optional(),
  temperature: z.number().optional(),
  color: z.string().optional(),
  permission: Permission.Ruleset.zod,   // per-agent permission rules
  model: z.object({                     // optional model override
    modelID: ModelID.zod,
    providerID: ProviderID.zod,
  }).optional(),
  variant: z.string().optional(),
  prompt: z.string().optional(),        // agent-specific system prompt
  options: z.record(z.string(), z.any()),
  steps: z.number().int().positive().optional(),
})
export type Info = z.infer<typeof Info>
```

### Built-in Agents

| Name | Mode | Description |
|------|------|-------------|
| `build` | primary | Default agent. Full tool access. |
| `plan` | primary | Plan mode. Denies edit tools except plan file. |
| `general` | subagent | Multi-step tasks, parallel work units. Denies todowrite. |
| `explore` | subagent | Fast read-only codebase exploration. Only read/search tools. |
| `compaction` | primary (hidden) | Context compaction. No tools. |
| `title` | primary (hidden) | Title generation. No tools. temp=0.5. |
| `summary` | primary (hidden) | Session summary. No tools. |

### Permission Defaults

```ts
const defaults = Permission.fromConfig({
  "*": "allow",
  doom_loop: "ask",
  external_directory: { "*": "ask", ...whitelistedDirs },
  question: "deny",
  plan_enter: "deny",
  plan_exit: "deny",
  read: {
    "*": "allow",
    "*.env": "ask",
    "*.env.*": "ask",
    "*.env.example": "allow",
  },
})
```

### Service Interface

```ts
export interface Interface {
  readonly get: (agent: string) => Effect.Effect<Info>
  readonly list: () => Effect.Effect<Info[]>
  readonly defaultAgent: () => Effect.Effect<string>
  readonly generate: (input: {
    description: string
    model?: { providerID: ProviderID; modelID: ModelID }
  }) => Effect.Effect<{ identifier: string; whenToUse: string; systemPrompt: string }>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/Agent") {}
```

### Agent Config Merge

User config (`config.agent.<name>`) is merged over built-in defaults:
- `model`, `variant`, `prompt`, `description`, `temperature`, `topP`, `mode`,
  `color`, `hidden`, `name`, `steps` can all be overridden
- `options` deep-merged
- `permission` merged additively
- `disable: true` removes the agent entirely

### Dependencies

- `Config.Service`, `Auth.Service`, `Plugin.Service`, `Skill.Service`, `Provider.Service`

---

## 10. Configuration

**File:** `config/config.ts`

Multi-layer config system that merges global, project, remote, and managed configs.

### Info Schema (Effect Schema, derived to Zod)

Key fields (abbreviated):

```ts
export const Info = Schema.Struct({
  $schema: Schema.optional(Schema.String),
  logLevel: Schema.optional(LogLevel),
  server: Schema.optional(ConfigServer.Server),
  command: Schema.optional(Schema.Record(Schema.String, ConfigCommand.Info)),
  skills: Schema.optional(ConfigSkills.Info),       // { paths?: string[], urls?: string[] }
  watcher: Schema.optional(Schema.Struct({ ignore: Schema.optional(Schema.Array(Schema.String)) })),
  snapshot: Schema.optional(Schema.Boolean),
  plugin: Schema.optional(Schema.Array(ConfigPlugin.Spec)),
  share: Schema.optional(Schema.Literals(["manual", "auto", "disabled"])),
  autoupdate: Schema.optional(Schema.Union([Schema.Boolean, Schema.Literal("notify")])),
  disabled_providers: Schema.optional(Schema.Array(Schema.String)),
  enabled_providers: Schema.optional(Schema.Array(Schema.String)),
  model: Schema.optional(ConfigModelID),            // "provider/model" format
  small_model: Schema.optional(ConfigModelID),
  default_agent: Schema.optional(Schema.String),
  username: Schema.optional(Schema.String),
  agent: Schema.optional(/* Record<string, ConfigAgent.Info> */),
  provider: Schema.optional(Schema.Record(Schema.String, ConfigProvider.Info)),
  mcp: Schema.optional(Schema.Record(Schema.String, ConfigMCP.Info)),
  formatter: Schema.optional(ConfigFormatter.Info),
  lsp: Schema.optional(ConfigLSP.Info),
  instructions: Schema.optional(Schema.Array(Schema.String)),  // <-- instruction file paths/URLs
  permission: Schema.optional(ConfigPermission.Info),
  tools: Schema.optional(Schema.Record(Schema.String, Schema.Boolean)),  // legacy
  compaction: Schema.optional(Schema.Struct({
    auto: Schema.optional(Schema.Boolean),           // default true
    prune: Schema.optional(Schema.Boolean),           // default true
    tail_turns: Schema.optional(NonNegativeInt),      // default 2
    preserve_recent_tokens: Schema.optional(NonNegativeInt),
    reserved: Schema.optional(NonNegativeInt),
  })),
  experimental: Schema.optional(Schema.Struct({
    disable_paste_summary: Schema.optional(Schema.Boolean),
    batch_tool: Schema.optional(Schema.Boolean),
    openTelemetry: Schema.optional(Schema.Boolean),
    primary_tools: Schema.optional(Schema.Array(Schema.String)),
    continue_loop_on_deny: Schema.optional(Schema.Boolean),
    mcp_timeout: Schema.optional(PositiveInt),
  })),
})
```

### Skills Config Sub-Schema

**File:** `config/skills.ts`

```ts
export const Info = Schema.Struct({
  paths: Schema.optional(Schema.Array(Schema.String)),  // local directories
  urls: Schema.optional(Schema.Array(Schema.String)),   // remote skill index URLs
})
```

### Service Interface

```ts
export interface Interface {
  readonly get: () => Effect.Effect<Info>
  readonly getGlobal: () => Effect.Effect<Info>
  readonly getConsoleState: () => Effect.Effect<ConsoleState>
  readonly update: (config: Info) => Effect.Effect<void>
  readonly updateGlobal: (config: Info) => Effect.Effect<Info>
  readonly invalidate: (wait?: boolean) => Effect.Effect<void>
  readonly directories: () => Effect.Effect<string[]>
  readonly waitForDependencies: () => Effect.Effect<void>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/Config") {}
```

### Config Merge Order (loadInstanceState)

1. **Well-known remote configs** -- fetched from auth provider URLs (`/.well-known/opencode`)
2. **Global config** -- `~/.config/opencode/{config.json, opencode.json, opencode.jsonc}`
3. **OPENCODE_CONFIG env var** -- explicit config file path
4. **Project config** -- `opencode.{json,jsonc}` found via `ConfigPaths.files()` walking up from cwd
5. **Config directories** -- `.opencode/opencode.{json,jsonc}` in project dirs
6. **OPENCODE_CONFIG_CONTENT env var** -- inline JSON config
7. **Console/account org config** -- from active account's API
8. **Managed config dir** -- system-managed config (MDM/mobileconfig on macOS)
9. **Legacy `mode` -> `agent` migration**
10. **OPENCODE_PERMISSION env var** -- permission overrides
11. **`tools` -> `permission` migration** -- legacy tool enable/disable

Array fields (like `instructions`) are concatenated with deduplication, not replaced.
Object fields are deep-merged using `mergeDeep`.

### Config File Locations

```ts
function globalConfigFile(): string
// Checks: opencode.jsonc, opencode.json, config.json in Global.Path.config
// Returns first existing, or opencode.jsonc as default

// Project config search: ConfigPaths.files("opencode", ctx.directory, ctx.worktree)
// Config directories: ConfigPaths.directories(ctx.directory, ctx.worktree)
```

### Key Helpers

```ts
// Merges two Info objects, concatenating array fields like `instructions`
function mergeConfigConcatArrays(target: Info, source: Info): Info

// Patches JSONC content using jsonc-parser (preserves comments)
function patchJsonc(input: string, patch: unknown, path?: string[]): string
```

---

## 11. Skill System

### Skill Index

**File:** `skill/index.ts`

Skills are markdown files (`SKILL.md`) with YAML frontmatter containing `name` and
`description`. They provide specialized instructions loaded on demand via the skill tool.

#### Info Schema

```ts
export const Info = z.object({
  name: z.string(),
  description: z.string(),
  location: z.string(),   // absolute path to SKILL.md
  content: z.string(),    // markdown body (without frontmatter)
})
export type Info = z.infer<typeof Info>
```

#### Service Interface

```ts
export interface Interface {
  readonly get: (name: string) => Effect.Effect<Info | undefined>
  readonly all: () => Effect.Effect<Info[]>
  readonly dirs: () => Effect.Effect<string[]>
  readonly available: (agent?: Agent.Info) => Effect.Effect<Info[]>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/Skill") {}
```

#### Discovery Algorithm (discoverSkills)

1. **External dirs** (unless `OPENCODE_DISABLE_EXTERNAL_SKILLS`):
   - `~/.claude/skills/**/SKILL.md` and `~/.agents/skills/**/SKILL.md`
   - Walk up from `ctx.directory` to `ctx.worktree` looking for `.claude/` and `.agents/` dirs
2. **Config directories**: `{skill,skills}/**/SKILL.md` in each config dir
3. **Configured paths** (`config.skills.paths`): `**/SKILL.md` in each path
4. **Remote URLs** (`config.skills.urls`): fetched via `Discovery.pull()`

#### Skill Formatting

```ts
// Formats skill list for system prompt or tool description
export function fmt(list: Info[], opts: { verbose: boolean }): string
// verbose=true: XML format with <available_skills><skill><name>...<description>...<location>...
// verbose=false: markdown list "- **name**: description"
```

#### Constants

```ts
const EXTERNAL_DIRS = [".claude", ".agents"]
const EXTERNAL_SKILL_PATTERN = "skills/**/SKILL.md"
const OPENCODE_SKILL_PATTERN = "{skill,skills}/**/SKILL.md"
const SKILL_PATTERN = "**/SKILL.md"
```

### Skill Discovery (Remote)

**File:** `skill/discovery.ts`

Fetches skill indexes from remote URLs and caches them locally.

#### Schema

```ts
class IndexSkill extends Schema.Class<IndexSkill>("IndexSkill")({
  name: Schema.String,
  files: Schema.Array(Schema.String),
})

class Index extends Schema.Class<Index>("Index")({
  skills: Schema.Array(IndexSkill),
})
```

#### Service Interface

```ts
export interface Interface {
  // Fetch skill index from URL, download files, return local directory paths
  readonly pull: (url: string) => Effect.Effect<string[]>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/SkillDiscovery") {}
```

#### Pull Algorithm

1. Fetch `{url}/index.json` -> parse as `Index`
2. Filter skills that contain `SKILL.md`
3. Download each skill's files to `~/.cache/opencode/skills/{name}/`
4. Return directories that have a valid `SKILL.md`

Concurrency: 4 skills, 8 files per skill.

---

## 12. Permission System

**File:** `permission/index.ts` (partial)

Permission rules control tool access per agent. Rules are pattern-matched using
wildcards.

### Core Types

```ts
export const Action = Schema.Literals(["allow", "deny", "ask"])
export type Action = "allow" | "deny" | "ask"

export class Rule extends Schema.Class<Rule>("PermissionRule")({
  permission: Schema.String,  // tool or category name
  pattern: Schema.String,     // glob pattern for the target
  action: Action,             // allow, deny, or ask user
})

export const Ruleset = Schema.mutable(Schema.Array(Rule))
export type Ruleset = Rule[]

export class Request extends Schema.Class<Request>("PermissionRequest")({
  id: PermissionID,
  sessionID: SessionID,
  permission: Schema.String,
  patterns: Schema.Array(Schema.String),
  metadata: Schema.Record(Schema.String, Schema.Unknown),
  always: Schema.Array(Schema.String),
  tool: Schema.optional(Schema.Struct({ messageID: MessageID, callID: Schema.String })),
})
```

### Key Functions (referenced in other modules)

```ts
// Merge multiple rulesets (later rules override earlier ones)
Permission.merge(...rulesets: Ruleset[]): Ruleset

// Convert config permission object to Ruleset
Permission.fromConfig(config: Record<string, Action | Record<string, Action>>): Ruleset

// Evaluate a permission check against a ruleset
Permission.evaluate(permission: string, pattern: string, ruleset: Ruleset): { action: Action }

// Check which permissions are disabled for an agent
Permission.disabled(permissions: string[], ruleset: Ruleset): Set<string>
```

---

## 13. Cross-Module Wiring

### Effect Service Pattern

All modules use Effect's `Context.Service` pattern:
- Define an `Interface` with readonly method signatures returning `Effect.Effect<T>`
- Create a `Service` class extending `Context.Service`
- Export a `layer` that provides the service implementation
- Export a `defaultLayer` that includes all transitive dependencies

### Instruction -> Read Tool Connection

`read.ts` depends on `Instruction.Service`. After reading a file, it calls
`instruction.resolve(messages, filepath, messageID)` to discover nearby instruction
files, then appends them as `<system-reminder>` blocks in the tool output.

### Config -> Instruction Connection

`instruction.ts` reads `config.instructions` (string array) to find additional
instruction files. These can be:
- Absolute paths
- `~/`-prefixed paths
- Relative globs (walked upward from project dir)
- HTTP/HTTPS URLs (fetched at runtime)

### Config -> Skill Connection

`config.skills` provides `paths` (local dirs) and `urls` (remote indexes) that
feed into the skill discovery system. Skills are also auto-discovered from
`.claude/skills/` and `.agents/skills/` directories.

### Agent -> Permission Connection

Each agent carries a `permission: Ruleset` that controls which tools it can use.
The ruleset is built by merging:
1. Hard-coded defaults (e.g., `read: allow`, `doom_loop: ask`)
2. User config permissions
3. Agent-specific overrides

### Skill -> Agent Connection

`Skill.Service.available(agent)` filters skills by evaluating `Permission.evaluate("skill", skillName, agent.permission)`.
Skills denied by the agent's ruleset are excluded from the listing.

### SystemPrompt -> Prompt Assembly

`system.ts` provides:
1. `environment(model)` -> environment context block
2. `skills(agent)` -> skill listing (if not denied)

These are consumed by `prompt.ts` during system prompt assembly, alongside
instruction files from `instruction.ts` and the base provider prompt.

### Dependency Graph (simplified)

```
SessionPrompt
  +-- SystemPrompt
  |     +-- Skill.Service
  +-- Instruction.Service
  |     +-- Config.Service
  |     +-- AppFileSystem.Service
  +-- Agent.Service
  |     +-- Config.Service
  |     +-- Skill.Service
  |     +-- Permission (static)
  +-- ToolRegistry.Service
  |     +-- All tool definitions
  |     +-- Plugin.Service
  |     +-- Agent.Service (for permission filtering)
  +-- Provider.Service
  +-- Session.Service
  +-- LLM.Service
  +-- ... (20+ services total)
```
