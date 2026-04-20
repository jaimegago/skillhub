# Prompt: Fix marketplace.json URL construction and document .claude-plugin/ convention

Generated: 2026-04-20
Model: claude-sonnet-4-6
Target: internal/marketplace/marketplace.go, internal/marketplace/marketplace_test.go, internal/marketplace/testdata/README.md, docs/DECISIONS.md

## Specification

Fix URL construction in `internal/marketplace/marketplace.go` and update related tests and documentation to reflect the Claude Code plugin-marketplace convention that `marketplace.json` lives under `.claude-plugin/` in the repository root, not at the repo root.

### Fix 1 — URL construction in rawURL()

For `gitHostType "github"`: the URL pattern must be `https://raw.githubusercontent.com/{owner}/{repo}/{ref}/.claude-plugin/marketplace.json`.

For `gitHostType "gitlab"`: the URL pattern must be `https://gitlab.com/{owner}/{repo}/-/raw/{ref}/.claude-plugin/marketplace.json`.

For `gitHostType "generic"`: no change. The caller provides the full URL and is responsible for pointing it at `marketplace.json` wherever it lives.

Update the doc comment on `rawURL` to document this convention.

### Fix 2 — Test assertions

Update `TestRawURL_GitHub`, `TestRawURL_GitHubDefaultRef`, and `TestRawURL_GitLab` in `internal/marketplace/marketplace_test.go` to assert the `.claude-plugin/` segment in the constructed URL. Do not change `TestRawURL_Generic`.

### Fix 3 — Snapshot fixture provenance

Add a `README.md` in `internal/marketplace/testdata/` recording where `official_snapshot.json` came from. The source URL is `https://raw.githubusercontent.com/anthropics/claude-plugins-official/main/.claude-plugin/marketplace.json`. Note that the repo-root path returns 404 — the `.claude-plugin/` path is the correct location. Document the synthetic fixtures (`valid.json`, `minimal.json`, `malformed.json`) as unit-test-only, not derived from any live source.

### Fix 4 — DECISIONS.md entry

Add a new decision entry dated 2026-04-20. Title: "Marketplace.json lives under .claude-plugin/, not at repo root." The entry must cover:
- Context: initial URL construction appended `/marketplace.json` directly after the ref.
- Problem: live smoke test against `anthropics/claude-plugins-official` returned HTTP 404 at the root path; the `.claude-plugin/` path returns HTTP 200.
- Decision: github and gitlab host types now include `.claude-plugin/` in the path; generic type is unchanged.
- Consequence: any existing config pointing a github/gitlab source at a repo with root-level marketplace.json would break, but no such config exists in this project.
- Verification: cite the Claude Code plugin-marketplaces docs, which show `my-marketplace/.claude-plugin/marketplace.json` as the canonical path.

### Constraints

Do not change the tool handler, cache logic, config schema, or any file outside `marketplace.go`, `marketplace_test.go`, `testdata/`, and `DECISIONS.md`. Run `go test ./...` and `make build` to verify after changes.
