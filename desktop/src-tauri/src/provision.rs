use crate::{config::AppConfig, paths::Resources};
use serde::Serialize;
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

/// Provisioning status surfaced to the frontend (`local_executor_status` IPC)
/// and the first-run wizard. `available` is the gate: only when true will
/// `is_local_executor_available()` report readiness to the studio create form.
#[derive(Serialize)]
pub struct ProvisionStatus {
    pub state: String,
    pub available: bool,
    pub running: bool,
    pub agent_present: bool,
    pub node_present: bool,
    pub claude_present: bool,
    pub plugin_present: bool,
    pub ffmpeg_present: bool,
    pub claude_authenticated: bool,
    pub workspace_set: bool,
    pub workspace_valid: bool,
    pub workspace_writable: bool,
    pub api_key_set: bool,
    pub auth_mode: String,
    pub checks: Vec<ProvisionCheck>,
    pub current_task_id: Option<String>,
    pub last_error: Option<String>,
    pub last_event_at: Option<String>,
    pub workspace: String,
    /// Human-readable reason when not available (shown in the wizard).
    pub reason: String,
}

#[derive(Serialize)]
pub struct ProvisionCheck {
    pub id: &'static str,
    pub label: &'static str,
    pub ok: bool,
    pub required: bool,
    pub hint: String,
}

#[derive(Clone, Debug, Default)]
pub struct RuntimeSnapshot {
    pub state: Option<String>,
    pub current_task_id: Option<String>,
    pub last_error: Option<String>,
    pub last_event_at: Option<String>,
}

pub fn status(res: &Resources, cfg: &AppConfig, running: bool) -> ProvisionStatus {
    status_with_runtime(res, cfg, running, &RuntimeSnapshot::default())
}

pub fn status_with_runtime(
    res: &Resources,
    cfg: &AppConfig,
    running: bool,
    runtime: &RuntimeSnapshot,
) -> ProvisionStatus {
    let agent_present = res.agent_entry.is_some();
    let node_present = res.node_bin.is_some();
    let claude_present = res.claude_sdk.is_some();
    let plugin_present = res.plugin_dir.is_some();
    let ffmpeg_present = res.ffmpeg.is_some();
    let claude_authenticated = !cfg.claude_api_key.is_empty();
    let workspace_set = !cfg.workspace_root.is_empty();
    let workspace_valid = workspace_set && Path::new(&cfg.workspace_root).is_dir();
    let workspace_writable = workspace_valid && can_write_to_dir(Path::new(&cfg.workspace_root));
    let api_key_set = !cfg.api_key.is_empty();
    let api_base_set = !cfg.api_base.trim().is_empty();

    // Core requirements: sidecar + node + claude + plugin + credentials + workspace.
    let available = agent_present
        && node_present
        && claude_present
        && plugin_present
        && claude_authenticated
        && workspace_valid
        && workspace_writable
        && api_key_set
        && api_base_set;

    let state = if !available {
        "needs_config"
    } else if let Some(state) = runtime_state(runtime, running) {
        state
    } else if running {
        "running_idle"
    } else {
        "ready_stopped"
    };

    let reason = if available {
        String::new()
    } else if !api_base_set {
        "请先配置 Anban Creator 服务地址".to_string()
    } else if !api_key_set {
        "请先配置 Anban Creator API Key（在 Studio「API 密钥」页创建后填入）".to_string()
    } else if !claude_authenticated {
        "请配置 Anthropic API Key（当前版本必填）".to_string()
    } else if !workspace_set {
        "请选择本地工作区根目录".to_string()
    } else if !workspace_valid {
        "请选择有效的本地工作区目录".to_string()
    } else if !workspace_writable {
        "本地工作区不可写，请选择有写入权限的目录".to_string()
    } else if !agent_present {
        "缺少内置 TypeScript Agent（请运行 populate-resources.sh）".to_string()
    } else if !node_present || !claude_present {
        "缺少内置 Node / claude-code 运行时（请运行 populate-resources.sh）".to_string()
    } else if !plugin_present {
        "缺少内置 Anban 插件（请运行 populate-resources.sh）".to_string()
    } else {
        "依赖未就绪".to_string()
    };

    let runtime_ok = agent_present && node_present && claude_present && plugin_present;
    let checks = vec![
        ProvisionCheck {
            id: "identity",
            label: "Anban 身份",
            ok: api_key_set && api_base_set,
            required: true,
            hint: if !api_base_set {
                "请先配置 Anban Creator 服务地址".to_string()
            } else if api_key_set {
                "平台密钥已配置".to_string()
            } else {
                "在设置页创建 Anban Creator API Key 后填入".to_string()
            },
        },
        ProvisionCheck {
            id: "claude",
            label: "Claude 鉴权",
            ok: claude_authenticated,
            required: true,
            hint: if claude_authenticated {
                "ANTHROPIC_API_KEY 已配置".to_string()
            } else {
                "当前版本需要填入 ANTHROPIC_API_KEY".to_string()
            },
        },
        ProvisionCheck {
            id: "workspace",
            label: "本地工作区",
            ok: workspace_valid && workspace_writable,
            required: true,
            hint: if workspace_valid && workspace_writable {
                "工作区可写".to_string()
            } else if workspace_set {
                "目录不存在、不是文件夹或没有写入权限".to_string()
            } else {
                "请选择任务临时文件目录".to_string()
            },
        },
        ProvisionCheck {
            id: "runtime",
            label: "运行时依赖",
            ok: runtime_ok,
            required: true,
            hint: if runtime_ok {
                "anban / Node / claude-code / 插件已就绪".to_string()
            } else {
                "请先填充桌面端运行资源".to_string()
            },
        },
        ProvisionCheck {
            id: "ffmpeg",
            label: "ffmpeg",
            ok: ffmpeg_present,
            required: false,
            hint: if ffmpeg_present {
                "视频与直播切片能力可用".to_string()
            } else {
                "可选；缺失时视频/切片能力不可用".to_string()
            },
        },
    ];

    ProvisionStatus {
        state: state.to_string(),
        available,
        running,
        agent_present,
        node_present,
        claude_present,
        plugin_present,
        ffmpeg_present,
        claude_authenticated,
        workspace_set,
        workspace_valid,
        workspace_writable,
        api_key_set,
        auth_mode: if claude_authenticated {
            "api_key".to_string()
        } else {
            "missing".to_string()
        },
        checks,
        current_task_id: runtime.current_task_id.clone(),
        last_error: runtime.last_error.clone(),
        last_event_at: runtime.last_event_at.clone(),
        workspace: cfg.workspace_root.clone(),
        reason,
    }
}

