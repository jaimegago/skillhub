# skillhub

A Claude Code plugin that ships an MCP server with tooling for authoring, inspecting, and maintaining Claude Code plugins and skills.

**Status**: three tools implemented (`check_drift`, `list_available_plugins`, `describe_plugin`); four return `NOT_IMPLEMENTED` stubs (`diff_skill`, `propose_skill_changes`, `search_plugins`, `recommend_plugins`).

## What it does

skillhub exposes MCP tools to Claude covering the full plugin/skill lifecycle.

**Implemented**

| Tool | Description |
|------|-------------|
| `describe_plugin` | Return plugin.json metadata and enumerated component contents for a local plugin directory |
| `list_available_plugins` | Fetch configured marketplace sources and list plugins not currently installed locally |
| `check_drift` | Compare a locally-installed plugin against its declared marketplace upstream and report changed, added, and removed files |

**Stubbed (not yet implemented)**

| Tool | Description |
|------|-------------|
| `diff_skill` | Unified diff between local and canonical skill version |
| `propose_skill_changes` | Open an MR against the marketplace source; supports `dry_run` |
| `search_plugins` | Substring search across plugin metadata |
| `recommend_plugins` | Rank plugins by relevance to a free-text description |

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

## Claude Code plugin usage

`plugin.json` resolves the `skillhub` binary from `$PATH`. Any install method that places the binary on `$PATH` enables plugin use without a separate build step.

**Binary installs (Homebrew, Scoop, install scripts):** the binary is on `$PATH` after installation. Clone this repo to get `plugin.json`, then activate:

```bash
git clone https://github.com/jaimegago/skillhub
claude --plugin-dir skillhub
```

**Build from source:** run `make install` after `make build` to put the binary on `$PATH`, then:

```bash
claude --plugin-dir .
```

Once activated, run `/mcp` inside Claude Code to confirm the `skillhub` server and its tools appear.

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

Contributions are welcome. Implement one stub at a time: pick any tool in `internal/tools/`, remove the `notImplemented()` call, define typed input/output structs, write tests, and open a PR. `make test` and `make lint` must pass.

## License

Apache 2.0 — see [LICENSE](LICENSE).
