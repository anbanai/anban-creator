use crate::{config::AppConfig, paths::Resources};
use serde::Serialize;

/// Provisioning status surfaced to the frontend (`local_executor_status` IPC)
/// and the first-run wizard. `available` is the gate: only when true will
/// `is_local_executor_available()` report readiness to the studio create form.
#[derive(Serialize)]
pub struct ProvisionStatus {
    pub available: bool,
    pub running: bool,
    pub agent_present: bool,
    pub node_present: bool,
    pub claude_present: bool,
    pub plugin_present: bool,
    pub ffmpeg_present: bool,
    pub claude_authenticated: bool,
    pub workspace_set: bool,
    pub api_key_set: bool,
    pub workspace: String,
    /// Human-readable reason when not available (shown in the wizard).
    pub reason: String,
}

pub fn status(res: &Resources, cfg: &AppConfig, running: bool) -> ProvisionStatus {
    let agent_present = res.agent_bin.is_some();
    let node_present = res.node_bin.is_some();
    let claude_present = res.claude_cli.is_some();
    let plugin_present = res.plugin_dir.is_some();
    let ffmpeg_present = res.ffmpeg.is_some();
    let claude_authenticated = !cfg.claude_api_key.is_empty();
    let workspace_set = !cfg.workspace_root.is_empty();
    let api_key_set = !cfg.api_key.is_empty();

    // Core requirements: sidecar + node + claude + plugin + credentials + workspace.
    let available = agent_present
        && node_present
        && claude_present
        && plugin_present
        && claude_authenticated
        && workspace_set
        && api_key_set;

    let reason = if available {
        String::new()
    } else if !api_key_set {
        "请先配置 AnbanWriter API Key（在 Studio「API 密钥」页创建后填入）".to_string()
    } else if !claude_authenticated {
        "请配置 Claude 鉴权（ANTHROPIC_API_KEY 或完成 Claude OAuth 登录）".to_string()
    } else if !workspace_set {
        "请选择本地工作区根目录".to_string()
    } else if !agent_present {
        "缺少内置 abwriter-agent 二进制（请运行 populate-resources.sh）".to_string()
    } else if !node_present || !claude_present {
        "缺少内置 Node / claude-code 运行时（请运行 populate-resources.sh）".to_string()
    } else if !plugin_present {
        "缺少内置 claudecode 插件（请运行 populate-resources.sh）".to_string()
    } else {
        "依赖未就绪".to_string()
    };

    ProvisionStatus {
        available,
        running,
        agent_present,
        node_present,
        claude_present,
        plugin_present,
        ffmpeg_present,
        claude_authenticated,
        workspace_set,
        api_key_set,
        workspace: cfg.workspace_root.clone(),
        reason,
    }
}
