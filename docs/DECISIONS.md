## 2026-04-23 — v0.1.0 distribution: binary-first, $PATH-resolved plugin command

**Context.** v0.1.0 is the first public release. Code is ready: 3 of 7 tools are implemented (`describe_plugin`, `list_available_plugins`, `check_drift`), the test suite passes, and the MCP server works end-to-end. The release needs a distribution model that serves two distinct audiences: binary users (install via Homebrew, Scoop, or an install script, no repo clone required) and developer users (clone + build from source, iterate on the code).

**Problem.**

Before this decision, `plugin.json` declared:

    "command": "${CLAUDE_PLUGIN_ROOT}/bin/skillhub"

That path requires a compiled binary inside the cloned plugin directory. Binary users who install via Homebrew, Scoop, or an install script do not have the repo cloned (and if they do, they have no reason to run `make build` inside it). They cannot activate skillhub as a Claude Code plugin.

The Claude Code plugin schema accepts a single `command` string. There is no fallback, no alternative path, and no platform-conditional syntax. (Verified against the Claude Code plugin reference this session.) Supporting both resolution paths in a single `plugin.json` is not possible.

**Decision.**

*Distribution.* Pre-built binaries for darwin, linux, and windows × amd64/arm64 (arm64 where applicable) are shipped on GitHub Releases and distributed via:

- Homebrew tap: `jaimegago/homebrew-skillhub`
- Scoop bucket: `jaimegago/scoop-skillhub`
- Unix install script: `install.sh` (curl|bash)
- PowerShell install script: `install.ps1` (iwr|iex)
- `go install github.com/jaimegago/skillhub/cmd/skillhub@v0.1.0`

All pre-built distribution is driven by goreleaser on tag push (goreleaser config added in a follow-up).

*`plugin.json` command.* Changed to `"skillhub"` — a bare binary name, resolved via `$PATH`. Every distribution method above places `skillhub` on `$PATH` automatically. Clone users must run `make install` once before `claude --plugin-dir .`; this is documented in the README and in `make help`.

**Consequence.**

Binary users get fully frictionless plugin activation: install the binary via any method above, clone the repo for `plugin.json`, and point `--plugin-dir` at the clone.

Clone users need one extra step (`make install`) before `claude --plugin-dir .` works. This is a one-time setup cost, explicitly documented.

`$PATH` collisions are unlikely given the name `skillhub`; a collision would silently shadow the installed binary. Acceptable tradeoff — no realistic collision surface exists at v0.1.0.

Windows support is best-effort for v0.1.0. The MCP server logic and git subprocess calls include `-c core.autocrlf=false -c core.eol=lf` to prevent line-ending drift, but end-to-end testing on Windows has not been performed. Revisit for v0.2.0.

Revisit this decision if: (a) Claude Code gains a first-class way to resolve plugin binaries from a shared install location without requiring `$PATH`; (b) the plugin manifest schema adds fallback or platform-conditional command syntax; (c) we need to ship a non-stdio transport where the binary is not the execution entry point.

**Verified against.** Claude Code plugin reference (fetched this session) — `mcpServers.command` is a single string with no fallback or conditional syntax documented. Goreleaser documentation not consulted at decision time; goreleaser config lands in a follow-up prompt.

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

## 2026-04-21 — Per-plugin fetch: git subprocess with sparse shallow partial clone

**Context.** `check_drift`, and future `diff_skill` / install flows, need the actual file tree of one plugin from one marketplace entry, not just the marketplace index. The prior decision (HTTP-only marketplace fetch) explicitly deferred this and listed two options: (a) sparse checkout via `git` subprocess, (b) fetching individual files via each host's API (GitHub Contents API, GitLab Repository Files API, etc.).

**Decision.** Use `git` subprocess. The canonical fetch for a plugin subtree is:

    git clone --filter=blob:none --sparse --depth=1 <repo-url> <cache-dir>
    git -C <cache-dir> sparse-checkout set <path-within-repo>

For `ref` / `sha` pinning, `--branch <ref>` is appended to `clone` when a ref is present; when a sha is specified, the clone is followed by `git -C <cache-dir> checkout <sha>` (which requires unshallow-ing or an initial deeper fetch — handled in the fetch package).

**Why git subprocess over per-host HTTP APIs.**

