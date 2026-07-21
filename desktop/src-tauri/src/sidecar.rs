use crate::executor::LocalExecutionConfig;
use serde::Serialize;
use std::io::{Error, ErrorKind};
use std::path::{Path, PathBuf};
use std::process::Stdio;
use tauri::{AppHandle, Emitter};
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::Command;
use tokio::task::JoinHandle;
use tokio_util::sync::CancellationToken;

/// Per-run sidecar configuration: the process environment the spawned agent
/// inherits plus the cancellation token shared with the executor loop.
pub struct SidecarEnv {
    /// Path to the bundled Node executable (its parent dir is prepended to PATH
    /// so claude-agent-sdk-go can spawn the `claude` CLI).
    pub node_bin: PathBuf,
    /// Bundled unified Anban plugin root → CLAUDE_PLUGIN_ROOT.
    pub plugin_dir: PathBuf,
    /// ANTHROPIC_API_KEY forwarded to Claude Code; provisioning requires it.
    pub anthropic_api_key: String,
    /// Original PATH to preserve system tools (ffmpeg may be system-installed).
    pub system_path: String,
    /// Executor-loop cancellation token; fired when the user stops the executor
    /// so run_agent can kill the spawned agent promptly instead of letting it
    /// run (and burn quota) in the background.
    pub cancel: CancellationToken,
}

/// One line of stdout/stderr forwarded to the frontend as a local-run event.
#[derive(Clone, Serialize)]
struct LogLine {
    task_id: String,
    stage: &'static str,
    level: &'static str,
    message: String,
}

/// Emit one local-run log line. Best-effort: a failed emit (e.g. no listener)
/// is silently dropped — the agent owns the authoritative progress stream.
fn emit(app: &AppHandle, task_id: &str, level: &'static str, message: impl Into<String>) {
    let _ = app.emit(
        "local-run://event",
        LogLine {
            task_id: task_id.to_string(),
            stage: "agent",
            level,
            message: message.into(),
        },
    );
}

/// Spawn the bundled `anban` for a claimed task and stream its output
/// to the frontend via `local-run://event` until it exits. The agent reports
/// progress + results back to the cloud itself (using --api-key/--server-url,
/// which the SDK surfaces as ANBAN_API_KEY/ANBAN_API_URL); we only observe.
///
/// If `env.cancel` fires while the agent is running we kill the subprocess so a
/// stopped executor doesn't leave Claude Code running (and burning quota) in the
/// background. Without this, the loop would block on `child.wait()` until the
/// task finished on its own.
///
/// Argv + env mirror `server/agent/docker_executor.go::buildAgentCommand`.
pub async fn run_agent(
    app: &AppHandle,
    agent_bin: &Path,
    env: &SidecarEnv,
    workspace_root: &Path,
    server_url: &str,
    api_key: &str,
    cfg: &LocalExecutionConfig,
) -> std::io::Result<()> {
    let task_workspace = workspace_root.join(&cfg.task_id);
    std::fs::create_dir_all(&task_workspace)?;

    let mut cmd = Command::new(agent_bin);
    for arg in agent_args(server_url, api_key, &task_workspace, cfg) {
        cmd.arg(arg);
    }

    // Environment: extend PATH with the bundled Node dir, point Claude Code at
    // the bundled plugin, forward the Anthropic key when configured.
    let extended_path = match env.node_bin.parent() {
        Some(node_dir) => format!("{}:{}", node_dir.display(), env.system_path),
        None => env.system_path.clone(),
    };
    cmd.env("PATH", &extended_path);
    cmd.env("CLAUDE_PLUGIN_ROOT", &env.plugin_dir);
    if !env.anthropic_api_key.is_empty() {
        cmd.env("ANTHROPIC_API_KEY", &env.anthropic_api_key);
    }
    // Project context: the agent reads ANBAN_DEFAULT_PROJECT (not a flag) and
    // forwards it to BuildUserPrompt. Mirror the cloud DockerExecutor
    // (docker_executor.go sets ANBAN_DEFAULT_PROJECT=%s) so local tasks get the
    // same project scoping — e.g. the topic-pool anti-double-consume `about:`
    // invariant depends on BuildUserPrompt seeing the right project.
    if !cfg.project_id.is_empty() {
        cmd.env("ANBAN_DEFAULT_PROJECT", &cfg.project_id);
    }

    cmd.stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true);

    let mut child = cmd.spawn()?;
    let stdout = child.stdout.take().expect("piped stdout");
    let stderr = child.stderr.take().expect("piped stderr");

    let app_out = app.clone();
    let task_id_out = cfg.task_id.clone();
    let out_handle: JoinHandle<()> = tokio::spawn(async move {
        let mut lines = BufReader::new(stdout).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            emit(&app_out, &task_id_out, "info", line);
        }
    });

    let app_err = app.clone();
    let task_id_err = cfg.task_id.clone();
    let err_handle: JoinHandle<()> = tokio::spawn(async move {
        let mut lines = BufReader::new(stderr).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            emit(&app_err, &task_id_err, "warn", line);
        }
    });

    // Wait for exit OR cancellation. On cancel we kill + reap the child so a
    // stopped executor halts the running task promptly (the loop can't reach
    // its next cancel-check while it's blocked in this await).
    let mut run_error: Option<String> = None;
    tokio::select! {
        status = child.wait() => {
            let status = status?;
            let level: &'static str = if status.success() { "info" } else { "error" };
            emit(app, &cfg.task_id, level, format!("agent exited ({})", status));
            if !status.success() {
                run_error = Some(format!("agent exited ({})", status));
            }
        }
        _ = env.cancel.cancelled() => {
            emit(app, &cfg.task_id, "warn", "本地执行器已停止，终止当前 agent".to_string());
            // Best-effort kill + reap so we don't orphan the process tree.
            let _ = child.kill().await;
            let _ = child.wait().await;
            run_error = Some("用户停止了本地执行器，当前 agent 已终止".to_string());
        }
    }

    // The forwarders exit on their own once the child's pipes close (they do on
    // both normal exit and kill); abort anyway so cleanup is deterministic.
    out_handle.abort();
    err_handle.abort();
    if let Some(message) = run_error {
        return Err(Error::new(ErrorKind::Other, message));
    }
    Ok(())
}

