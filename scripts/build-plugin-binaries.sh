#!/usr/bin/env bash
# Build the anban CLI into each plugin distribution as a fixed bundled binary.

set -euo pipefail

TARGET_GOOS="${PLUGIN_GOOS:-${GOOS:-$(go env GOOS)}}"
TARGET_GOARCH="${PLUGIN_GOARCH:-${GOARCH:-$(go env GOARCH)}}"
EXT=""
if [[ "$TARGET_GOOS" == "windows" ]]; then
  EXT=".exe"
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

OUT="$TMPDIR/anban$EXT"
echo "Building anban for ${TARGET_GOOS}/${TARGET_GOARCH}..."
CGO_ENABLED=0 GOOS="$TARGET_GOOS" GOARCH="$TARGET_GOARCH" go build -trimpath -o "$OUT" ./agent

for plugin in claudecode codex; do
  mkdir -p "$plugin/bin"
  cp "$OUT" "$plugin/bin/anban$EXT"
  chmod +x "$plugin/bin/anban$EXT" 2>/dev/null || true
  echo "Bundled $plugin/bin/anban$EXT"
done