fn runtime_state<'a>(runtime: &'a RuntimeSnapshot, running: bool) -> Option<&'a str> {
    let state = runtime.state.as_deref()?;
    match state {
        "starting" | "claiming" | "running_idle" | "running_task" if running => Some(state),
        "stopping" | "error" => Some(state),
        _ => None,
    }
}

pub fn now_event_at() -> String {
    let duration = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default();
    let secs = duration.as_secs() as i64;
    let millis = duration.subsec_millis();
    let days = secs.div_euclid(86_400);
    let seconds_of_day = secs.rem_euclid(86_400);
    let (year, month, day) = civil_from_days(days);
    let hour = seconds_of_day / 3_600;
    let minute = (seconds_of_day % 3_600) / 60;
    let second = seconds_of_day % 60;
    format!("{year:04}-{month:02}-{day:02}T{hour:02}:{minute:02}:{second:02}.{millis:03}Z")
}

fn civil_from_days(days_since_unix_epoch: i64) -> (i32, u32, u32) {
    let z = days_since_unix_epoch + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1_460 + doe / 36_524 - doe / 146_096) / 365;
    let mut year = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let day = doy - (153 * mp + 2) / 5 + 1;
    let month = mp + if mp < 10 { 3 } else { -9 };
    if month <= 2 {
        year += 1;
    }
    (year as i32, month as u32, day as u32)
}