*Uniform surface across plugin source types.* The marketplace schema lists five plugin source kinds: relative path (resolved inside the cloned marketplace repo), `github`, `url`, `git-subdir`, and `npm`. The first four all reduce to "clone a repo and check out a path." Per-host HTTP APIs would need a separate adapter for each host plus a traversal strategy per source kind; `generic` URL-type sources have no API at all.

*Rate limit arithmetic.* GitHub unauthenticated API and `raw.githubusercontent.com` share a 60/hr bucket (verified 2026-04-21 against github.blog changelog dated 2025-05-08). A plugin tree with 30 files per host-API walk exhausts the budget in two checks. Authenticated at 5000/hr is workable but pushes a PAT requirement onto every skillhub user for public-marketplace operations — a regression compared to the git path, which uses existing credential helpers only for private repos.

*Correctness.* Drift detection compares file bytes between local and upstream. Git checkout produces the exact committed bytes. Host HTTP APIs sometimes apply transformations (CRLF normalization on some endpoints, for instance) — unverified for each endpoint we'd need, and the cost of being wrong is silent false-positive drift. Git sidesteps this entire class of concern.

*Parity with Claude Code.* Claude Code itself uses `git` for plugin installation, including a `git-subdir` plugin source type that is explicitly a sparse partial clone. Anyone running Claude Code already has git. Adopting the same dependency is free.

**Verification.** Ran the canonical sequence live against `https://github.com/anthropics/claude-plugins-official.git` on 2026-04-21 with `git 2.43.0`. A sparse shallow partial clone followed by `sparse-checkout set plugins/agent-sdk-dev` materialized exactly that subtree. Total on-disk footprint: 262 KB. The three-flag combination (`--filter=blob:none --sparse --depth=1`) works as documented.

**Cache layout.**

    {CacheDir()}/plugin-trees/{8-hex-of-repo-url}/{ref-or-sha}/
      .git/                  (the sparse clone)
      <materialized subtree>

Cache is TTL-based (1h, matching marketplaces) when the source specifies a ref (branch/tag); when a full-length sha is specified, cache is immutable and reused indefinitely. `refresh=true` on the tool input bypasses TTL.

**Credentials.** For private repos, the `credentialEnvVar` on the marketplace source is propagated as an HTTPS basic-auth token via `git -c credential.helper='!f() { echo "username=x-access-token"; echo "password=$TOKEN"; }; f'` or equivalent — concrete mechanism deferred to implementation. SSH URLs rely on the host's `ssh-agent` and `known_hosts`, same as Claude Code.

**Windows / line endings.** All git invocations will include `-c core.autocrlf=false -c core.eol=lf` to prevent silent content transformation that would produce spurious drift on Windows hosts. Tested on Linux only for v1; Windows parity is a v2 concern.

**Consequence / trigger to revisit.**
- If skillhub needs to ship to an environment where `git` is genuinely unavailable (some air-gapped LGT workstations, sandboxed serverless runtimes), the correct replacement is either a vendored libgit2 binding or a pre-built marketplace snapshot bundled at build time — **not** per-host HTTP adapters. The rate limit and correctness arguments above get worse, not better, with scale.
- If drift becomes a hot path (hundreds of checks per minute), revisit the cache eviction policy and consider pre-warming known-upstream clones.
- `npm` plugin source type is explicitly out of scope for drift in v1: the tool returns `ErrUnsupportedSource` when it encounters one.

