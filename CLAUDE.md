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

`bin/skillhub` is not tracked by git. Run `make build` after a fresh clone before using `claude --plugin-dir .`.

## Architecture

skillhub is a Claude Code plugin that exposes an MCP server (`bin/skillhub mcp`) with 7 tools covering the plugin/skill lifecycle. **Current status: scaffolding.** Only `describe_plugin` is fully implemented; the other 6 tools return `NOT_IMPLEMENTED` stubs.

**Request flow:**
1. Claude Code starts the binary via `mcpServers` in `.claude-plugin/plugin.json` (intentionally inlined — not a separate `.mcp.json`, to avoid double-discovery when the repo root is also the Claude project root).
2. `internal/server/server.go` builds the MCP server on stdio and iterates `tools.Registry`.
3. Each tool registers via one of two mutually-exclusive paths (see below).
4. Handlers return structured JSON payloads using `internal/errors.SkillhubError` for failures.

**Hybrid tool registration (`internal/tools/registry.go`):**

The `Tool` struct has two mutually-exclusive fields:
- `Handler ToolHandler` — low-level path (stubs): caller provides schema + handler
- `Register func(*mcp.Server)` — generic path (implemented tools): SDK infers schema from `In` struct tags and marshals `Out` into structured content

The server checks `Register` first; falls back to `Handler`. When all 7 tools are migrated, delete `Handler` and the dispatch fork.

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
- Plugin mode (`CLAUDE_PLUGIN_DATA` set): reads `$CLAUDE_PLUGIN_DATA/config.yaml`
- Standalone: `$XDG_CONFIG_HOME/skillhub/config.yaml` (fallback `~/.config/skillhub/config.yaml`)
- Credentials are never stored in config — each `MarketplaceSource` names a `credentialEnvVar` read at invocation time
- Missing config file returns empty `Config`, not an error

**`describe_plugin` as the reference implementation** for the generic-path pattern. When implementing a stub: remove `notImplemented()`, define typed `Input`/`Output` structs, close over `mcp.AddTool[In, Out]` in the `Register` field, delete `Handler` and `InputSchema`, remove `//nolint` pragmas.

## Applicable Skills

- [dev-standards](~/.claude/skills/dev-standards/SKILL.md) — universal standards, apply to all repos
- [go-backend-standards](~/.claude/skills/go-backend-standards/SKILL.md) — Go patterns for this codebase
- [git-commit](~/.claude/skills/git-commit/SKILL.md) — conventional commit workflow
- [llm-docs-authoring](~/.claude/skills/llm-docs-authoring/SKILL.md) — editing this file