fn can_write_to_dir(dir: &Path) -> bool {
    let probe = dir.join(".anban-write-test");
    match std::fs::write(&probe, b"ok") {
        Ok(()) => {
            let _ = std::fs::remove_file(probe);
            true
        }
        Err(_) => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn ready_resources() -> Resources {
        Resources {
            agent_entry: Some(PathBuf::from("/bundle/agent/dist/main.js")),
            node_bin: Some(PathBuf::from("/bundle/node")),
            claude_sdk: Some(PathBuf::from("/bundle/agent/node_modules/@anthropic-ai/claude-agent-sdk")),
            plugin_dir: Some(PathBuf::from("/bundle/anban")),
            ffmpeg: Some(PathBuf::from("/bundle/ffmpeg")),
        }
    }

    fn temp_workspace() -> PathBuf {
        let suffix = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos();
        let dir = std::env::temp_dir().join(format!("anban-desktop-test-{suffix}"));
        std::fs::create_dir_all(&dir).expect("create temp workspace");
        dir
    }

    #[test]
    fn status_reports_ready_stopped_when_configured_but_not_running() {
        let workspace = temp_workspace();
        let cfg = AppConfig {
            api_key: "anban-key".to_string(),
            claude_api_key: "anthropic-key".to_string(),
            workspace_root: workspace.display().to_string(),
            ..AppConfig::default()
        };

        let s = status(&ready_resources(), &cfg, false);

        assert!(s.available);
        assert_eq!(s.state, "ready_stopped");
        assert_eq!(s.auth_mode, "api_key");
        assert!(s.workspace_valid);
        assert!(s.workspace_writable);
        assert!(s.current_task_id.is_none());
        assert!(s.last_error.is_none());
        assert!(s.checks.iter().any(|c| c.id == "identity" && c.ok));
        assert!(s.checks.iter().any(|c| c.id == "workspace" && c.ok));
        assert!(s.checks.iter().any(|c| c.id == "runtime" && c.ok));
    }

    #[test]
    fn status_reports_running_idle_when_executor_is_running_without_task() {
        let workspace = temp_workspace();
        let cfg = AppConfig {
            api_key: "anban-key".to_string(),
            claude_api_key: "anthropic-key".to_string(),
            workspace_root: workspace.display().to_string(),
            ..AppConfig::default()
        };

        let s = status(&ready_resources(), &cfg, true);

        assert_eq!(s.state, "running_idle");
    }

    #[test]
    fn status_reflects_live_runtime_snapshot_when_available() {
        let workspace = temp_workspace();
        let cfg = AppConfig {
            api_key: "anban-key".to_string(),
            claude_api_key: "anthropic-key".to_string(),
            workspace_root: workspace.display().to_string(),
            ..AppConfig::default()
        };
        let runtime = RuntimeSnapshot {
            state: Some("running_task".to_string()),
            current_task_id: Some("task-1".to_string()),
            last_error: Some("recent warning".to_string()),
            last_event_at: Some("2026-07-06T00:00:00Z".to_string()),
        };

        let s = status_with_runtime(&ready_resources(), &cfg, true, &runtime);

        assert_eq!(s.state, "running_task");
        assert_eq!(s.current_task_id.as_deref(), Some("task-1"));
        assert_eq!(s.last_error.as_deref(), Some("recent warning"));
        assert_eq!(s.last_event_at.as_deref(), Some("2026-07-06T00:00:00Z"));
    }

    #[test]
    fn status_requires_writable_workspace() {
        let file_path = std::env::temp_dir().join("anban-desktop-workspace-file");
        std::fs::write(&file_path, b"not a directory").expect("write temp file");
        let cfg = AppConfig {
            api_key: "anban-key".to_string(),
            claude_api_key: "anthropic-key".to_string(),
            workspace_root: file_path.display().to_string(),
            ..AppConfig::default()
        };

        let s = status(&ready_resources(), &cfg, false);

        assert!(!s.available);
        assert_eq!(s.state, "needs_config");
        assert!(!s.workspace_valid);
        assert!(!s.workspace_writable);
        assert!(s.reason.contains("工作区"));
    }

    #[test]
    fn status_requires_api_base_before_reporting_available() {
        let workspace = temp_workspace();
        let cfg = AppConfig {
            api_base: "".to_string(),
            api_key: "anban-key".to_string(),
            claude_api_key: "anthropic-key".to_string(),
            workspace_root: workspace.display().to_string(),
            ..AppConfig::default()
        };

        let s = status(&ready_resources(), &cfg, false);

        assert!(!s.available);
        assert_eq!(s.state, "needs_config");
        assert!(s.reason.contains("服务地址"));
    }
}
