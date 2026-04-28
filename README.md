# skillhub

An MCP server that provides tooling for authoring, inspecting, and maintaining Claude Code plugins and skills.

## What it does

skillhub exposes MCP tools to Claude covering the full plugin/skill lifecycle.

| Tool | Description |
|------|-------------|
| `describe_plugin` | Return plugin.json metadata and enumerated component contents for a local plugin directory |
| `list_available_plugins` | Fetch configured marketplace sources and list available plugins |
| `check_drift` | Compare a locally-installed plugin against its declared marketplace upstream and report changed, added, and removed files |
| `diff_skill` | Unified diff between the local version of a skill and its canonical marketplace version |
| `search_plugins` | Case-insensitive substring search across plugin names, descriptions, keywords, and categories |
| `recommend_plugins` | Rank plugins by relevance to a free-text description of a task or need |
| `propose_skill_changes` | Open a pull request against the marketplace source proposing local skill changes; supports `dry_run` |

## Install

### Homebrew (macOS / Linux)

```bash
brew tap jaimegago/skillhub
brew install skillhub
```

### Scoop (Windows)

```powershell
scoop bucket add skillhub https://github.com/jaimegago/scoop-skillhub
scoop install skillhub
```

### Install script (Unix)

```bash
curl -fsSL https://raw.githubusercontent.com/jaimegago/skillhub/main/install.sh | bash
```

### Install script (PowerShell)

```powershell
iwr https://raw.githubusercontent.com/jaimegago/skillhub/main/install.ps1 | iex
```

### go install

```bash
go install github.com/jaimegago/skillhub/cmd/skillhub@v0.1.0
```

### Build from source

```bash
git clone https://github.com/jaimegago/skillhub
cd skillhub
make build
make install
```

`make install` copies the binary to `$GOBIN` if set, otherwise `$HOME/go/bin`. Ensure that directory is on `$PATH`.

## Use with an MCP client

skillhub speaks the Model Context Protocol (MCP) over stdio. Any MCP client can use it after installing the binary.

### Claude Code

Register skillhub as an MCP server in Claude Code:

```bash
claude mcp add skillhub -- skillhub mcp
```

### Claude Desktop

Edit `claude_desktop_config.json` and add a server entry:

```json
{
  "mcpServers": {
    "skillhub": {
      "command": "skillhub",
      "args": ["mcp"]
    }
  }
}
```

Config file locations:
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

### Other MCP clients

Any MCP client that supports stdio servers works the same way — point it at the `skillhub` binary with `mcp` as the argument.

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

```bash
make build    # compile to bin/skillhub
make test     # run all tests
make lint     # run golangci-lint
make run      # build + start MCP server on stdio
make install  # install binary to $GOPATH/bin
make help     # list all targets
```

Go 1.25+ required.

See [CHANGELOG.md](CHANGELOG.md) for release history.

Contributions are welcome. Pick a tool in `internal/tools/`, add a feature or fix, write tests, and open a PR. `make test` and `make lint` must pass.

## License

Apache 2.0 — see [LICENSE](LICENSE).
