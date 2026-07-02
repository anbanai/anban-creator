use crate::{config::AppConfig, paths::Resources};
use std::path::PathBuf;
use std::sync::atomic::AtomicBool;
use std::sync::Arc;
use tokio::sync::{Mutex, RwLock};
use tokio_util::sync::CancellationToken;

/// Shared state managed by Tauri and accessed by IPC commands + the executor
/// loop. `config` is behind an RwLock so the loop reads live values while
/// commands mutate; `cancel` holds the active loop's cancellation token.
///
/// The lock-bearing fields are `Arc` so async IPC commands can `clone()` an
/// owned handle and `.await` on it without borrowing `State` across the await
/// (Tauri's macro requires async-command futures to be `'static`).
pub struct AppState {
    pub config: Arc<RwLock<AppConfig>>,
    pub config_dir: PathBuf,
    pub resources: Resources,
    pub running: Arc<AtomicBool>,
    pub cancel: Arc<Mutex<Option<CancellationToken>>>,
}

impl AppState {
    pub fn new(config: AppConfig, config_dir: PathBuf, resources: Resources) -> Self {
        Self {
            config: Arc::new(RwLock::new(config)),
            config_dir,
            resources,
            running: Arc::new(AtomicBool::new(false)),
            cancel: Arc::new(Mutex::new(None)),
        }
    }
}
