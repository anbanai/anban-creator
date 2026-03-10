#!/bin/bash
# Auto-download/update anbanwriter binary from GitHub releases.
# Designed to run in background from SessionStart hook.
# All output goes to bin/.bootstrap.log; failures are silent.

set -euo pipefail

REPO="royalrick/anbanwriter"
ROOT="${CLAUDE_PLUGIN_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
BIN_DIR="$ROOT/bin"
LOG="$BIN_DIR/.bootstrap.log"
VERSION_FILE="$BIN_DIR/.version"

mkdir -p "$BIN_DIR"
exec > "$LOG" 2>&1

# Detect platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac

BINARY_NAME="anbanwriter-${OS}-${ARCH}"
[ "$OS" = "windows" ] && BINARY_NAME="${BINARY_NAME}.exe"

# Get latest version from GitHub API
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
if [ -z "$LATEST" ]; then
  echo "Failed to fetch latest version"
  exit 0
fi

echo "Latest version: $LATEST"

# Compare with local version
if [ -f "$VERSION_FILE" ] && [ "$(cat "$VERSION_FILE")" = "$LATEST" ] && [ -x "$BIN_DIR/anbanwriter" ]; then
  echo "Already up to date"
  exit 0
fi

# Download
URL="https://github.com/${REPO}/releases/download/${LATEST}/${BINARY_NAME}"
echo "Downloading $URL ..."
curl -fsSL "$URL" -o "$BIN_DIR/anbanwriter.new"
chmod +x "$BIN_DIR/anbanwriter.new"

# Atomic replace
mv -f "$BIN_DIR/anbanwriter.new" "$BIN_DIR/anbanwriter"
echo "$LATEST" > "$VERSION_FILE"
echo "Updated to $LATEST"
