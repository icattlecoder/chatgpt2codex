# Tool Definitions

The files under `src/core/tools` define the built-in tool set used by `pi-coding-agent` and expose the same definitions through `src/index.ts` via `src/core/tools/index.ts`.

## Exports from `src/core/tools/index.ts`

| Item | Purpose |
| --- | --- |
| `readToolDefinition` / `bashToolDefinition` / `editToolDefinition` / `writeToolDefinition` / `grepToolDefinition` / `findToolDefinition` / `lsToolDefinition` | Default tool definitions bound to `process.cwd()` |
| `readTool` / `bashTool` / `editTool` / `writeTool` / `grepTool` / `findTool` / `lsTool` | Default runtime tools (`AgentTool`) bound to `process.cwd()` |
| `createReadToolDefinition` / `createBashToolDefinition` / `createEditToolDefinition` / `createWriteToolDefinition` / `createGrepToolDefinition` / `createFindToolDefinition` / `createLsToolDefinition` | Factory methods for custom `cwd` / custom options |
| `createReadTool` / `createBashTool` / `createEditTool` / `createWriteTool` / `createGrepTool` / `createFindTool` / `createLsTool` | Factory methods returning wrapped `AgentTool` |
| `createCodingToolDefinitions` / `createReadOnlyToolDefinitions` | Convenience groups returning definitions |
| `createCodingTools` / `createReadOnlyTools` | Convenience groups returning wrapped tools |
| `codingTools` / `readOnlyTools` / `allTools` / `allToolDefinitions` | Prebuilt tool collections |
| `ToolsOptions` | `read` / `bash` options passed into group factories |

## Common helpers in `src/core/tools`

- `tool-definition-wrapper.ts`
  - `wrapToolDefinition(definition, ctxFactory?)` and `wrapToolDefinitions(definitions, ctxFactory?)`
  - `createToolDefinitionFromAgentTool(tool)` synthesizes a minimal definition view from `AgentTool`.
- `file-mutation-queue.ts`
  - `withFileMutationQueue(filePath, fn)` serializes concurrent writes to the same resolved file path.
- `truncate.ts`
  - Constants: `DEFAULT_MAX_LINES = 2000`, `DEFAULT_MAX_BYTES = 50 * 1024`, `GREP_MAX_LINE_LENGTH = 500`.
  - Functions: `truncateHead`, `truncateTail`, `truncateLine`, `formatSize`.
- `render-utils.ts`
  - Helper functions for rendering tool output in TUI (`shortenPath`, `str`, `replaceTabs`, `normalizeDisplayText`, `getTextOutput`, `invalidArgText`).
- `path-utils.ts`
  - Helpers resolving tool paths (`expandPath`, `resolveToCwd`, `resolveReadPath`) including macOS screenshot-name fallbacks.
- `edit-diff.ts`
  - Shared diff/matching engine for `edit`, including fuzzy matching helpers and conflict checks.

## Shared rendering behavior

- Most tools return `ToolDefinition` objects with:
  - `name`, `label`, `description`, optional `promptSnippet`, optional `promptGuidelines`
  - `parameters` based on `@sinclair/typebox`
  - `execute(...)` and optional TUI renderers (`renderCall`, `renderResult`)
- Text-like output generally uses truncation utilities and exposes details for follow-up actions (`truncation`, limit flags).

## `read` tool (`createReadToolDefinition`)

- **File**: `read.ts`
- **Schema**
  - `path: string` (required)
  - `offset?: number` (1-indexed start line)
  - `limit?: number`
- **Options**
  - `autoResizeImages?: boolean` (default: `true`)
  - `operations?: ReadOperations`
- **Operations (`ReadOperations`)**
  - `readFile(absolutePath): Promise<Buffer>`
  - `access(absolutePath): Promise<void>`
  - `detectImageMimeType?(absolutePath): Promise<string | null | undefined>`
- **Behavior**
  - Reads a file via `resolveReadPath(path, cwd)`.
  - Supports image files (`jpg/png/gif/webp`) and emits attachments.
  - Text output is truncated by `truncateHead` with defaults `DEFAULT_MAX_LINES` / `DEFAULT_MAX_BYTES`.
  - If output is truncated, returns `ReadToolDetails.truncation`.
  - `offset` beyond EOF throws.
  - First-line overrun emits a specific hint pointing to a `sed + head -c` fallback.
