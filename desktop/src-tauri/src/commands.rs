use crate::{provision, state::AppState};
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tauri::{AppHandle, Manager, State};
use tauri_plugin_dialog::DialogExt;
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
}

fn clone_handles(app: &AppHandle) -> ClonedHandles {
    let s = app.state::<AppState>();
    ClonedHandles {
        config: s.config.clone(),
        config_dir: s.config_dir.clone(),
        resources: s.resources.clone(),
        running: s.running.clone(),
        cancel: s.cancel.clone(),
    }
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
    app: AppHandle,
) -> bool {
    let h = clone_handles(&app);
    let snapshot = {
        let mut cfg = h.config.write().await;
        cfg.api_key = api_key.trim().to_string();
        cfg.workspace_root = workspace.trim().to_string();
        cfg.claude_api_key = claude_api_key.trim().to_string();
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
    provision::status(&h.resources, &cfg, running)
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

    let cancel = CancellationToken::new();
    {
        let mut slot = h.cancel.lock().await;
        *slot = Some(cancel.clone());
    }

    tauri::async_runtime::spawn(crate::executor::run_loop(app, h.config, h.running, h.resources, cancel));
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
        t.cancel();
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
    !url.chars()
        .any(|c| matches!(c, '&' | '|' | '>' | '<' | '^' | '(' | ')' | '%' | '"' | ';' | '`' | '\n' | '\r'))
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
    let (tx, rx) = tokio::sync::oneshot::channel();
    app.dialog()
        .file()
        .set_file_name(&filename)
        .save_file(move |path| {
            let _ = tx.send(path);
        });
    let path = rx.await.map_err(|e| e.to_string())?;
    match path {
        Some(p) => {
            let p = p.into_path().map_err(|e| e.to_string())?;
            std::fs::write(&p, &data).map_err(|e| e.to_string())?;
            Ok(true)
        }
        None => Ok(false),
    }
}
