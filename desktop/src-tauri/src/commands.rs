use crate::{provision, state::AppState};
use std::sync::atomic::Ordering;
use tauri::{AppHandle, State};
use tauri_plugin_dialog::DialogExt;
use tokio_util::sync::CancellationToken;

#[tauri::command]
pub fn get_api_base(state: State<'_, AppState>) -> String {
    state.config.blocking_read().api_base.clone()
}

#[tauri::command]
pub fn set_api_base(base: String, state: State<'_, AppState>) -> bool {
    {
        let mut cfg = state.config.blocking_write();
        cfg.api_base = base.trim_end_matches('/').to_string();
        cfg.save(&state.config_dir);
    }
    // The loop reads the live config, so a base change needs no restart.
    true
}

/// Persist the local-executor credentials + workspace. Called from the first-run
/// wizard / settings. Returns true on success.
#[tauri::command]
pub fn set_local_executor_config(
    api_key: String,
    workspace: String,
    claude_api_key: String,
    state: State<'_, AppState>,
) -> bool {
    let mut cfg = state.config.blocking_write();
    cfg.api_key = api_key.trim().to_string();
    cfg.workspace_root = workspace.trim().to_string();
    cfg.claude_api_key = claude_api_key.trim().to_string();
    cfg.save(&state.config_dir);
    true
}

#[tauri::command]
pub fn set_executor_enabled(enabled: bool, state: State<'_, AppState>) -> bool {
    let mut cfg = state.config.blocking_write();
    cfg.local_executor_enabled = enabled;
    cfg.save(&state.config_dir);
    true
}

#[tauri::command]
pub fn local_executor_status(state: State<'_, AppState>) -> provision::ProvisionStatus {
    let cfg = state.config.blocking_read().clone();
    let running = state.running.load(Ordering::SeqCst);
    provision::status(&state.resources, &cfg, running)
}

#[tauri::command]
pub fn is_local_executor_available(state: State<'_, AppState>) -> bool {
    let cfg = state.config.blocking_read().clone();
    provision::status(&state.resources, &cfg, false).available
}

#[tauri::command]
pub fn is_local_executor_running(state: State<'_, AppState>) -> bool {
    state.running.load(Ordering::SeqCst)
}

/// Start the background claim loop. Refuses if not provisioned. The loop
/// self-stops on cancellation (stop_local_executor) or fatal config gaps.
#[tauri::command]
pub fn start_local_executor(app: AppHandle, state: State<'_, AppState>) -> bool {
    let cfg = state.config.blocking_read().clone();
    if !provision::status(&state.resources, &cfg, false).available {
        return false;
    }
    if state.running.swap(true, Ordering::SeqCst) {
        return true; // already running
    }

    let cancel = CancellationToken::new();
    {
        let mut slot = state.cancel.blocking_lock();
        *slot = Some(cancel.clone());
    }

    let config = state.config.clone();
    let running = state.running.clone();
    let res = state.resources.clone();
    let handle = app.clone();
    tauri::async_runtime::spawn(crate::executor::run_loop(handle, config, running, res, cancel));
    true
}

#[tauri::command]
pub fn stop_local_executor(state: State<'_, AppState>) -> bool {
    let token = {
        let mut slot = state.cancel.blocking_lock();
        slot.take()
    };
    if let Some(t) = token {
        t.cancel();
    }
    state.running.store(false, Ordering::SeqCst);
    true
}

/// Open a URL in the user's default browser/app (replaces window.open in the SPA).
/// Uses the platform launcher directly — no plugin dependency (the dialog plugin,
/// used by save_blob, is the only plugin we pull in).
#[tauri::command]
pub fn open_external(_app: AppHandle, url: String) -> Result<(), String> {
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
