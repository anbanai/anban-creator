use crate::{config::AppConfig, paths::Resources};
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::atomic::AtomicBool;
use tokio::sync::{Mutex, RwLock};
use tokio_util::sync::CancellationToken;

/// Shared state managed by Tauri and accessed by IPC commands + the executor
/// loop. `config` is behind an RwLock so the loop reads live values while
/// commands mutate; `cancel` holds the active loop's cancellation token.
pub struct AppState {
    pub config: Arc<RwLock<AppConfig>>,
    pub config_dir: PathBuf,
    pub resources: Resources,
    pub running: Arc<AtomicBool>,
    pub cancel: Mutex<Option<CancellationToken>>,
}

impl AppState {
    pub fn new(config: AppConfig, config_dir: PathBuf, resources: Resources) -> Self {
        Self {
            config: Arc::new(RwLock::new(config)),
            config_dir,
            resources,
            running: Arc::new(AtomicBool::new(false)),
            cancel: Mutex::new(None),
        }
    }
}
