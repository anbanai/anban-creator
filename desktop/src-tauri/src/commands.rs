use crate::{config::LocalExecutorConfigPatch, provision, state::AppState};
use std::net::IpAddr;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tauri::{AppHandle, Manager, State};
use tauri_plugin_dialog::DialogExt;
use tokio::io::AsyncWriteExt;
use tokio_util::sync::CancellationToken;

// NOTE on the async commands below: Tauri's `#[tauri::command]` macro requires an
// async command's future to be `'static`. A `State<'_, AppState>` *parameter*
// is borrowed from the IPC message and would be captured into the future,
// breaking that bound — so async commands take `app: AppHandle` (an owned arg)
// and reach state via `app.state::<AppState>()` (which borrows `app`, owned by
// the future). State is only ever touched synchronously to clone `Arc`/owned
// handles out before any `.await`, so nothing borrowed crosses an await point.

/// Owned clones of AppState's lock-bearing fields, extracted synchronously so an
/// async command can `.await` on them without borrowing `State` across the await.
struct ClonedHandles {
    config: Arc<tokio::sync::RwLock<crate::config::AppConfig>>,
    config_dir: PathBuf,
    resources: crate::paths::Resources,
    running: Arc<AtomicBool>,
    cancel: Arc<tokio::sync::Mutex<Option<CancellationToken>>>,
    runtime: Arc<tokio::sync::RwLock<provision::RuntimeSnapshot>>,
}

fn clone_handles(app: &AppHandle) -> ClonedHandles {
    let s = app.state::<AppState>();
    ClonedHandles {
        config: s.config.clone(),
        config_dir: s.config_dir.clone(),
        resources: s.resources.clone(),
        running: s.running.clone(),
        cancel: s.cancel.clone(),
        runtime: s.runtime.clone(),
    }
}

async fn set_runtime_state(
    runtime: &Arc<tokio::sync::RwLock<provision::RuntimeSnapshot>>,
    state: &str,
    current_task_id: Option<String>,
    last_error: Option<String>,
) {
    let mut snapshot = runtime.write().await;
    snapshot.state = Some(state.to_string());
    snapshot.current_task_id = current_task_id;
    if state == "starting" {
        snapshot.last_error = None;
    } else if last_error.is_some() {
        snapshot.last_error = last_error;
    }
    snapshot.last_event_at = Some(provision::now_event_at());
}

#[tauri::command]
pub async fn get_api_base(app: AppHandle) -> String {
    let h = clone_handles(&app);
    let base = h.config.read().await.api_base.clone();
    base
}

#[tauri::command]
pub async fn set_api_base(base: String, app: AppHandle) -> bool {
    let h = clone_handles(&app);
    let snapshot = {
        let mut cfg = h.config.write().await;
        cfg.api_base = base.trim_end_matches('/').to_string();
        cfg.clone()
    };
    // Persist outside the lock: synchronous file IO must never hold the RwLock
    // (it would stall the executor loop reading the live config).
    snapshot.save(&h.config_dir);
    // The loop reads the live config, so a base change needs no restart.
    true
}

/// Persist the local-executor credentials + workspace. Called from the first-run
/// wizard / settings. Returns true on success.
#[tauri::command]
pub async fn set_local_executor_config(
    api_key: String,
    workspace: String,
    claude_api_key: String,
    clear_api_key: Option<bool>,
    clear_claude_api_key: Option<bool>,
    app: AppHandle,
) -> bool {
    let h = clone_handles(&app);
    let snapshot = {
        let mut cfg = h.config.write().await;
        cfg.apply_local_executor_patch(LocalExecutorConfigPatch {
            api_key,
            workspace,
            claude_api_key,
            clear_api_key: clear_api_key.unwrap_or(false),
            clear_claude_api_key: clear_claude_api_key.unwrap_or(false),
        });
        cfg.clone()
    };
    snapshot.save(&h.config_dir);
    true
}

#[tauri::command]
pub async fn set_executor_enabled(enabled: bool, app: AppHandle) -> bool {
    let h = clone_handles(&app);
    let snapshot = {
        let mut cfg = h.config.write().await;
        cfg.local_executor_enabled = enabled;
        cfg.clone()
    };
    snapshot.save(&h.config_dir);
    true
}

#[tauri::command]
pub async fn local_executor_status(app: AppHandle) -> provision::ProvisionStatus {
    let h = clone_handles(&app);
    let cfg = h.config.read().await.clone();
    let running = h.running.load(Ordering::SeqCst);
    let runtime = h.runtime.read().await.clone();
    provision::status_with_runtime(&h.resources, &cfg, running, &runtime)
}

#[tauri::command]
pub async fn validate_local_executor_config(
    workspace: String,
    app: AppHandle,
) -> provision::ProvisionStatus {
    let h = clone_handles(&app);
    let mut cfg = h.config.read().await.clone();
    let workspace = workspace.trim();
    if !workspace.is_empty() {
        let _ = std::fs::create_dir_all(workspace);
        cfg.workspace_root = workspace.to_string();
    }
    let running = h.running.load(Ordering::SeqCst);
    let runtime = h.runtime.read().await.clone();
    provision::status_with_runtime(&h.resources, &cfg, running, &runtime)
}

