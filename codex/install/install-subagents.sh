#!/usr/bin/env bash
# install-subagents.sh — Install anbanwriter Codex subagents (idempotent)
#
# What this script does:
#   1. Copies codex/agents/*.toml into ~/.codex/agents/, substituting __PLUGIN_ROOT__
#      with the discovered plugin install path.
#   2. Merges codex/install/agents-registration.toml into ~/.codex/config.toml
#      (idempotent: skips any [agents.*] table that already exists).
#   3. Prompts the user to restart Codex and verify with /agents.
#
# Usage:
#   bash codex/install/install-subagents.sh          # from repo root
#   bash install-subagents.sh                         # from inside codex/install/
#
# Environment overrides:
#   ANBANWRITER_PLUGIN_ROOT  Override plugin install path detection (advanced).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
AGENTS_SRC="$REPO_ROOT/codex/agents"
REGISTRATION_SRC="$REPO_ROOT/codex/install/agents-registration.toml"

CODEX_DIR="${HOME}/.codex"
CODEX_AGENTS_DIR="$CODEX_DIR/agents"
CODEX_CONFIG="$CODEX_DIR/config.toml"

# --- 1. Discover plugin install path -------------------------------------

discover_plugin_root() {
  if [[ -n "${ANBANWRITER_PLUGIN_ROOT:-}" ]]; then
    echo "$ANBANWRITER_PLUGIN_ROOT"
    return
  fi

  # Pattern: ~/.codex/plugins/cache/<marketplace>/anbanwriter/<version>/skills/article/SKILL.md
  local candidate
  candidate="$(find "$CODEX_DIR/plugins/cache" -maxdepth 6 \
    -path "*anbanwriter*/skills/article/SKILL.md" -type f 2>/dev/null \
    | head -n 1 | sed 's|/skills/article/SKILL.md$||')"

  if [[ -n "$candidate" ]]; then
    echo "$candidate"
    return
  fi

  # Fallback: assume repo layout (developer mode). The skills stay where they are.
  echo "$REPO_ROOT/codex"
}

PLUGIN_ROOT="$(discover_plugin_root)"

echo "[install-subagents] Detected plugin root: $PLUGIN_ROOT"
if [[ ! -f "$PLUGIN_ROOT/skills/article/SKILL.md" ]]; then
  echo "[install-subagents] WARNING: $PLUGIN_ROOT/skills/article/SKILL.md not found." >&2
  echo "[install-subagents] Subagent skill loading may fail. Install the plugin first:" >&2
  echo "[install-subagents]   codex plugin marketplace add $REPO_ROOT/codex" >&2
  echo "[install-subagents]   codex plugin install anbanwriter" >&2
  echo "[install-subagents] Or set ANBANWRITER_PLUGIN_ROOT to point at the installed plugin directory." >&2
fi

# --- 2. Copy subagent TOMLs into ~/.codex/agents/ -------------------------

mkdir -p "$CODEX_AGENTS_DIR"