**Verified against.** [Plugin Marketplaces docs](https://code.claude.com/docs/en/plugin-marketplaces) — plugin source schema (relative/github/url/git-subdir/npm), `git-subdir` sparse-partial-clone behavior, `CLAUDE_CODE_PLUGIN_GIT_TIMEOUT_MS`. [GitHub rate limit docs](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api) — 60/hr unauthenticated, 5000/hr PAT. [GitHub blog changelog 2025-05-08](https://github.blog/changelog/2025-05-08-updated-rate-limits-for-unauthenticated-requests/) — `raw.githubusercontent.com` shares the unauthenticated bucket. Live `git clone --filter=blob:none --sparse --depth=1 && git sparse-checkout set` smoke test on 2026-04-21.

## 2026-04-21 — Local plugin upstream declaration: `x-skillhub-upstream` field in plugin.json

**Context.** `check_drift` needs to know, for a locally-installed plugin, which marketplace entry to compare against. Two surfaces were considered: (a) derive upstream from Claude Code's on-disk installation records at `~/.claude/plugins/cache/`, (b) require the user to pass marketplace + plugin name into every drift-check invocation, (c) declare upstream as a skillhub-specific extension field inside the local plugin's own `plugin.json`.

**Decision.** Option (c). Local plugins that want to participate in drift tracking declare an `x-skillhub-upstream` object in their `plugin.json`:

    {
      "name": "my-plugin",
      "version": "1.2.3",
      "x-skillhub-upstream": {
        "marketplace": "anthropic-official",
        "plugin": "code-review"
      }
    }

The `x-` prefix follows the widely-recognized convention (OpenAPI, Kubernetes CRDs, Docker Compose) for vendor-extension fields in schemas: it marks the key as "not part of the host spec, won't collide with future core additions." The skillhub qualifier makes it unambiguous which tool reads this field, allowing other MCP servers to coexist with their own `x-*` extensions on the same manifest.

`marketplace` is the marketplace `name` as it appears in the configured `marketplaceSources` (the same name that shows up as `Marketplace` in `list_available_plugins` output). `plugin` is the plugin entry name within that marketplace. Both are strings; unknown marketplaces surface as `ErrMarketplaceNotConfigured`, unknown plugin entries as `ErrPluginNotFound` scoped to the marketplace.

**Why not option (a).** Coupling skillhub to Claude Code's internal on-disk cache layout creates a silent breakage surface every time Claude Code reorganizes that directory. skillhub is an MCP server that happens to be distributed as a Claude Code plugin; it should not depend on Claude Code's private state.

**Why not option (b).** Works for one-off invocations but turns every repeated drift check into a lookup-and-retype exercise. The upstream relationship is a property of the local plugin's identity, not of the drift query; storing it with the plugin is the right locality.

**Consequence.** The `x-skillhub-upstream` field is a skillhub extension, not a Claude Code core schema field. Claude Code's plugin loader ignores unknown top-level fields in `plugin.json` (the schema is additive), so declaring `x-skillhub-upstream` is safe for plugins that also need to load under Claude Code. `check_drift` tolerates the field being absent — the status is `missing-upstream` at the plugin level, not an error.

**Input override.** `check_drift` input also accepts an optional `marketplace` + `plugin` override pair; when both are provided, the `x-skillhub-upstream` field in `plugin.json` is ignored for that invocation. Useful for testing a local plugin against an arbitrary upstream without modifying its manifest.

**Verified against.** [Plugin manifest schema](https://code.claude.com/docs/en/plugins-reference) — top-level fields are an open set; unrecognized fields do not fail validation. (From training: schema is JSON-Schema-based and does not use `additionalProperties: false` at the root. Unverified this session; will confirm if the implementation shows a validation failure.)

## 2026-04-22 — output_styles frontmatter shape: name + description only, inferred from convention

**Context.** `enumerateOutputStyles` in `internal/tools/describe_plugin.go` needs a frontmatter schema to parse output-style files. The [Claude Code plugin reference](https://code.claude.com/docs/en/plugins-reference) documents `output_styles` as a component type with default directory `output-styles/` and manifest key `outputStyles` (`string|array`), but the reference gives no frontmatter field schema — no field names are listed and no example frontmatter is shown.

**Research.** Sparse-cloned all 33 plugins from `github.com/anthropics/claude-plugins-public` via `internal/fetch` to look for a real-world output-style file. Zero plugins had an `output-styles/` directory. The two plugins named as if they would — `explanatory-output-style` and `learning-output-style` — implement their behavior entirely via hooks, not via markdown files with frontmatter.

**Decision.** `enumerateOutputStyles` parses frontmatter with `name` and `description` fields only, matching `enumerateAgents` byte-for-byte. The shape is inferred from convention: every other markdown-frontmatter component in the Claude Code plugin system (skills, agents, commands) uses `name` + `description`, and no real-world counterexample exists to contradict it.

**Consequence.** If a real-world output-style file appears in any marketplace and carries frontmatter fields beyond `name` and `description`, `enumerateOutputStyles` will silently drop those fields. Not a correctness failure — the enumeration returns partial but valid metadata — but a completeness gap. Revisit when: (a) the Claude Code reference adds an explicit output-styles frontmatter schema, or (b) a plugin ships a real `output-styles/` directory with markdown files we can inspect.

**Verified against.** [Claude Code plugin reference](https://code.claude.com/docs/en/plugins-reference) as of 2026-04-22 — `output_styles` is listed as a component type but no frontmatter schema is documented. Sparse-clone of all 33 plugins in `github.com/anthropics/claude-plugins-public` on 2026-04-22 — zero `output-styles/` directories exist across all 33 plugins.