#[tauri::command]
pub async fn copy_local_diagnostics(app: AppHandle) -> String {
    let h = clone_handles(&app);
    let cfg = h.config.read().await.clone();
    let running = h.running.load(Ordering::SeqCst);
    let runtime = h.runtime.read().await.clone();
    let status = provision::status_with_runtime(&h.resources, &cfg, running, &runtime);
    serde_json::to_string_pretty(&serde_json::json!({
        "api_base": cfg.api_base,
        "workspace": cfg.workspace_root,
        "local_executor_enabled": cfg.local_executor_enabled,
        "status": status,
    }))
    .unwrap_or_else(|_| "{}".to_string())
}

#[tauri::command]
pub async fn is_local_executor_available(app: AppHandle) -> bool {
    let h = clone_handles(&app);
    let cfg = h.config.read().await.clone();
    provision::status(&h.resources, &cfg, false).available
}

#[tauri::command]
pub fn is_local_executor_running(state: State<'_, AppState>) -> bool {
    state.running.load(Ordering::SeqCst)
}

/// Start the background claim loop. Refuses if not provisioned. The loop
/// self-stops on cancellation (stop_local_executor) or fatal config gaps.
#[tauri::command]
pub async fn start_local_executor(app: AppHandle) -> bool {
    let h = clone_handles(&app);

    let cfg = h.config.read().await.clone();
    if !provision::status(&h.resources, &cfg, false).available {
        return false;
    }
    if h.running.swap(true, Ordering::SeqCst) {
        return true; // already running
    }
    set_runtime_state(&h.runtime, "starting", None, None).await;

    let cancel = CancellationToken::new();
    {
        let mut slot = h.cancel.lock().await;
        *slot = Some(cancel.clone());
    }

    tauri::async_runtime::spawn(crate::executor::run_loop(
        app,
        h.config,
        h.running,
        h.resources,
        h.runtime,
        cancel,
    ));
    true
}

#[tauri::command]
pub async fn stop_local_executor(app: AppHandle) -> bool {
    let h = clone_handles(&app);
    let token = {
        let mut slot = h.cancel.lock().await;
        slot.take()
    };
    if let Some(t) = token {
        set_runtime_state(&h.runtime, "stopping", None, None).await;
        t.cancel();
    } else {
        set_runtime_state(&h.runtime, "ready_stopped", None, None).await;
    }
    h.running.store(false, Ordering::SeqCst);
    true
}

/// A URL is safe to hand to the OS launcher only if it is an http(s) link with
/// no shell metacharacters. Defense-in-depth: the scheme whitelist blocks
/// `file://`, `smb://`, custom URI schemes and `javascript:`; the metacharacter
/// reject blocks `cmd /C start` injection on Windows (e.g. `https://x&calc`).
/// `url` is attacker-controllable via IPC, so we never trust the SPA to have
/// pre-validated it.
fn is_safe_external_url(url: &str) -> bool {
    let lower = url.trim().to_ascii_lowercase();
    if !(lower.starts_with("http://") || lower.starts_with("https://")) {
        return false;
    }
    !url.chars().any(|c| {
        matches!(
            c,
            '&' | '|' | '>' | '<' | '^' | '(' | ')' | '%' | '"' | ';' | '`' | '\n' | '\r'
        )
    })
}

fn is_safe_download_url(url: &str) -> bool {
    let Ok(parsed) = reqwest::Url::parse(url.trim()) else {
        return false;
    };
    if parsed.scheme() != "http" && parsed.scheme() != "https" {
        return false;
    }
    if !parsed.username().is_empty() || parsed.password().is_some() {
        return false;
    }
    let Some(host) = parsed.host_str() else {
        return false;
    };
    let host = host.trim_end_matches('.').to_ascii_lowercase();
    if matches!(host.as_str(), "localhost" | "0.0.0.0") || host.ends_with(".localhost") {
        return false;
    }
    if let Ok(ip) = host.parse::<IpAddr>() {
        return match ip {
            IpAddr::V4(v4) => {
                !(v4.is_private()
                    || v4.is_loopback()
                    || v4.is_link_local()
                    || v4.is_unspecified()
                    || v4.is_broadcast())
            }
            IpAddr::V6(v6) => {
                !(v6.is_loopback() || v6.is_unspecified() || v6.is_unique_local())
            }
        };
    }
    true
}

