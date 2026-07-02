#!/bin/bash
# Auto-download/update Anban Creator binaries from GitHub releases.
# Designed to run from plugin install scripts or in background from SessionStart hooks.
# All output goes to bin/.bootstrap.log; failures are silent.

set -euo pipefail

REPO="anbanai/anban-creator"
ROOT="${ANBAN_PLUGIN_ROOT:-${CLAUDE_PLUGIN_ROOT:-${PLUGIN_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}}}"
BIN_DIR="$ROOT/bin"
LOG="$BIN_DIR/.bootstrap.log"
VERSION_FILE="$BIN_DIR/.version"

mkdir -p "$BIN_DIR"
exec > "$LOG" 2>&1

# Detect platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$OS" in
  mingw*|msys*|cygwin*) OS="windows" ;;
esac
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac

case "$OS/$ARCH" in
  linux/amd64|linux/arm64|darwin/amd64|darwin/arm64|windows/amd64) ;;
  *)
    echo "Unsupported platform: $OS/$ARCH"
    exit 0
    ;;
esac

SERVER_ASSET="anban-creator-server-${OS}-${ARCH}"
AGENT_ASSET="anban-${OS}-${ARCH}"
SERVER_DEST="$BIN_DIR/anban-creator-server"
AGENT_DEST="$BIN_DIR/anban"
if [ "$OS" = "windows" ]; then
  SERVER_ASSET="${SERVER_ASSET}.exe"
  AGENT_ASSET="${AGENT_ASSET}.exe"
  SERVER_DEST="${SERVER_DEST}.exe"
  AGENT_DEST="${AGENT_DEST}.exe"
fi

# Get latest version from GitHub API
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')
if [ -z "$LATEST" ]; then
  echo "Failed to fetch latest version"
  exit 0
fi

echo "Latest version: $LATEST"

# Compare with local version and both binaries. bin/anban is the plugin-local CLI.
CURRENT_VERSION=""
if [ -f "$VERSION_FILE" ]; then
  CURRENT_VERSION="$(cat "$VERSION_FILE")"
fi

if [ "$CURRENT_VERSION" = "$LATEST" ] && [ -x "$SERVER_DEST" ] && [ -x "$AGENT_DEST" ]; then
  echo "Already up to date"
  exit 0
fi

install_asset() {
  local asset="$1"
  local dest="$2"
  local url="https://github.com/${REPO}/releases/download/${LATEST}/${asset}"
  local tmp="${dest}.new.$$"

  echo "Downloading $url ..."
  curl -fsSL "$url" -o "$tmp"
  chmod +x "$tmp"
  mv -f "$tmp" "$dest"
  echo "Installed $dest"
}

if [ "$CURRENT_VERSION" != "$LATEST" ] || [ ! -x "$SERVER_DEST" ]; then
  install_asset "$SERVER_ASSET" "$SERVER_DEST"
fi

if [ "$CURRENT_VERSION" != "$LATEST" ] || [ ! -x "$AGENT_DEST" ]; then
  install_asset "$AGENT_ASSET" "$AGENT_DEST"
fi

echo "$LATEST" > "$VERSION_FILE"
echo "Updated to $LATEST"
