# Changelog

All notable changes to this project will be documented in this file.

This project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

No unreleased changes yet.

## [0.1.0] - 2026-04-23

### Added

- `describe_plugin` tool: returns `plugin.json` metadata and enumerated component contents (skills, agents, commands, hooks, output styles) for a local plugin directory.
- `list_available_plugins` tool: fetches configured marketplace sources over HTTP and lists plugins not currently installed locally.
- `check_drift` tool: compares a locally-installed plugin against its declared marketplace upstream (via `x-skillhub-upstream` in `plugin.json`) and reports changed, added, and removed files using a sparse git clone.
- MCP server mode via `skillhub mcp` on stdio transport.
- Configuration via `config.yaml`; plugin mode reads from `$CLAUDE_PLUGIN_DATA`, standalone from `$XDG_CONFIG_HOME/skillhub/` (fallback `~/.config/skillhub/`).
- Pre-built binaries for darwin/linux × amd64/arm64 and windows/amd64 on GitHub Releases.
- Distribution via Homebrew tap (`jaimegago/skillhub`), Scoop bucket (`jaimegago/skillhub`), Unix install script (`install.sh`), PowerShell install script (`install.ps1`), and `go install`.
- Version metadata (`version`, `commit`, `date`) injected at build time via `-ldflags`.

### Known Limitations

- `diff_skill`, `propose_skill_changes`, `search_plugins`, and `recommend_plugins` return `NOT_IMPLEMENTED`; these tools are scaffolded but have no business logic yet.
- `npm` plugin source type is not supported in `check_drift`; affected plugins return `ErrUnsupportedSource`.
- Windows support is best-effort for v0.1.0; end-to-end testing on Windows has not been performed.
