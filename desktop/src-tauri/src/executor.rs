use crate::{config::AppConfig, paths::Resources, sidecar::{self, SidecarEnv}};
use serde::{Deserialize, Serialize};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tauri::{AppHandle, Emitter};
use tokio::sync::RwLock;
use tokio_util::sync::CancellationToken;

/// Claim interval when nothing is claimable (the server-side LocalClaimWindow
/// is 30s, so polling well within that keeps latency low).
const POLL_INTERVAL: std::time::Duration = std::time::Duration::from_secs(2);
const ERROR_BACKOFF: std::time::Duration = std::time::Duration::from_secs(5);

/// Full task config returned by POST /api/v1/agent/claim (mirrors server
/// service.LocalExecutionConfig). The desktop builds the abwriter-agent argv
/// from this in sidecar::run_agent.
#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct LocalExecutionConfig {
    pub task_id: String,
    pub task_type: String,
    pub topic: String,
    pub agent_flag: String,
    pub max_turns: i64,
    #[serde(default)]
    pub model: Option<String>,
    #[serde(default)]
    pub goal: Option<String>,
    #[serde(default)]
    pub has_content_image: bool,
    #[serde(default)]
    pub has_tail_image: bool,
    /// Returned by the server claim contract; reserved for project-scoped
    /// workspace namespacing. Not yet consumed by the agent argv builder.
    #[serde(default)]
    #[allow(dead_code)]
    pub project_id: String,
}

/// Standard server response envelope (`{code,msg,data}`).
#[derive(Deserialize)]
struct Envelope<T> {
    data: Option<T>,
}

#[derive(Serialize)]
struct ExecutorInfo<'a> {
    hostname: &'a str,
    version: &'a str,
}

#[derive(Serialize)]
struct ClaimBody<'a> {
    executor_info: ExecutorInfo<'a>,
}

#[derive(Clone, Serialize)]
struct StatusEvent {
    stage: &'static str,
    level: &'static str,
    message: String,
}

/// Strip a trailing `/api/v1` (or `/api/v1/`) from the API base to get the host
/// base the agent expects via `--server-url` (the agent appends `/api/v1/...`).
pub fn derive_server_url(api_base: &str) -> String {
    let trimmed = api_base.trim_end_matches('/');
    for suffix in ["/api/v1", "/api"] {
        if let Some(stripped) = trimmed.strip_suffix(suffix) {
            return stripped.trim_end_matches('/').to_string();
        }
    }
    trimmed.to_string()
}

/// Atomically claim the oldest pending local task for this user. Returns
/// `Ok(Some(cfg))` on a successful claim, `Ok(None)` on 204 (nothing to run).
async fn claim_once(
    client: &reqwest::Client,
    api_base: &str,
    api_key: &str,
) -> Result<Option<LocalExecutionConfig>, String> {
    let url = format!("{}/agent/claim", api_base.trim_end_matches('/'));
    let resp = client
        .post(&url)
        .bearer_auth(api_key)
        .json(&ClaimBody {
            executor_info: ExecutorInfo { hostname: "desktop", version: "0.1" },
        })
        .send()
        .await
        .map_err(|e| format!("claim request failed: {e}"))?;

    if resp.status() == reqwest::StatusCode::NO_CONTENT {
        return Ok(None);
    }
    if !resp.status().is_success() {
        return Err(format!("claim returned HTTP {}", resp.status()));
    }
    let envelope: Envelope<LocalExecutionConfig> =
        resp.json().await.map_err(|e| format!("claim decode failed: {e}"))?;
    Ok(envelope.data)
}

/// The background claim loop. Runs until `cancel` is cancelled. Designed to be
/// spawned on a tokio task; reads the live config from `cfg` so credential /
/// workspace / server changes take effect without restarting the loop.
pub async fn run_loop(
    app: AppHandle,
    cfg: Arc<RwLock<AppConfig>>,
    running: Arc<AtomicBool>,
    res: Resources,
    cancel: CancellationToken,
) {
    let client = match reqwest::Client::builder().build() {
        Ok(c) => c,
        Err(e) => {
            let _ = app.emit(
                "local-run://event",
                StatusEvent { stage: "executor", level: "error", message: format!("http client init failed: {e}") },
            );
            return;
        }
    };

    let _ = app.emit(
        "local-run://event",
        StatusEvent { stage: "executor", level: "info", message: "本地执行器已启动".to_string() },
    );

    loop {
        if cancel.is_cancelled() {
            break;
        }

        let snapshot = cfg.read().await.clone();
        let proceed = snapshot.agent_ready() && !snapshot.api_key.is_empty() && !snapshot.workspace_root.is_empty();
        if !proceed {
            // Not fully provisioned — back off and re-check.
            if tokio::time::timeout(ERROR_BACKOFF, cancel.cancelled()).await.is_ok() {
                break;
            }
            continue;
        }

        match claim_once(&client, &snapshot.api_base, &snapshot.api_key).await {
            Ok(Some(task_cfg)) => {
                let _ = app.emit(
                    "local-run://event",
                    StatusEvent {
                        stage: "executor",
                        level: "info",
                        message: format!("已认领任务 {}（{}），开始本地执行", task_cfg.task_id, task_cfg.task_type),
                    },
                );
                let env = SidecarEnv {
                    node_bin: res.node_bin.clone().unwrap_or_default(),
                    plugin_dir: res.plugin_dir.clone().unwrap_or_default(),
                    anthropic_api_key: snapshot.claude_api_key.clone(),
                    system_path: std::env::var("PATH").unwrap_or_default(),
                };
                let server_url = derive_server_url(&snapshot.api_base);
                let workspace = std::path::PathBuf::from(&snapshot.workspace_root);
                let agent_bin = res.agent_bin.clone().unwrap_or_default();
                if let Err(e) = sidecar::run_agent(&app, &agent_bin, &env, &workspace, &server_url, &snapshot.api_key, &task_cfg).await {
                    let _ = app.emit(
                        "local-run://event",
                        StatusEvent { stage: "executor", level: "error", message: format!("任务 {} 执行失败: {e}", task_cfg.task_id) },
                    );
                }
            }
            Ok(None) => {
                // Nothing claimable — sleep until the next poll.
                if tokio::time::timeout(POLL_INTERVAL, cancel.cancelled()).await.is_ok() {
                    break;
                }
            }
            Err(e) => {
                let _ = app.emit(
                    "local-run://event",
                    StatusEvent { stage: "executor", level: "error", message: format!("认领失败: {e}") },
                );
                if tokio::time::timeout(ERROR_BACKOFF, cancel.cancelled()).await.is_ok() {
                    break;
                }
            }
        }
    }

    running.store(false, Ordering::SeqCst);
    let _ = app.emit(
        "local-run://event",
        StatusEvent { stage: "executor", level: "info", message: "本地执行器已停止".to_string() },
    );
}

/// Helper on AppConfig: whether all bundled deps are present for local runs.
impl AppConfig {
    pub fn agent_ready(&self) -> bool {
        // Resource presence is checked elsewhere (provision::status). From the
        // config side we only need credentials + workspace + api base set.
        !self.api_key.is_empty() && !self.workspace_root.is_empty() && !self.api_base.is_empty()
    }
}
