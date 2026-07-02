#!/usr/bin/env bash
#
# Populate desktop/src-tauri/resources/ with the bundled runtime dependencies
# the Tauri app needs to run Claude Code tasks locally. Run once before
# `bun tauri build` (and again whenever the agent binary, Node, claude-code,
# the claudecode plugin, or ffmpeg change).
#
# macOS-only for now (matches the v1 target platform). Requires:
#   - Go toolchain (for `make agent-build-native`)
#   - Node.js (any recent LTS; bundled as-is — pin if claude-code demands it)
#   - ffmpeg (brew install ffmpeg) — optional but needed for live-slicer
#
# Usage:  bash desktop/populate-resources.sh
#
set -euo pipefail

# This script lives at <repo>/desktop/populate-resources.sh, so the repo root is
# one level up from its directory (NOT two — ../.. would escape the repo).
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RES_DIR="$REPO_ROOT/desktop/src-tauri/resources"
BIN_DIR="$RES_DIR/bin"

echo "==> Resource target: $RES_DIR"
mkdir -p "$BIN_DIR" "$RES_DIR/claude" "$RES_DIR/claudecode"

# ---------------------------------------------------------------------------
# 1. anban-creator-agent sidecar (native Go build for this host arch).
# ---------------------------------------------------------------------------
echo "==> Building anban-creator-agent (native)…"
( cd "$REPO_ROOT" && make agent-build-native )
AGENT_BIN=$(ls "$REPO_ROOT"/bin/anban-creator-agent-* 2>/dev/null | head -n1 || true)
if [[ -z "$AGENT_BIN" ]]; then
  echo "ERROR: agent build produced no binary under bin/." >&2
  exit 1
fi
cp "$AGENT_BIN" "$BIN_DIR/anban-creator-agent"
chmod +x "$BIN_DIR/anban-creator-agent"
echo "    -> $BIN_DIR/anban-creator-agent"

# ---------------------------------------------------------------------------
# 2. Node runtime (copy the dev machine's node binary; PATH is overridden at
#    runtime so only this node is used to spawn claude).
# ---------------------------------------------------------------------------
NODE_BIN="$(command -v node || true)"
if [[ -z "$NODE_BIN" ]]; then
  echo "ERROR: node not found on PATH. Install Node.js first." >&2
  exit 1
fi
cp "$NODE_BIN" "$BIN_DIR/node"
chmod +x "$BIN_DIR/node"
echo "    -> $BIN_DIR/node  ($(node --version))"

# ---------------------------------------------------------------------------
# 3. @anthropic-ai/claude-code (the `claude` CLI the SDK spawns). Pack the
#    installed package into resources/claude so it travels with the app.
# ---------------------------------------------------------------------------
echo "==> Bundling @anthropic-ai/claude-code…"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
( cd "$STAGE" && npm init -y >/dev/null 2>&1 && npm install "@anthropic-ai/claude-code" >/dev/null 2>&1 )
if [[ ! -d "$STAGE/node_modules/@anthropic-ai/claude-code" ]]; then
  echo "ERROR: could not install @anthropic-ai/claude-code." >&2
  exit 1
fi
rm -rf "$RES_DIR/claude"
cp -R "$STAGE/node_modules/@anthropic-ai/claude-code" "$RES_DIR/claude"
echo "    -> $RES_DIR/claude"

# ---------------------------------------------------------------------------
# 4. claudecode plugin (git submodule) → CLAUDE_PLUGIN_ROOT.
# ---------------------------------------------------------------------------
echo "==> Bundling claudecode plugin…"
( cd "$REPO_ROOT" && git submodule update --init --recursive >/dev/null 2>&1 || true )
if [[ ! -d "$REPO_ROOT/claudecode/.claude-plugin" ]]; then
  echo "ERROR: claudecode submodule missing or not checked out." >&2
  exit 1
fi
rm -rf "$RES_DIR/claudecode"
cp -R "$REPO_ROOT/claudecode" "$RES_DIR/claudecode"
echo "    -> $RES_DIR/claudecode"

# ---------------------------------------------------------------------------
# 5. ffmpeg (optional; needed only for live-slicer / video work).
# ---------------------------------------------------------------------------
FF_BIN="$(command -v ffmpeg || true)"
if [[ -n "$FF_BIN" ]]; then
  cp "$FF_BIN" "$BIN_DIR/ffmpeg"
  chmod +x "$BIN_DIR/ffmpeg"
  echo "    -> $BIN_DIR/ffmpeg"
else
  echo "WARN: ffmpeg not found on PATH — live-slicer/video tasks will be unavailable." >&2
  echo "      Install with: brew install ffmpeg" >&2
fi

echo ""
echo "==> Done. Resources populated under $RES_DIR"
echo "    Next: cd desktop && bun install && bun tauri build"
