# skillhub

A Claude Code plugin that ships an MCP server with tooling for authoring, inspecting, and maintaining Claude Code plugins and skills.

**Status**: scaffolding — all tools are stubbed and return `NOT_IMPLEMENTED`. Business logic lands one tool at a time.

## What it does

skillhub exposes MCP tools to Claude that cover the full plugin/skill lifecycle:

| Tool | Description |
|------|-------------|
| `check_drift` | Detect whether a local skill has diverged from its marketplace source |
| `diff_skill` | Unified diff between local and canonical skill version |
| `propose_skill_changes` | Open an MR against the marketplace source; supports `dry_run` |
| `list_available_plugins` | List uninstalled plugins from configured marketplace sources |
| `search_plugins` | Substring search across plugin metadata |
| `recommend_plugins` | Rank plugins by relevance to a free-text description |
| `describe_plugin` | Return plugin.json + bundled SKILL.md contents |

## Install as a Claude Code plugin

```
git clone https://github.com/jaime-gago/skillhub
cd skillhub
make build
claude --plugin-dir .
```

Once activated, run `/mcp` inside Claude Code to confirm the `skillhub` server and its tools appear.

> **Prerequisites / Troubleshooting**
>
> `bin/skillhub` is not tracked by git and does not exist after a fresh clone. Run `make build` before `claude --plugin-dir .` for the first time.
>
> If the plugin activates but tools do not appear in `/mcp`, confirm the binary exists and is executable:
> ```
> ls -l bin/skillhub
> chmod +x bin/skillhub   # if needed
> ```

## Configuration

skillhub reads `config.yaml` from:

- **Plugin mode** (`CLAUDE_PLUGIN_DATA` is set by Claude Code): `$CLAUDE_PLUGIN_DATA/config.yaml`
- **Standalone**: `$XDG_CONFIG_HOME/skillhub/config.yaml` (default: `~/.config/skillhub/config.yaml`)

Minimal config:

```yaml
marketplaceSources:
  - url: "https://github.com/example/skills-registry"
    gitHostType: github        # github | gitlab | generic
    credentialEnvVar: SKILLHUB_GITHUB_TOKEN
```

Credentials are read from the named environment variable at invocation time. The config file never stores secrets.

## Development

```
make build   # compile to bin/skillhub
make test    # run all tests
make lint    # run golangci-lint
make run     # build + start MCP server on stdio
```

Go 1.25+ required.

## Contributing

1. Fork and clone the repo.
2. Implement one tool at a time — pick any stub in `internal/tools/`.
3. Remove the `notImplemented()` call, add a real handler, define the input/output schema, and delete the `//nolint` pragmas if present.
4. `make test` and `make lint` must pass before opening a PR.

## License

Apache 2.0 — see [LICENSE](LICENSE).
