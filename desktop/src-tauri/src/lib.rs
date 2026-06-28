mod commands;
mod config;
mod executor;
mod paths;
mod provision;
mod sidecar;
mod state;

use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

/// Build the webview initialization script that seeds localStorage with the
/// configured cloud API base BEFORE the SPA boots, so axios resolves it
/// synchronously on the very first request (login). Runs on every navigation.
fn init_script(api_base: &str) -> String {
    // Serialize as a JSON string literal (handles quotes/escapes safely).
    let base_json = serde_json::to_string(api_base).unwrap_or_else(|_| "\"\"".to_string());
    format!(
        "(function(){{try{{window.localStorage.setItem('anbanwriter_api_base',{base});}}catch(e){{}}}})();",
        base = base_json
    )
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .setup(|app| {
            // Resolve platform config dir + load persisted config.
            let config_dir = app
                .path()
                .app_config_dir()
                .unwrap_or_else(|_| std::path::PathBuf::from("."));
            let config = config::AppConfig::load(&config_dir);

            let resources = paths::resolve(app.handle());
            let should_run = config.local_executor_enabled;
            let api_base_for_script = config.api_base.clone();

            app.manage(state::AppState::new(config, config_dir, resources));

            // Create the main window in code so we can attach an init script
            // (config-defined windows can't take one). Label "main" matches the
            // capability in capabilities/default.json.
            WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
                .title("AnbanWriter")
                .inner_size(1280.0, 820.0)
                .min_inner_size(960.0, 640.0)
                .initialization_script(init_script(&api_base_for_script))
                .build()?;

            // Optionally auto-start the executor on launch.
            if should_run {
                let handle = app.handle().clone();
                tauri::async_runtime::spawn(async move {
                    // Give the window a tick to come up before the first claim.
                    tokio::time::sleep(std::time::Duration::from_secs(1)).await;
                    let _ = commands_start(&handle).await;
                });
            }

            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::get_api_base,
            commands::set_api_base,
            commands::set_local_executor_config,
            commands::set_executor_enabled,
            commands::local_executor_status,
            commands::is_local_executor_available,
            commands::is_local_executor_running,
            commands::start_local_executor,
            commands::stop_local_executor,
            commands::open_external,
            commands::save_blob,
            commands::pick_directory,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

/// Spawn the local executor if provisioned. Mirrors the start_local_executor
/// command but callable from setup without a State extractor. Async because it's
/// invoked from inside `tauri::async_runtime::spawn` (an async context) — using
/// the blocking lock variants there would panic/deadlock the runtime.
async fn commands_start(app: &tauri::AppHandle) -> Result<bool, String> {
    let state = app.state::<state::AppState>();
    let cfg = state.config.read().await.clone();
    if !provision::status(&state.resources, &cfg, false).available {
        return Ok(false);
    }
    if state.running.swap(true, std::sync::atomic::Ordering::SeqCst) {
        return Ok(true);
    }
    let cancel = tokio_util::sync::CancellationToken::new();
    {
        let mut slot = state.cancel.lock().await;
        *slot = Some(cancel.clone());
    }
    let config = state.config.clone();
    let running = state.running.clone();
    let res = state.resources.clone();
    let handle = app.clone();
    tauri::async_runtime::spawn(executor::run_loop(handle, config, running, res, cancel));
    Ok(true)
}