/// Open a URL in the user's default browser/app (replaces window.open in the SPA).
/// Uses the platform launcher directly — no plugin dependency (the dialog plugin,
/// used by save_blob, is the only plugin we pull in).
#[tauri::command]
pub fn open_external(_app: AppHandle, url: String) -> Result<(), String> {
    if !is_safe_external_url(&url) {
        return Err(
            "refused to open URL: must be an http(s) link without shell metacharacters".to_string(),
        );
    }
    use std::process::Command;
    let program = if cfg!(target_os = "macos") {
        "open"
    } else if cfg!(target_os = "windows") {
        "cmd"
    } else {
        "xdg-open"
    };
    let mut cmd = Command::new(program);
    if cfg!(target_os = "windows") {
        cmd.args(["/C", "start", "", &url]);
    } else {
        cmd.arg(&url);
    }
    cmd.spawn().map_err(|e| e.to_string())?;
    Ok(())
}

fn open_path(path: &Path) -> Result<(), String> {
    let program = if cfg!(target_os = "macos") {
        "open"
    } else if cfg!(target_os = "windows") {
        "explorer"
    } else {
        "xdg-open"
    };
    std::process::Command::new(program)
        .arg(path)
        .spawn()
        .map_err(|e| e.to_string())?;
    Ok(())
}

#[tauri::command]
pub async fn open_workspace(app: AppHandle) -> Result<bool, String> {
    let h = clone_handles(&app);
    let cfg = h.config.read().await.clone();
    if cfg.workspace_root.trim().is_empty() {
        return Err("workspace is not configured".to_string());
    }
    open_path(Path::new(&cfg.workspace_root))?;
    Ok(true)
}

/// Native directory picker for choosing the local workspace root. Returns the
/// chosen absolute path, or null when cancelled.
///
/// `tauri-plugin-dialog` 2.7 exposes `pick_folder` as a callback (not a future),
/// so we bridge it onto a oneshot channel to keep this command awaitable.
#[tauri::command]
pub async fn pick_directory(app: AppHandle) -> Result<Option<String>, String> {
    let (tx, rx) = tokio::sync::oneshot::channel();
    app.dialog().file().pick_folder(move |path| {
        let _ = tx.send(path);
    });
    let path = rx.await.map_err(|e| e.to_string())?;
    match path {
        Some(p) => p
            .into_path()
            .map(|p| Some(p.to_string_lossy().into_owned()))
            .map_err(|e| e.to_string()),
        None => Ok(None),
    }
}

/// Persist a downloaded file via a native save dialog. `data` arrives as a JSON
/// number array from the frontend (see studio downloadBlob).
#[tauri::command]
pub async fn save_blob(app: AppHandle, filename: String, data: Vec<u8>) -> Result<bool, String> {
    let Some(p) = pick_save_path(&app, &filename).await? else {
        return Ok(false);
    };
    std::fs::write(&p, &data).map_err(|e| e.to_string())?;
    Ok(true)
}

async fn pick_save_path(app: &AppHandle, filename: &str) -> Result<Option<PathBuf>, String> {
    let (tx, rx) = tokio::sync::oneshot::channel();
    app.dialog()
        .file()
        .set_file_name(filename)
        .save_file(move |path| {
            let _ = tx.send(path);
        });
    let path = rx.await.map_err(|e| e.to_string())?;
    match path {
        Some(p) => p.into_path().map(Some).map_err(|e| e.to_string()),
        None => Ok(None),
    }
}

#[tauri::command]
pub async fn save_url_to_file(
    app: AppHandle,
    url: String,
    filename: String,
) -> Result<bool, String> {
    if !is_safe_download_url(&url) {
        return Err("refused to download URL: must be a safe http(s) link".to_string());
    }
    let Some(path) = pick_save_path(&app, &filename).await? else {
        return Ok(false);
    };
    let mut resp = reqwest::get(&url).await.map_err(|e| e.to_string())?;
    if !resp.status().is_success() {
        return Err(format!("download returned HTTP {}", resp.status()));
    }
    let mut file = tokio::fs::File::create(&path)
        .await
        .map_err(|e| e.to_string())?;
    while let Some(chunk) = resp.chunk().await.map_err(|e| e.to_string())? {
        file.write_all(&chunk).await.map_err(|e| e.to_string())?;
    }
    file.flush().await.map_err(|e| e.to_string())?;
    Ok(true)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn download_urls_reject_local_or_credentialed_hosts() {
        assert!(is_safe_download_url(
            "https://cdn.example.com/files/result.zip"
        ));
        assert!(is_safe_download_url(
            "https://cdn.example.com/files/result.zip?X-Amz-Signature=a%2Fb&token=ok"
        ));
        assert!(!is_safe_download_url("http://localhost:8080/admin/export"));
        assert!(!is_safe_download_url("http://127.0.0.1:8080/admin/export"));
        assert!(!is_safe_download_url("http://10.0.0.8/file.zip"));
        assert!(!is_safe_download_url("http://172.16.0.8/file.zip"));
        assert!(!is_safe_download_url("http://192.168.1.8/file.zip"));
        assert!(!is_safe_download_url(
            "https://user:pass@example.com/file.zip"
        ));
        assert!(!is_safe_download_url("file:///tmp/result.zip"));
    }
}