- **Factories/exports**
  - `createReadToolDefinition(cwd, options?)`
  - `createReadTool(cwd, options?)`
  - `readToolDefinition`, `readTool`
- **Type aliases**
  - `ReadToolInput`, `ReadToolDetails`, `ReadToolOptions`, `ReadOperations`

## `bash` tool (`createBashToolDefinition`)

- **File**: `bash.ts`
- **Schema**
  - `command: string` (required)
  - `timeout?: number` (seconds, optional)
- **Options**
  - `operations?: BashOperations`
  - `commandPrefix?: string`
  - `spawnHook?: (context) => BashSpawnContext`
- **Operations (`BashOperations`)**
  - `exec(command, cwd, { onData, signal, timeout, env }) : Promise<{ exitCode: number | null }>`
- **Behavior**
  - Command is executed in `cwd`; optional prefix and spawn hook can mutate context.
  - Streams command output and also keeps tail chunks for partial updates.
  - Output truncated by `truncateTail` (`DEFAULT_MAX_LINES` / `DEFAULT_MAX_BYTES`) for display.
  - If output exceeds thresholds, a temp file is created and `fullOutputPath` is returned in details (`BashToolDetails`).
  - Non-zero exit (`exitCode != 0`) and runtime errors reject with command output.
  - `"aborted"` and `"timeout:<seconds>"` are translated into readable errors.
- **Factories/exports**
  - `createBashToolDefinition(cwd, options?)`
  - `createBashTool(cwd, options?)`
  - `bashToolDefinition`, `bashTool`
  - `createLocalBashOperations`
- **Type aliases**
  - `BashToolInput`, `BashToolDetails`, `BashToolOptions`, `BashOperations`, `BashSpawnContext`, `BashSpawnHook`

## `edit` tool (`createEditToolDefinition`)

- **File**: `edit.ts`
- **Schema**
  - `path: string` (required)
  - `edits: Array<{ oldText: string; newText: string }>` (required, at least one)
- **Input compatibility**
  - `prepareEditArguments` accepts legacy `{ oldText, newText }` and converts to `edits`.
- **Options**
  - `operations?: EditOperations`
- **Operations (`EditOperations`)**
  - `readFile(absolutePath): Promise<Buffer>`
  - `writeFile(absolutePath, content)`
  - `access(absolutePath)`
- **Behavior**
  - Resolves path with `resolveToCwd`.
  - Applies edits against original file content (non-overlapping, unique old-text regions).
  - Uses `withFileMutationQueue` so writes to the same file are serialized.
  - Returns unified diff details (`EditToolDetails`) on success.
- **Factories/exports**
  - `createEditToolDefinition(cwd, options?)`
  - `createEditTool(cwd, options?)`
  - `editToolDefinition`, `editTool`
- **Type aliases**
  - `EditToolInput`, `EditToolDetails`, `EditToolOptions`, `EditOperations`

## `write` tool (`createWriteToolDefinition`)

- **File**: `write.ts`
- **Schema**
  - `path: string` (required)
  - `content: string` (required)
- **Options**
  - `operations?: WriteOperations`
- **Operations (`WriteOperations`)**
  - `writeFile(absolutePath, content)`
  - `mkdir(dir)`
- **Behavior**
  - Resolves path with `resolveToCwd`, creates parent directories, writes file (create or overwrite).
  - Uses `withFileMutationQueue` to serialize writes by target path.
  - Returns `content` with bytes written string.
- **Factories/exports**
  - `createWriteToolDefinition(cwd, options?)`
  - `createWriteTool(cwd, options?)`
  - `writeToolDefinition`, `writeTool`
- **Type aliases**
  - `WriteToolInput`, `WriteToolOptions`, `WriteOperations`

## `find` tool (`createFindToolDefinition`)

- **File**: `find.ts`
- **Schema**
  - `pattern: string` (required, glob)
  - `path?: string` (search root, default current dir)
  - `limit?: number` (default `1000`)
