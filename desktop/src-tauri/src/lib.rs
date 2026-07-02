mod commands;
mod config;
mod executor;
mod paths;
mod provision;
mod sidecar;
mod state;
mod window_state;

use std::sync::Arc;
use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};

/// Build the webview initialization script that seeds localStorage with the
/// configured cloud API base BEFORE the SPA boots, so axios resolves it
/// synchronously on the very first request (login). Runs on every navigation.
fn init_script(api_base: &str) -> String {
    // Serialize as a JSON string literal (handles quotes/escapes safely).
    let base_json = serde_json::to_string(api_base).unwrap_or_else(|_| "\"\"".to_string());
    format!(
        "(function(){{try{{window.localStorage.setItem('anban_creator_api_base',{base});}}catch(e){{}}}})();",
        base = base_json
    )
}

/// Native app menu (About / Services / Hide / Quit, Edit, Window). All items are
/// OS-predefined so the standard shortcuts (⌘Q, ⌘C, ⌘V, ⌘A, ⌘Z, ⌘M …) route
/// correctly — without this the macOS menu bar shows an empty/generic app menu.
/// No custom items → no accelerator parsing → no runtime-panic risk.
fn build_menu(app: &tauri::AppHandle) -> tauri::Result<tauri::menu::Menu<tauri::Wry>> {
    use tauri::menu::{Menu, SubmenuBuilder};
    let app_menu = SubmenuBuilder::new(app, "Anban Creator")
        .about(None)
        .separator()
        .services()
        .hide()
        .hide_others()
        .separator()
        .quit()
        .build()?;
    let edit_menu = SubmenuBuilder::new(app, "编辑")
        .undo()
        .redo()
        .separator()
        .cut()
        .copy()
        .paste()
        .select_all()
        .build()?;
    let window_menu = SubmenuBuilder::new(app, "窗口")
        .minimize()
        .maximize()
        .build()?;
    Menu::with_items(app, &[&app_menu, &edit_menu, &window_menu])
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        // Single-instance guard: a second launch focuses the existing window
        // instead of starting a second executor (which would compete for claims).
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            if let Some(w) = app.get_webview_window("main") {
                let _ = w.unminimize();
                let _ = w.show();
                let _ = w.set_focus();
            }
        }))
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

            app.manage(state::AppState::new(config, config_dir.clone(), resources));

            // Native menu (standard items + working copy/paste/quit shortcuts).
            let menu_handle = app.handle().clone();
            menu_handle.set_menu(build_menu(&menu_handle)?)?;

            // Restore last window geometry (size/pos/maximized) if we have it.
            let saved = window_state::load(&config_dir);
            let mut builder =
                WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
                    .title("Anban Creator")
                    .min_inner_size(960.0, 640.0)
                    .initialization_script(init_script(&api_base_for_script));
            if let Some(s) = &saved {
                builder = builder.inner_size(s.width as f64, s.height as f64);
            } else {
                builder = builder.inner_size(1280.0, 820.0);
            }
            // Label "main" matches the capability in capabilities/default.json.
            let main_window = builder.build()?;
            if let Some(s) = &saved {
                let _ = main_window.set_position(tauri::PhysicalPosition::new(s.x, s.y));
                if s.maximized {
                    let _ = main_window.maximize();
                }
            }

            // Persist geometry on resize/move, throttled to once/second (those
            // events fire continuously during a drag/resize).
            let throttle = Arc::new(window_state::SaveThrottle::new(std::time::Duration::from_secs(1)));
            let win = main_window.clone();
            let cd = config_dir.clone();
            main_window.on_window_event(move |event| {
                if matches!(event, tauri::WindowEvent::Resized(_) | tauri::WindowEvent::Moved(_))
                    && throttle.allow()
                {
                    let size = win.inner_size().unwrap_or_default();
                    let pos = win.outer_position().unwrap_or_default();
                    let maximized = win.is_maximized().unwrap_or(false);
                    window_state::save(
                        &cd,
                        &window_state::WindowState {
                            width: size.width,
                            height: size.height,
                            x: pos.x,
                            y: pos.y,
                            maximized,
                        },
                    );
                }
            });

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
