use std::path::PathBuf;
use tauri::{path::BaseDirectory, Manager};

/// Resolved paths to the bundled runtime dependencies. `Option` because in
/// `tauri dev` (before resources are populated/bundled) some may be absent;
/// `provision::status` reports which are missing so the first-run wizard can
/// guide the user. See desktop/populate-resources.sh for how these are filled.
#[derive(Clone, Debug, Default)]
pub struct Resources {
    /// `anban` sidecar binary (built via `make agent-build-native`).
    pub agent_bin: Option<PathBuf>,
    /// Bundled Node runtime executable (used by claude-agent-sdk-go to spawn
    /// the `claude` CLI).
    pub node_bin: Option<PathBuf>,
    /// Bundled `@anthropic-ai/claude-code` package root / CLI entry.
    pub claude_cli: Option<PathBuf>,
    /// Bundled `claudecode/` plugin root, pointed at via CLAUDE_PLUGIN_ROOT.
    pub plugin_dir: Option<PathBuf>,
    /// Bundled ffmpeg binary (for live-slicer / video work).
    pub ffmpeg: Option<PathBuf>,
}

/// Resolve bundled resource paths. Missing resources resolve to None (logged at
/// debug) — provisioning surfaces a friendly status rather than crashing.
pub fn resolve(app: &tauri::AppHandle) -> Resources {
    Resources {
        agent_bin: resolve_one(app, "resources/bin/anban"),
        node_bin: resolve_one(app, "resources/bin/node"),
        claude_cli: resolve_one(app, "resources/claude"),
        plugin_dir: resolve_one(app, "resources/claudecode"),
        ffmpeg: resolve_one(app, "resources/bin/ffmpeg"),
    }
}

fn resolve_one(app: &tauri::AppHandle, rel: &str) -> Option<PathBuf> {
    match app.path().resolve(rel, BaseDirectory::Resource) {
        Ok(p) if p.exists() => Some(p),
        Ok(p) => {
            log::debug!("resource not yet present: {rel} -> {:?}", p);
            None
        }
        Err(e) => {
            log::debug!("failed to resolve resource {rel}: {e}");
            None
        }
    }
}