- **Options**
  - `operations?: FindOperations`
- **Operations (`FindOperations`)**
  - `exists(absolutePath): Promise<boolean> | boolean`
  - `glob(pattern, cwd, { ignore, limit }): Promise<string[]> | string[]`
- **Behavior**
  - Resolves path with `resolveToCwd`.
  - If `operations.glob` exists, uses it directly (suitable for remote implementations).
  - Otherwise downloads/uses local `fd` via `ensureTool("fd", true)` and runs with `.gitignore` handling.
  - Returns relative file paths, `/` suffix for directories.
  - Result output defaults to head-limit truncation behavior and can include:
    - `resultLimitReached`
    - `truncation`
- **Factories/exports**
  - `createFindToolDefinition(cwd, options?)`
  - `createFindTool(cwd, options?)`
  - `findToolDefinition`, `findTool`
- **Type aliases**
  - `FindToolInput`, `FindToolDetails`, `FindToolOptions`, `FindOperations`

## `grep` tool (`createGrepToolDefinition`)

- **File**: `grep.ts`
- **Schema**
  - `pattern: string` (required)
  - `path?: string` (default current dir)
  - `glob?: string`
  - `ignoreCase?: boolean`
  - `literal?: boolean`
  - `context?: number`
  - `limit?: number` (default `100`)
- **Options**
  - `operations?: GrepOperations`
- **Operations (`GrepOperations`)**
  - `isDirectory(absolutePath): Promise<boolean> | boolean`
  - `readFile(absolutePath): Promise<string> | string`
- **Behavior**
  - Resolves path with `resolveToCwd` and checks directory mode.
  - Runs local `rg` with `--json`, optional glob/case/literal/context flags.
  - Uses line-context formatting around each match and truncates long lines by `truncateLine(..., GREP_MAX_LINE_LENGTH)`.
  - Stops at `limit`, appends notices in result when limit reached.
- **Factories/exports**
  - `createGrepToolDefinition(cwd, options?)`
  - `createGrepTool(cwd, options?)`
  - `grepToolDefinition`, `grepTool`
- **Type aliases**
  - `GrepToolInput`, `GrepToolDetails`, `GrepToolOptions`, `GrepOperations`

## `ls` tool (`createLsToolDefinition`)

- **File**: `ls.ts`
- **Schema**
  - `path?: string` (directory, default current dir)
  - `limit?: number` (default `500`)
- **Options**
  - `operations?: LsOperations`
- **Operations (`LsOperations`)**
  - `exists(absolutePath): Promise<boolean> | boolean`
  - `stat(absolutePath): Promise<{ isDirectory(): boolean }> | { isDirectory(): boolean }`
  - `readdir(absolutePath): Promise<string[]> | string[]`
- **Behavior**
  - Validates directory existence/type, sorts alphabetically, marks directories with `/`.
  - Output may include byte truncation warning and `entryLimitReached`.
- **Factories/exports**
  - `createLsToolDefinition(cwd, options?)`
  - `createLsTool(cwd, options?)`
  - `lsToolDefinition`, `lsTool`
- **Type aliases**
  - `LsToolInput`, `LsToolDetails`, `LsToolOptions`, `LsOperations`

## Tool set constructors and collections

- `createCodingToolDefinitions(cwd, options?)` → `[read, bash, edit, write]` definitions
- `createReadOnlyToolDefinitions(cwd, options?)` → `[read, grep, find, ls]` definitions
- `createAllToolDefinitions(cwd, options?)` → record of all 7 definitions by tool name
- `createCodingTools(cwd, options?)` → `[read, bash, edit, write]` runtime tools
- `createReadOnlyTools(cwd, options?)` → `[read, grep, find, ls]` runtime tools
- `createAllTools(cwd, options?)` → record of all 7 runtime tools by name

## Notes

- Tool factories use `cwd` to resolve relative paths and are safe for SDK embeddings where runtime directory differs from process cwd.
- `read` and `bash` are the only tools exposed in `ToolsOptions` for the grouped factory methods (`read` for reading semantics, `bash` for command execution hooks).
