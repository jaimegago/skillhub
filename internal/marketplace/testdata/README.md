# testdata fixtures

## official_snapshot.json

Snapshot of the Anthropic official plugin marketplace index, fetched from:

    https://raw.githubusercontent.com/anthropics/claude-plugins-official/main/.claude-plugin/marketplace.json

Contains 5 plugins as of 2026-04-20. To refresh, fetch the URL above and replace the file.
Note: the repo-root path (`/main/marketplace.json`) returns 404; the `.claude-plugin/` path is the correct location.

## valid.json / minimal.json / malformed.json

Synthetic fixtures used by unit tests. Not derived from any live source.
