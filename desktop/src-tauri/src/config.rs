use serde::{Deserialize, Serialize};
use std::fs;
use std::path::{Path, PathBuf};

/// Default cloud server API base used when the user hasn't configured one.
/// Mirrors `DEFAULT_CLOUD_API_BASE` in studio/src/lib/http-client.ts.
pub const DEFAULT_API_BASE: &str = "https://api.anbanai.com/api/v1";

fn default_api_base() -> String {
    DEFAULT_API_BASE.to_string()
}

/// Persisted desktop configuration. Stored as JSON in the platform config dir
/// (`anban-creator-desktop.json`). Loaded once at startup; mutations go through
/// the IPC commands which re-save the file.
#[derive(Clone, Serialize, Deserialize)]
pub struct AppConfig {
    /// Cloud API base the studio webview targets (e.g.
    /// `https://api.anbanai.com/api/v1`). Seeded into the webview's
    /// localStorage before the SPA boots so axios resolves it synchronously.
    #[serde(default = "default_api_base")]
    pub api_base: String,

    /// User API key used to authenticate the local-executor claim loop against
    /// POST /api/v1/agent/claim (and the spawned agent's /progress + /upload).
    /// Created by the user in the Studio "API Keys" page and pasted/configured
    /// in the desktop first-run wizard.
    #[serde(default)]
    pub api_key: String,

    /// Local filesystem root under which per-task workspaces are created
    /// (`{workspace_root}/{task_id}`). Empty until the user picks one.
    #[serde(default)]
    pub workspace_root: String,

    /// Anthropic API key (ANTHROPIC_API_KEY) for Claude Code. Current desktop
    /// UX requires this explicitly; OAuth detection can be added later as a
    /// separate authenticated state.
    #[serde(default)]
    pub claude_api_key: String,

    /// Whether the background claim loop should run at startup.
    #[serde(default)]
    pub local_executor_enabled: bool,
}

pub struct LocalExecutorConfigPatch {
    pub api_key: String,
    pub workspace: String,
    pub claude_api_key: String,
    pub clear_api_key: bool,
    pub clear_claude_api_key: bool,
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            api_base: default_api_base(),
            api_key: String::new(),
            workspace_root: String::new(),
            claude_api_key: String::new(),
            local_executor_enabled: false,
        }
    }
}

impl AppConfig {
    /// Load config from `dir/anban-creator-desktop.json`, falling back to defaults
    /// (and persisting them) when missing or unreadable.
    pub fn load(dir: &Path) -> Self {
        let path = config_path(dir);
        match fs::read_to_string(&path) {
            Ok(text) => serde_json::from_str(&text).unwrap_or_default(),
            Err(_) => AppConfig::default(),
        }
    }

    /// Serialize to the config dir. Best-effort: logs on failure rather than
    /// propagating, since config persistence is non-fatal.
    pub fn save(&self, dir: &Path) {
        let path = config_path(dir);
        if let Err(e) = self.save_inner(&path) {
            log::error!("failed to persist desktop config to {:?}: {e}", path);
        }
    }

    pub fn apply_local_executor_patch(&mut self, patch: LocalExecutorConfigPatch) {
        if patch.clear_api_key {
            self.api_key.clear();
        } else {
            let api_key = patch.api_key.trim();
            if !api_key.is_empty() {
                self.api_key = api_key.to_string();
            }
        }

        if patch.clear_claude_api_key {
            self.claude_api_key.clear();
        } else {
            let claude_api_key = patch.claude_api_key.trim();
            if !claude_api_key.is_empty() {
                self.claude_api_key = claude_api_key.to_string();
            }
        }

        let workspace = patch.workspace.trim();
        if !workspace.is_empty() {
            self.workspace_root = workspace.to_string();
        }
    }

    fn save_inner(&self, path: &PathBuf) -> Result<(), Box<dyn std::error::Error>> {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)?;
        }
        let text = serde_json::to_string_pretty(self)?;
        fs::write(path, text)?;
        // Tighten permissions: this file holds the user's Anban Creator API key
        // and ANTHROPIC_API_KEY in plaintext. The default umask is typically
        // 0o644 (world-readable); restrict to owner-only on unix. (OS
        // keychain-backed storage for the secrets is a follow-up.)
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(path, fs::Permissions::from_mode(0o600))?;
        }
        Ok(())
    }
}

pub fn config_path(dir: &Path) -> PathBuf {
    dir.join("anban-creator-desktop.json")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn local_executor_patch_preserves_existing_secrets_when_inputs_are_blank() {
        let mut cfg = AppConfig {
            api_key: "anban-old".to_string(),
            claude_api_key: "anthropic-old".to_string(),
            workspace_root: "/old/workspace".to_string(),
            ..AppConfig::default()
        };

        cfg.apply_local_executor_patch(LocalExecutorConfigPatch {
            api_key: "".to_string(),
            workspace: "/new/workspace".to_string(),
            claude_api_key: "   ".to_string(),
            clear_api_key: false,
            clear_claude_api_key: false,
        });

        assert_eq!(cfg.api_key, "anban-old");
        assert_eq!(cfg.claude_api_key, "anthropic-old");
        assert_eq!(cfg.workspace_root, "/new/workspace");
    }

    #[test]
    fn local_executor_patch_clears_secrets_only_when_explicitly_requested() {
        let mut cfg = AppConfig {
            api_key: "anban-old".to_string(),
            claude_api_key: "anthropic-old".to_string(),
            workspace_root: "/old/workspace".to_string(),
            ..AppConfig::default()
        };

        cfg.apply_local_executor_patch(LocalExecutorConfigPatch {
            api_key: "".to_string(),
            workspace: "".to_string(),
            claude_api_key: "".to_string(),
            clear_api_key: true,
            clear_claude_api_key: true,
        });

        assert!(cfg.api_key.is_empty());
        assert!(cfg.claude_api_key.is_empty());
        assert_eq!(cfg.workspace_root, "/old/workspace");
    }
}
