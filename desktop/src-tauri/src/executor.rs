use crate::{
    config::AppConfig,
    paths::Resources,
    provision::{self, RuntimeSnapshot},
    sidecar::{self, SidecarEnv},
};
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
/// service.LocalExecutionConfig). The desktop builds the anban run argv
/// from this in sidecar::run_agent.
#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct LocalExecutionConfig {
    pub task_id: String,
    pub task_type: String,
    pub agent_pack_id: String,
    pub agent_pack_version: String,
    pub agent_pack_digest: String,
    pub runtime_adapter: String,
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
    #[serde(default = "default_true")]
    pub article_with_cover: bool,
    #[serde(default = "default_true")]
    pub article_with_content_images: bool,
    /// Returned by the server claim contract; injected into the spawned agent's
    /// env as ANBAN_DEFAULT_PROJECT (mirrors the cloud DockerExecutor) so the
    /// agent's BuildUserPrompt sees the project context (e.g. the topic-pool
    /// anti-double-consume `about:` invariant depends on it).
    #[serde(default)]
    pub project_id: String,
}

fn default_true() -> bool {
    true
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

#[derive(Serialize)]
struct CompleteBody<'a> {
    task_id: &'a str,
    result: FailureResult<'a>,
}

#[derive(Serialize)]
struct FailureResult<'a> {
    success: bool,
    error: &'a str,
    log_text: &'a str,
}

#[derive(Clone, Serialize)]
struct StatusEvent {
    stage: &'static str,
    level: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    task_id: Option<String>,
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
            executor_info: ExecutorInfo {
                hostname: "desktop",
                version: "0.1",
            },
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
    let envelope: Envelope<LocalExecutionConfig> = resp
        .json()
        .await
        .map_err(|e| format!("claim decode failed: {e}"))?;
    Ok(envelope.data)
}

fn failure_complete_payload<'a>(task_id: &'a str, error: &'a str) -> CompleteBody<'a> {
    CompleteBody {
        task_id,
        result: FailureResult {
            success: false,
            error,
            log_text: error,
        },
    }
}

async fn complete_failed_task(
    client: &reqwest::Client,
    api_base: &str,
    api_key: &str,
    task_id: &str,
    error: &str,
) -> Result<(), String> {
    let url = format!("{}/agent/complete", api_base.trim_end_matches('/'));
    let resp = client
        .post(&url)
        .bearer_auth(api_key)
        .json(&failure_complete_payload(task_id, error))
        .send()
        .await
        .map_err(|e| format!("complete request failed: {e}"))?;
    if !resp.status().is_success() {
        return Err(format!("complete returned HTTP {}", resp.status()));
    }
    Ok(())
}

async fn set_runtime(
    runtime: &Arc<RwLock<RuntimeSnapshot>>,
    state: &str,
    current_task_id: Option<String>,
    last_error: Option<String>,
) {
    let mut snapshot = runtime.write().await;
    snapshot.state = Some(state.to_string());
    snapshot.current_task_id = current_task_id;
    snapshot.last_error = last_error;
    snapshot.last_event_at = Some(provision::now_event_at());
}

async fn set_runtime_state_preserving_error(
    runtime: &Arc<RwLock<RuntimeSnapshot>>,
    state: &str,
    current_task_id: Option<String>,
) {
    let mut snapshot = runtime.write().await;
    snapshot.state = Some(state.to_string());
    snapshot.current_task_id = current_task_id;
    snapshot.last_event_at = Some(provision::now_event_at());
}