for toml in "$AGENTS_SRC"/*.toml; do
  name="$(basename "$toml")"
  target="$CODEX_AGENTS_DIR/$name"
  # Substitute __PLUGIN_ROOT__ with discovered path on copy
  sed "s|__PLUGIN_ROOT__|$PLUGIN_ROOT|g" "$toml" > "$target"
  echo "[install-subagents] Installed: $target"
done

# --- 3. Merge registration into ~/.codex/config.toml ----------------------

mkdir -p "$CODEX_DIR"

if [[ ! -f "$CODEX_CONFIG" ]]; then
  cp "$REGISTRATION_SRC" "$CODEX_CONFIG"
  echo "[install-subagents] Created $CODEX_CONFIG"
else
  # Idempotent merge: skip [features], [agents], and any [agents.<name>] tables
  # that already exist; append only what is missing.
  tmp="$(mktemp)"
  cp "$CODEX_CONFIG" "$tmp"

  append_if_missing_block() {
    local block_header="$1"   # e.g. "[agents.wechatarticle]"
    if ! grep -qE "^${block_header//\[/\\[}" "$tmp"; then
      printf '\n%s\n' "$2" >> "$tmp"
      echo "[install-subagents] Appended block: $block_header"
    else
      echo "[install-subagents] Already registered: $block_header (skipped)"
    fi
  }

  # Idempotent merge: append top-level keys, append missing tables,
  # AND append missing keys within existing tables (key-level merge for [features], [agents]).
  python3 - "$REGISTRATION_SRC" "$tmp" <<'PY'
import re, sys
reg_path, tmp_path = sys.argv[1], sys.argv[2]
reg_lines = open(reg_path).read().splitlines()
tmp_lines = open(tmp_path).read().splitlines()

HEADER_RE = re.compile(r'^\s*(\[[^\]]+\])\s*$')
KV_RE = re.compile(r'^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=')

# Parse target file into: { header_name: [list of kv keys] }
# Top-level (no header) is stored under "".
target_tables = {"": []}
current = ""
for line in tmp_lines:
    m = HEADER_RE.match(line)
    if m:
        current = m.group(1)
        target_tables.setdefault(current, [])
        continue
    kvm = KV_RE.match(line)
    if kvm:
        target_tables[current].append(kvm.group(1))

# Parse registration file into: [(header, [kv lines]), ...]
# Preamble (top-level before first header) is treated as header="".
reg_blocks = []
current_header = ""
current_kvs = []
for line in reg_lines:
    m = HEADER_RE.match(line)
    if m:
        reg_blocks.append((current_header, current_kvs))
        current_header = m.group(1)
        current_kvs = []
        continue
    # Skip comment-only and blank lines in kv collection (preserve them in original form for append)
    s = line.strip()
    if not s or s.startswith('#'):
        continue
    current_kvs.append(line)
reg_blocks.append((current_header, current_kvs))

# Now merge into target
additions = []  # list of (header_to_insert_under, line_to_append)
for header, kvs in reg_blocks:
    header_exists = header == "" or header in target_tables
    if not header_exists:
        # Append entire table block (header + kvs)
        additions.append((None, header))
        for kv in kvs:
            additions.append((header, kv))
        target_tables[header] = [KV_RE.match(kv).group(1) for kv in kvs if KV_RE.match(kv)]
        print(f'[install-subagents] Appended table: {header}')
    else:
        # Key-level merge within existing table
        existing_keys = set(target_tables.get(header, []))
        for kv in kvs:
            kvm = KV_RE.match(kv)
            if not kvm:
                continue
            key = kvm.group(1)
            if key in existing_keys:
                print(f'[install-subagents] Already present: {header or "(top-level)"}::{key} (skipped)')
                continue
            additions.append((header, kv))
            existing_keys.add(key)
            target_tables.setdefault(header, []).append(key)
            print(f'[install-subagents] Merged key: {header or "(top-level)"}::{key}')

# Apply additions to tmp_lines
# Strategy: for table-keyed additions, insert right after the header line in tmp.
# For table-creating additions (header=None first), append at end.
# Re-process tmp_lines to find header line numbers.
header_line_idx = {}  # header -> first line index
for i, line in enumerate(tmp_lines):
    m = HEADER_RE.match(line)
    if m:
        h = m.group(1)
        if h not in header_line_idx:
            header_line_idx[h] = i

# Build insertion map: line_index -> list of lines to insert AFTER that line
# For top-level (header=""), insert at end (after last line).
# For new table creation, append at end of file.
insertions = {}  # line_idx -> [lines]
append_at_end = []

for header, line in additions:
    if header is None:
        # New table header itself
        append_at_end.append('')  # blank separator
        append_at_end.append(line)
    elif header == "":
        append_at_end.append(line)
    else:
        # Insert after the header line in target
        idx = header_line_idx.get(header)
        if idx is None:
            # Header somehow missing from target — append at end as fallback
            append_at_end.append(line)
        else:
            insertions.setdefault(idx, []).append(line)

# Reconstruct file
out = []
for i, line in enumerate(tmp_lines):
    out.append(line)
    if i in insertions:
        out.extend(insertions[i])
out.extend(append_at_end)

with open(tmp_path, 'w') as f:
    f.write('\n'.join(out) + '\n')
PY

  mv "$tmp" "$CODEX_CONFIG"
  echo "[install-subagents] Merged registration into $CODEX_CONFIG"
fi

# --- 4. Reminders ---------------------------------------------------------

echo
echo "[install-subagents] Done."
echo "[install-subagents] Next steps:"
echo "  1. Set ANBANWRITER_API_KEY in your shell (e.g. ~/.zshrc):"
echo "       export ANBANWRITER_API_KEY=\"<your-api-key>\""
echo "  2. Restart Codex."
echo "  3. Verify with:"
echo "       /agents        # should list 5 anbanwriter subagents"
echo "       /skills        # should list 18 anbanwriter skills"
echo "  4. Trigger explicitly:"
echo "       use the wechatarticle subagent to write an article about X"
