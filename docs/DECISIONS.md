## 2026-04-17 — MCP server config: inline in plugin.json, not a separate .mcp.json

**Context.** The Claude Code plugin reference lists `.mcp.json` at the plugin root as the default location for MCP server declarations, and also accepts inline `mcpServers` in `plugin.json`. We initially chose the separate-file form for separation of concerns.

**Problem.** When the plugin repo root is also a Claude Code project root (which it is for skillhub — you develop the plugin by running `claude --plugin-dir .` in the repo), Claude Code discovers `.mcp.json` twice: once by the plugin loader (where `${CLAUDE_PLUGIN_ROOT}` expands and the binary path resolves) and once by project-scope MCP discovery (where `${CLAUDE_PLUGIN_ROOT}` stays literal, the command path is invalid, and the server fails to start). The user sees a confusing red ✘ entry alongside the working plugin entry in `/mcp`.

**Decision.** Inline the `mcpServers` object directly in `.claude-plugin/plugin.json`. Do not create `.mcp.json` at the plugin root. The plugin schema documents `mcpServers` as accepting `string|array|object`; the inline object form is schema-valid and eliminates the file that project-scope discovery reads.

**Consequence.** `plugin.json` carries both metadata and runtime config. Acceptable — the manifest is the canonical source of truth for plugin behavior, and inlining keeps server config co-located with the metadata that identifies the plugin. If we ever need the plugin to run outside the Claude Code project-root collision (e.g., plugin inside a subdirectory that isn't a project root), a separate `.mcp.json` becomes safe again; revisit then.

**Verified against.** https://code.claude.com/docs/en/plugins-reference — "Plugin manifest schema" section, `mcpServers` row of the component path fields table.

## 2026-04-17 — Hybrid tool registration: Handler + Register fields

**Context.** The Go MCP SDK offers two registration APIs. The low-level path is `server.AddTool(*mcp.Tool, ToolHandler)` where the handler signature is `func(ctx, *CallToolRequest) (*CallToolResult, error)` and the input schema and output serialization are caller's responsibility. The generic path is `mcp.AddTool[In, Out](server, *mcp.Tool, ToolHandlerFor[In, Out])` where the handler is `func(ctx, *CallToolRequest, In) (*CallToolResult, Out, error)` and the SDK infers the input schema from the `In` struct's `jsonschema` tags and marshals `Out` into the result's `StructuredContent`.

**Problem.** In pass 1, all seven tools are stubs returning `NOT_IMPLEMENTED`. Stubs have no real input or output schema — forcing them onto the generic path means committing to types that haven't been designed. In pass 2+, tools migrate to real implementations one at a time, and the generic form is strictly better for those (inferred schema, validated input, structured output). We need both paths to coexist during the migration.

**Decision.** The `Tool` struct carries two mutually-exclusive fields: `Handler ToolHandler` for the low-level path, `Register func(*mcp.Server)` for the generic path. The server's registration loop checks `Register` first and calls it (which itself calls `mcp.AddTool[In, Out]`); otherwise it falls back to `server.AddTool(&Tool{...}, t.Handler)`. Stubs set `Handler` and leave `Register` nil. Implemented tools set `Register` and leave `Handler` nil. When all seven tools are migrated, delete `Handler` and the dispatch fork.

**Consequence.** Two fields on a struct that are mutually exclusive is a mild smell, but it's explicit, the migration path is clear, and `TestToolListMatchesRegistry` continues to work because the completeness check reads `Name` only. Worth it.

**`jsonschema` tag convention for generic-path tools.** Verified from the SDK's `jsonschema/infer.go` and SDK example code: the tag value is the field description string only. Do not prefix with `required,` or `description=`. Required is inferred from the field being non-pointer and lacking `omitempty`. Example:

    type DescribePluginInput struct {
        Path string `json:"path" jsonschema:"Absolute path to the plugin root directory"`
    }

**Verified against.** SDK source at github.com/modelcontextprotocol/go-sdk, specifically the generic handler type `ToolHandlerFor[In, Out]` and the schema inference in the `jsonschema` subpackage. Commit this decision along with the first generic migration (describe_plugin) so future tool implementations have a worked example in the repo.

## 2026-04-20 — Marketplace.json lives under .claude-plugin/, not at repo root

**Context.** When implementing the HTTP-only marketplace fetch (see entry below), the initial URL construction for github and gitlab host types appended `/marketplace.json` directly after the ref, placing the file at the repository root.

**Problem.** Live smoke test against `https://github.com/anthropics/claude-plugins-official` returned HTTP 404 for `https://raw.githubusercontent.com/anthropics/claude-plugins-official/main/marketplace.json`. The correct URL `https://raw.githubusercontent.com/anthropics/claude-plugins-official/main/.claude-plugin/marketplace.json` returns HTTP 200 with valid JSON.

**Decision.** URL construction for `github` and `gitlab` host types now includes the `.claude-plugin/` path segment before `marketplace.json`. The `generic` host type continues to use the caller-supplied URL verbatim, so non-standard marketplaces remain supported.

**Consequence.** Any existing config pointing a `github` or `gitlab` source at a repo whose `marketplace.json` actually lives at root would silently break — but no such deployed config exists in this project. New marketplace repositories must follow the `.claude-plugin/marketplace.json` convention, which matches the documented Claude Code layout for plugin repos.

**Verified against.** [Plugin Marketplaces docs](https://code.claude.com/docs/en/plugin-marketplaces) — the walkthrough shows `my-marketplace/.claude-plugin/marketplace.json` as the canonical path, and the spec states "Create `.claude-plugin/marketplace.json` in your repository root."

## 2026-04-17 — Marketplace resolution: HTTP-only fetch, no git clone

**Context.** `list_available_plugins`, `search_plugins`, and `recommend_plugins` all need to enumerate plugins from configured marketplace sources. Each marketplace is a git repository whose root contains a `marketplace.json` file. The natural retrieval mechanism is `git clone`, but that requires git on PATH and produces a full working tree.

**Decision.** Fetch `marketplace.json` via raw HTTP only. GitHub sources are transformed to `raw.githubusercontent.com` URLs; GitLab sources to `/-/raw/` URLs; `generic` sources treat the configured URL as the direct URL to `marketplace.json`. No git subprocess is invoked. Credentials are applied as `Authorization: Bearer` headers from the configured `credentialEnvVar`. Responses are cached under `config.CacheDir()/marketplaces/{8-hex-key}/` with a 1-hour TTL; stale cache is used as a fallback on fetch failure.

**Consequence.** This is correct for read-only discovery (list, search, recommend) where only the index file is needed. It does not work for tools that need per-plugin file trees (e.g., pulling a plugin's `skills/` directory for deep inspection or installation). If such a tool is added, revisit: options include sparse checkout via `git` subprocess or fetching individual files via the host's API (GitHub tree API, etc.). Do not attempt git-free tree traversal for that case — it undercuts maintainability.