/// The background claim loop. Runs until `cancel` is cancelled. Designed to be
/// spawned on a tokio task; reads the live config from `cfg` so credential /
/// workspace / server changes take effect without restarting the loop.
pub async fn run_loop(
    app: AppHandle,
    cfg: Arc<RwLock<AppConfig>>,
    running: Arc<AtomicBool>,
    res: Resources,
    runtime: Arc<RwLock<RuntimeSnapshot>>,
    cancel: CancellationToken,
) {
    let client = match reqwest::Client::builder()
        // Bound every request so a hung/slow server (or a transparent proxy
        // that accepts the TCP connection but never responds) can't freeze the
        // claim loop — which would also make the executor un-stoppable, since
        // stop only takes effect on the next loop iteration.
        .timeout(std::time::Duration::from_secs(15))
        .connect_timeout(std::time::Duration::from_secs(8))
        .build()
    {
        Ok(c) => c,
        Err(e) => {
            let _ = app.emit(
                "local-run://event",
                StatusEvent {
                    stage: "executor",
                    level: "error",
                    task_id: None,
                    message: format!("http client init failed: {e}"),
                },
            );
            running.store(false, Ordering::SeqCst);
            set_runtime(
                &runtime,
                "error",
                None,
                Some(format!("http client init failed: {e}")),
            )
            .await;
            return;
        }
    };

    set_runtime(&runtime, "running_idle", None, None).await;
    let _ = app.emit(
        "local-run://event",
        StatusEvent {
            stage: "executor",
            level: "info",
            task_id: None,
            message: "本地执行器已启动".to_string(),
        },
    );

    loop {
        if cancel.is_cancelled() {
            break;
        }

        let snapshot = cfg.read().await.clone();
        // Gate on the bundled agent binary too: without it every claimed task
        // fails to spawn. The loop keeps claiming (there's no spawn-side backoff),
        // so a missing binary would burn through the pending queue — each task
        // lingering ~5 min until the stuck-task reaper force-fails + refunds it.
        // Don't claim work we can't execute.
        let proceed = provision::status(&res, &snapshot, false).available;
        if !proceed {
            // Not fully provisioned — back off and re-check.
            if tokio::time::timeout(ERROR_BACKOFF, cancel.cancelled())
                .await
                .is_ok()
            {
                break;
            }
            continue;
        }

        // Claim is cancelable: a hung request (bounded by the client timeout)
        // or a user-initiated stop can still interrupt this await so the loop
        // can exit promptly instead of blocking on the in-flight claim.
        set_runtime_state_preserving_error(&runtime, "claiming", None).await;
        let claimed = tokio::select! {
            r = claim_once(&client, &snapshot.api_base, &snapshot.api_key) => r,
            _ = cancel.cancelled() => break,
        };
        match claimed {
            Ok(Some(task_cfg)) => {
                set_runtime(
                    &runtime,
                    "running_task",
                    Some(task_cfg.task_id.clone()),
                    None,
                )
                .await;
                let _ = app.emit(
                    "local-run://event",
                    StatusEvent {
                        stage: "executor",
                        level: "info",
                        task_id: Some(task_cfg.task_id.clone()),
                        message: format!(
                            "已认领任务 {}（{}），开始本地执行",
                            task_cfg.task_id, task_cfg.task_type
                        ),
                    },
                );
                let env = SidecarEnv {
                    node_bin: res.node_bin.clone().unwrap_or_default(),
                    plugin_dir: res.plugin_dir.clone().unwrap_or_default(),
                    anthropic_api_key: snapshot.claude_api_key.clone(),
                    system_path: std::env::var("PATH").unwrap_or_default(),
                    cancel: cancel.clone(),
                };
                let server_url = derive_server_url(&snapshot.api_base);
                let workspace = std::path::PathBuf::from(&snapshot.workspace_root);
                let agent_bin = res.agent_bin.clone().unwrap_or_default();
                if let Err(e) = sidecar::run_agent(
                    &app,
                    &agent_bin,
                    &env,
                    &workspace,
                    &server_url,
                    &snapshot.api_key,
                    &task_cfg,
                )
                .await
                {
                    let error = e.to_string();
                    let _ = app.emit(
                        "local-run://event",
                        StatusEvent {
                            stage: "executor",
                            level: "error",
                            task_id: Some(task_cfg.task_id.clone()),
                            message: format!("任务 {} 执行失败: {error}", task_cfg.task_id),
                        },
                    );
                    set_runtime(
                        &runtime,
                        "error",
                        Some(task_cfg.task_id.clone()),
                        Some(error.clone()),
                    )
                    .await;
                    if let Err(complete_err) = complete_failed_task(
                        &client,
                        &snapshot.api_base,
                        &snapshot.api_key,
                        &task_cfg.task_id,
                        &error,
                    )
                    .await
                    {
                        let _ = app.emit(
                            "local-run://event",
                            StatusEvent {
                                stage: "executor",
                                level: "error",
                                task_id: Some(task_cfg.task_id.clone()),
                                message: format!(
                                    "任务 {} 失败终局上报失败: {complete_err}",
                                    task_cfg.task_id
                                ),
                            },
                        );
                    }
                } else {
                    set_runtime_state_preserving_error(&runtime, "running_idle", None).await;
                }
            }
            Ok(None) => {
                set_runtime_state_preserving_error(&runtime, "running_idle", None).await;
                // Nothing claimable — sleep until the next poll.
                if tokio::time::timeout(POLL_INTERVAL, cancel.cancelled())
                    .await
                    .is_ok()
                {
                    break;
                }
            }
            Err(e) => {
                let _ = app.emit(
                    "local-run://event",
                    StatusEvent {
                        stage: "executor",
                        level: "error",
                        task_id: None,
                        message: format!("认领失败: {e}"),
                    },
                );
                set_runtime(&runtime, "error", None, Some(format!("认领失败: {e}"))).await;
                if tokio::time::timeout(ERROR_BACKOFF, cancel.cancelled())
                    .await
                    .is_ok()
                {
                    break;
                }
            }
        }
    }

    running.store(false, Ordering::SeqCst);
    set_runtime_state_preserving_error(&runtime, "ready_stopped", None).await;
    let _ = app.emit(
        "local-run://event",
        StatusEvent {
            stage: "executor",
            level: "info",
            task_id: None,
            message: "本地执行器已停止".to_string(),
        },
    );
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn failure_complete_payload_marks_local_task_failed() {
        let body = failure_complete_payload("task-1", "spawn failed");
        let json = serde_json::to_value(&body).expect("serialize payload");

        assert_eq!(json["task_id"], "task-1");
        assert_eq!(json["result"]["success"], false);
        assert_eq!(json["result"]["error"], "spawn failed");
    }
}
