#!/usr/bin/env bash
#
# Populate desktop/src-tauri/resources/ with the bundled runtime dependencies
# the Tauri app needs to run Claude Code tasks locally. Run once before
# `bun tauri build` (and again whenever the TypeScript Agent, Node,
# the unified Anban plugin, or ffmpeg change).
#
# macOS-only for now (matches the v1 target platform). Requires:
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
mkdir -p "$BIN_DIR" "$RES_DIR/anban"
rm -rf "$RES_DIR/claude"

# ---------------------------------------------------------------------------
# 1. TypeScript Agent and its production dependencies.
# ---------------------------------------------------------------------------
echo "==> Building TypeScript Agent…"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/agent"
cp "$REPO_ROOT/agent-ts/package.json" "$REPO_ROOT/agent-ts/package-lock.json" "$REPO_ROOT/agent-ts/tsconfig.json" "$STAGE/agent/"
cp -R "$REPO_ROOT/agent-ts/src" "$STAGE/agent/src"
( cd "$STAGE/agent" && npm ci && npm run build && npm prune --omit=dev )
rm -rf "$RES_DIR/agent"
mkdir -p "$RES_DIR/agent"
cp "$STAGE/agent/package.json" "$RES_DIR/agent/"
cp -R "$STAGE/agent/dist" "$STAGE/agent/node_modules" "$RES_DIR/agent/"
echo "    -> $RES_DIR/agent"

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
# 3. Unified Anban plugin → CLAUDE_PLUGIN_ROOT.
# ---------------------------------------------------------------------------
echo "==> Bundling Anban plugin…"
if [[ ! -d "$REPO_ROOT/harness/.claude-plugin" ]]; then
  echo "ERROR: harness is missing the Claude Code manifest." >&2
  exit 1
fi
rm -rf "$RES_DIR/anban"
cp -R "$REPO_ROOT/harness" "$RES_DIR/anban"
echo "    -> $RES_DIR/anban"

# ---------------------------------------------------------------------------
# 4. ffmpeg (optional; needed only for live-slicer / video work).
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
