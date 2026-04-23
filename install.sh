#!/usr/bin/env bash
# shellcheck shell=bash
set -euo pipefail

REPO="jaimegago/skillhub"
BINARY="skillhub"

# ── OS detection ─────────────────────────────────────────────────────────────
case "$(uname -s)" in
  Darwin) OS="darwin" ;;
  Linux)  OS="linux"  ;;
  *)
    printf 'Unsupported OS: %s\nDownload manually: https://github.com/%s/releases\n' \
      "$(uname -s)" "$REPO" >&2
    exit 1
    ;;
esac

# ── Architecture detection ────────────────────────────────────────────────────
case "$(uname -m)" in
  x86_64)         ARCH="amd64" ;;
  aarch64|arm64)  ARCH="arm64" ;;
  *)
    printf 'Unsupported architecture: %s\nDownload manually: https://github.com/%s/releases\n' \
      "$(uname -m)" "$REPO" >&2
    exit 1
    ;;
esac

# ── Version resolution ────────────────────────────────────────────────────────
# Override: SKILLHUB_VERSION=v0.1.2 curl ... | bash
if [ -n "${SKILLHUB_VERSION:-}" ]; then
  TAG="$SKILLHUB_VERSION"
else
  printf 'Fetching latest release version...\n' >&2
  TAG="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | \
    grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
fi

# Normalize: TAG always has the v prefix (for release URLs);
# VERSION strips it to match goreleaser archive filenames (e.g. skillhub_0.1.0_darwin_arm64.tar.gz).
TAG="v${TAG#v}"
VERSION="${TAG#v}"

printf 'Installing %s %s (%s/%s)...\n' "$BINARY" "$TAG" "$OS" "$ARCH" >&2

# ── Download ──────────────────────────────────────────────────────────────────
ARCHIVE="${BINARY}_${VERSION}_${OS}_${ARCH}.tar.gz"
CHECKSUMS="${BINARY}_${VERSION}_checksums.txt"
BASE_URL="https://github.com/$REPO/releases/download/$TAG"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

printf 'Downloading %s...\n' "$ARCHIVE" >&2
curl -fsSL "$BASE_URL/$ARCHIVE"   -o "$TMP_DIR/$ARCHIVE"
curl -fsSL "$BASE_URL/$CHECKSUMS" -o "$TMP_DIR/$CHECKSUMS"

# ── Checksum verification ─────────────────────────────────────────────────────
# goreleaser produces sha256sum-format checksums: "<hash>  <filename>".
# Use awk to extract the expected hash for this exact filename.
printf 'Verifying checksum...\n' >&2
EXPECTED="$(awk -v name="$ARCHIVE" '$2 == name { print $1 }' "$TMP_DIR/$CHECKSUMS")"
if [ -z "$EXPECTED" ]; then
  printf 'Error: no checksum entry found for %s in %s\n' "$ARCHIVE" "$CHECKSUMS" >&2
  exit 1
fi

# sha256sum is Linux; shasum -a 256 is macOS (bash 3.2-compatible path).
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$TMP_DIR/$ARCHIVE" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "$TMP_DIR/$ARCHIVE" | awk '{print $1}')"
else
  printf 'Warning: sha256sum and shasum not found — skipping checksum verification\n' >&2
  ACTUAL="$EXPECTED"
fi

if [ "$ACTUAL" != "$EXPECTED" ]; then
  printf 'Checksum mismatch for %s\n  expected: %s\n  got:      %s\n' \
    "$ARCHIVE" "$EXPECTED" "$ACTUAL" >&2
  exit 1
fi

# ── Extract ───────────────────────────────────────────────────────────────────
tar -xzf "$TMP_DIR/$ARCHIVE" -C "$TMP_DIR"

# ── Install ───────────────────────────────────────────────────────────────────
# Override install directory with SKILLHUB_INSTALL_DIR; default is $HOME/.local/bin.
INSTALL_DIR="${SKILLHUB_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$INSTALL_DIR"
cp "$TMP_DIR/$BINARY" "$INSTALL_DIR/$BINARY"
chmod +x "$INSTALL_DIR/$BINARY"

# Final install line goes to stdout; all other progress goes to stderr.
printf 'Installed %s to %s/%s\n' "$BINARY" "$INSTALL_DIR" "$BINARY"

printf 'Hint: ensure %s is on your PATH\n' "$INSTALL_DIR" >&2
printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR" >&2
