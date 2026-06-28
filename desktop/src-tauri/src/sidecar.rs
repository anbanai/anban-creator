use crate::executor::LocalExecutionConfig;
use serde::Serialize;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use tauri::{AppHandle, Emitter};
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::Command;

/// Environment the spawned agent subprocess inherits beyond its flags.
pub struct SidecarEnv {
    /// Path to the bundled Node executable (its parent dir is prepended to PATH
    /// so claude-agent-sdk-go can spawn the `claude` CLI).
    pub node_bin: PathBuf,
    /// Bundled claudecode plugin root → CLAUDE_PLUGIN_ROOT.
    pub plugin_dir: PathBuf,
    /// Optional ANTHROPIC_API_KEY (empty → Claude uses its OAuth login).
    pub anthropic_api_key: String,
    /// Original PATH to preserve system tools (ffmpeg may be system-installed).
    pub system_path: String,
}

/// One line of stdout/stderr forwarded to the frontend as a local-run event.
#[derive(Clone, Serialize)]
struct LogLine {
    task_id: String,
    stage: &'static str,
    level: &'static str,
    message: String,
}

/// Spawn the bundled `abwriter-agent` for a claimed task and stream its output
/// to the frontend via `local-run://event` until it exits. The agent reports
/// progress + results back to the cloud itself (using --api-key/--server-url,
/// which the SDK surfaces as ANBAN_API_KEY/ANBAN_API_URL); we only observe.
///
/// Argv mirrors `server/agent/docker_executor.go::buildAgentCommand`.
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
    cmd.arg("--server-url").arg(server_url)
        .arg("--api-key").arg(api_key)
        .arg("--task-id").arg(&cfg.task_id)
        .arg("--task-type").arg(&cfg.task_type)
        .arg("--topic").arg(&cfg.topic)
        .arg("--max-turns").arg(cfg.max_turns.to_string())
        .arg("--workspace").arg(&task_workspace)
        .arg("--agent-flag").arg(&cfg.agent_flag);
    if let Some(model) = &cfg.model {
        if !model.is_empty() {
            cmd.arg("--model").arg(model);
        }
    }
    if let Some(goal) = &cfg.goal {
        if !goal.is_empty() {
            cmd.arg("--goal").arg(goal);
        }
    }
    // Bool flags mirror the Go flag defaults (has-content-image defaults true).
    cmd.arg("--has-content-image").arg(cfg.has_content_image.to_string());
    cmd.arg("--has-tail-image").arg(cfg.has_tail_image.to_string());

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

    cmd.stdout(Stdio::piped()).stderr(Stdio::piped()).kill_on_drop(true);

    let log_stage = "agent";
    let mut child = cmd.spawn()?;
    let stdout = child.stdout.take().expect("piped stdout");
    let stderr = child.stderr.take().expect("piped stderr");

    let app_out = app.clone();
    let task_id_out = cfg.task_id.clone();
    tokio::spawn(async move {
        let mut lines = BufReader::new(stdout).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            let _ = app_out.emit(
                "local-run://event",
                LogLine { task_id: task_id_out.clone(), stage: log_stage, level: "info", message: line },
            );
        }
    });

    let app_err = app.clone();
    let task_id_err = cfg.task_id.clone();
    tokio::spawn(async move {
        let mut lines = BufReader::new(stderr).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            let _ = app_err.emit(
                "local-run://event",
                LogLine { task_id: task_id_err.clone(), stage: log_stage, level: "warn", message: line },
            );
        }
    });

    let status = child.wait().await?;
    let level = if status.success() { "info" } else { "error" };
    let _ = app.emit(
        "local-run://event",
        LogLine {
            task_id: cfg.task_id.clone(),
            stage: log_stage,
            level,
            message: format!("agent exited ({})", status),
        },
    );
    Ok(())
}
