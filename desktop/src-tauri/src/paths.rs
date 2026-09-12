use std::path::PathBuf;
use tauri::{path::BaseDirectory, Manager};

/// Resolved paths to the bundled runtime dependencies. `Option` because in
/// `tauri dev` (before resources are populated/bundled) some may be absent;
/// `provision::status` reports which are missing so the first-run wizard can
/// guide the user. See desktop/populate-resources.sh for how these are filled.
#[derive(Clone, Debug, Default)]
pub struct Resources {
    /// Compiled TypeScript Agent entrypoint executed by the bundled Node runtime.
    pub agent_entry: Option<PathBuf>,
    /// Bundled Node runtime executable used by the TypeScript Agent SDK.
    pub node_bin: Option<PathBuf>,
    /// Bundled Claude Agent SDK package (which carries its Claude Code runtime).
    pub claude_sdk: Option<PathBuf>,
    /// Bundled unified Anban plugin root, pointed at via CLAUDE_PLUGIN_ROOT.
    pub plugin_dir: Option<PathBuf>,
    /// Bundled ffmpeg binary (for live-slicer / video work).
    pub ffmpeg: Option<PathBuf>,
}

/// Resolve bundled resource paths. Missing resources resolve to None (logged at
/// debug) — provisioning surfaces a friendly status rather than crashing.
pub fn resolve(app: &tauri::AppHandle) -> Resources {
    Resources {
        agent_entry: resolve_one(app, "resources/agent/dist/main.js"),
        node_bin: resolve_one(app, "resources/bin/node"),
        claude_sdk: resolve_one(
            app,
            "resources/agent/node_modules/@anthropic-ai/claude-agent-sdk",
        ),
        plugin_dir: resolve_one(app, "resources/anban"),
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