fn agent_args(
    server_url: &str,
    api_key: &str,
    task_workspace: &Path,
    cfg: &LocalExecutionConfig,
) -> Vec<String> {
    let mut args = vec![
        "run".to_string(),
        "--server-url".to_string(),
        server_url.to_string(),
        "--api-key".to_string(),
        api_key.to_string(),
        "--task-id".to_string(),
        cfg.task_id.clone(),
        "--task-type".to_string(),
        cfg.task_type.clone(),
        "--topic".to_string(),
        cfg.topic.clone(),
        "--max-turns".to_string(),
        cfg.max_turns.to_string(),
        "--workspace".to_string(),
        task_workspace.display().to_string(),
        "--agent-flag".to_string(),
        cfg.agent_flag.clone(),
    ];
    if let Some(model) = &cfg.model {
        if !model.is_empty() {
            args.push("--model".to_string());
            args.push(model.clone());
        }
    }
    if let Some(goal) = &cfg.goal {
        if !goal.is_empty() {
            args.push("--goal".to_string());
            args.push(goal.clone());
        }
    }
    // Bool flags mirror the Go flag defaults and server/agent/docker_executor.go.
    args.extend([
        format!("--has-content-image={}", cfg.has_content_image),
        format!("--has-tail-image={}", cfg.has_tail_image),
        format!("--article-with-cover={}", cfg.article_with_cover),
        format!(
            "--article-with-content-images={}",
            cfg.article_with_content_images
        ),
    ]);
    args
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn agent_args_include_article_image_switches() {
        let cfg = LocalExecutionConfig {
            task_id: "task-1".to_string(),
            task_type: "article".to_string(),
            topic: "topic".to_string(),
            agent_flag: "anban:wechatarticle".to_string(),
            max_turns: 12,
            model: None,
            goal: None,
            has_content_image: true,
            has_tail_image: false,
            article_with_cover: false,
            article_with_content_images: true,
            project_id: "project-1".to_string(),
        };

        let args = agent_args(
            "https://api.example.com",
            "key",
            Path::new("/tmp/task-1"),
            &cfg,
        );

        assert_eq!(args.first().map(String::as_str), Some("run"));
        assert!(args.iter().any(|arg| arg == "--article-with-cover=false"));
        assert!(args
            .iter()
            .any(|arg| arg == "--article-with-content-images=true"));
    }
}
