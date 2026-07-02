use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{Duration, Instant};

/// Persisted main-window geometry so size/position/maximized state survive a
/// restart. Stored as physical pixels (round-trips exactly on the same display;
/// if the DPI changed between sessions it's only slightly off — acceptable).
#[derive(Default, Serialize, Deserialize)]
pub struct WindowState {
    pub width: u32,
    pub height: u32,
    pub x: i32,
    pub y: i32,
    pub maximized: bool,
}

pub fn path(dir: &Path) -> PathBuf {
    dir.join("window-state.json")
}

/// Load the saved geometry, if any. Missing/unreadable/corrupt → None (the
/// caller falls back to the default window size + centered placement).
pub fn load(dir: &Path) -> Option<WindowState> {
    let text = std::fs::read_to_string(path(dir)).ok()?;
    serde_json::from_str(&text).ok()
}

/// Best-effort persist. Window geometry is non-critical, so failures are logged
/// at debug and swallowed.
pub fn save(dir: &Path, state: &WindowState) {
    let path = path(dir);
    if let Some(parent) = path.parent() {
        if let Err(e) = std::fs::create_dir_all(parent) {
            log::debug!("window-state: create_dir_all failed: {e}");
            return;
        }
    }
    match serde_json::to_string_pretty(state) {
        Ok(text) => {
            if let Err(e) = std::fs::write(&path, text) {
                log::debug!("window-state: write failed: {e}");
            }
        }
        Err(e) => log::debug!("window-state: serialize failed: {e}"),
    }
}

/// Bounds how often we persist geometry during a continuous resize/drag (those
/// events fire many times per second). Allows at most one save per `min_interval`.
pub struct SaveThrottle {
    last: Mutex<Option<Instant>>,
    min_interval: Duration,
}

impl SaveThrottle {
    pub fn new(min_interval: Duration) -> Self {
        Self {
            last: Mutex::new(None),
            min_interval,
        }
    }

    pub fn allow(&self) -> bool {
        let mut guard = match self.last.lock() {
            Ok(g) => g,
            Err(_) => return false,
        };
        let now = Instant::now();
        let allow = guard.map_or(true, |t| now.duration_since(t) >= self.min_interval);
        if allow {
            *guard = Some(now);
        }
        allow
    }
}
