# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
make build   # compile to bin/skillhub (CGO_ENABLED=0)
make test    # go test ./...
make lint    # golangci-lint run ./...
make run     # build + start MCP server on stdio
```

Single test:
```bash
go test -run TestDescribePlugin_MinimalManifest ./internal/tools
```

`bin/skillhub` is not tracked by git. Run `make build` after a fresh clone. For local development against an unreleased binary, register it with your MCP client using the absolute path: `claude mcp add skillhub -- /absolute/path/to/bin/skillhub mcp`. For released builds, the standard path is `claude mcp add skillhub -- skillhub mcp`.

## Architecture

skillhub is a standalone MCP server for authoring and maintaining Claude Code plugins, distributed as a prebuilt binary. It exposes 7 tools covering the plugin/skill lifecycle. **v0.1.0 status:** three tools implemented (`describe_plugin`, `list_available_plugins`, `check_drift`); four stubbed (`diff_skill`, `propose_skill_changes`, `search_plugins`, `recommend_plugins`).

**Request flow:**
1. The MCP client (Claude Code, Claude Desktop, or any stdio-capable MCP client) launches the binary via its configured `mcpServers` entry — e.g., `{"command": "skillhub", "args": ["mcp"]}`.
2. `internal/server/server.go` builds the MCP server on stdio and iterates `tools.Registry`.
3. Each tool registers via one of two mutually-exclusive paths (see below).
4. Handlers return structured JSON payloads using `internal/errors.SkillhubError` for failures.

**Hybrid tool registration (`internal/tools/registry.go`):**

The `Tool` struct has two mutually-exclusive fields:
- `Handler ToolHandler` — low-level path (stubs): caller provides schema + handler
- `Register func(*mcp.Server)` — generic path (implemented tools): SDK infers schema from `In` struct tags and marshals `Out` into structured content

The server checks `Register` first; falls back to `Handler`. Currently 4 tools are on the `Register` path (3 implemented + `diff_skill` stub); 3 remain on the `Handler` path (`propose_skill_changes`, `search_plugins`, `recommend_plugins`). When all 7 are on `Register`, delete `Handler` and the dispatch fork.

**`jsonschema` tag convention for generic-path tools** — the tag value is the field description string only, no `required,` prefix or `description=` key:
```go
type DescribePluginInput struct {
    Path string `json:"path" jsonschema:"Absolute path to the plugin root directory"`
}
```
Required is inferred from the field being non-pointer and lacking `omitempty`.

**Error handling pattern:**
- Success: return `nil` result + `nil` error (SDK marshals typed output)
- Failure: return a pre-built `*mcp.CallToolResult` with JSON-encoded `SkillhubError`, and `nil` error

**Configuration (`internal/config/config.go`):**
- Legacy plugin mode (`CLAUDE_PLUGIN_DATA` set): reads `$CLAUDE_PLUGIN_DATA/config.yaml` — a pre-v0.1.0 path retained for backward compatibility; not set in the standard standalone install
- Standalone: `$XDG_CONFIG_HOME/skillhub/config.yaml` (fallback `~/.config/skillhub/config.yaml`)
- Credentials are never stored in config — each `MarketplaceSource` names a `credentialEnvVar` read at invocation time
- Missing config file returns empty `Config`, not an error

**`describe_plugin` as the reference implementation** for the generic-path pattern. When implementing a stub: remove `notImplemented()`, define typed `Input`/`Output` structs, close over `mcp.AddTool[In, Out]` in the `Register` field, delete `Handler` and `InputSchema`, remove `//nolint` pragmas.

## Applicable Skills

- [dev-standards](~/.claude/skills/dev-standards/SKILL.md) — universal standards, apply to all repos
- [go-backend-standards](~/.claude/skills/go-backend-standards/SKILL.md) — Go patterns for this codebase
- [git-commit](~/.claude/skills/git-commit/SKILL.md) — conventional commit workflow
- [llm-docs-authoring](~/.claude/skills/llm-docs-authoring/SKILL.md) — editing this file
