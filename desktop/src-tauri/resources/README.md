Runtime resources are populated locally by `desktop/populate-resources.sh`.

The large runtime files under `bin/`, `agent/`, and `anban/`
are intentionally ignored. This small tracked file keeps Tauri's resource glob
valid in a clean checkout so `cargo check` can run before resources are bundled.
