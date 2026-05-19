// §7 task 7.5 — settings.json load + save.
//
// Wire-format: JSON object with arbitrary keys. The SPA-controlled keys
// (player_id, window state, audio prefs, etc.) live alongside any
// future-shell-managed keys. The single-writer protocol (design D25) puts
// the SPA in charge of writes; the shell only mutates settings when the SPA
// is unreachable (e.g., last-window-state at close, before the SPA acks).
//
// "Preserve unknown keys" property: load_or_default returns a fully-typed
// SettingsSnapshot whose `extra: Map<String, Value>` field absorbs anything
// not in our known schema. set() merges the partial update INTO the existing
// snapshot rather than replacing it, so a future schema field's value is
// preserved across writes from an older client.

use std::fs;
use std::path::PathBuf;
use std::sync::Mutex;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(default, rename_all = "camelCase")]
pub struct SettingsSnapshot {
    /// Player id (UUID in non-Steam, Steam ID64 in Steam). Migrated from
    /// SPA's localStorage on first packaged launch (§7 task 7.10).
    pub player_id: Option<String>,
    /// Last window state, persisted by the SPA on debounce, restored at next
    /// launch by the shell.
    pub window: Option<WindowState>,
    /// Anything we don't know about. Preserved verbatim across writes so an
    /// older shell doesn't drop fields a newer SPA persisted (D25).
    #[serde(flatten)]
    pub extra: serde_json::Map<String, serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
#[serde(default, rename_all = "camelCase")]
pub struct WindowState {
    pub width: Option<f64>,
    pub height: Option<f64>,
    pub x: Option<f64>,
    pub y: Option<f64>,
    pub maximized: Option<bool>,
}

pub struct SettingsStore {
    pub path: PathBuf,
    inner: Mutex<SettingsSnapshot>,
}

impl SettingsStore {
    /// Open the store at path, loading the existing file or creating a fresh
    /// default snapshot. Never panics on missing/corrupt files — corrupt
    /// files are logged and replaced with the default (we don't want a single
    /// bad write to brick the app; the SPA can re-persist).
    pub fn open(path: PathBuf) -> Self {
        let initial = match fs::read(&path) {
            Ok(bytes) => match serde_json::from_slice::<SettingsSnapshot>(&bytes) {
                Ok(s) => s,
                Err(e) => {
                    log::warn!(
                        "settings: corrupt file at {} ({e}); using defaults",
                        path.display()
                    );
                    SettingsSnapshot::default()
                }
            },
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => SettingsSnapshot::default(),
            Err(e) => {
                log::warn!(
                    "settings: read failed at {} ({e}); using defaults",
                    path.display()
                );
                SettingsSnapshot::default()
            }
        };
        Self {
            path,
            inner: Mutex::new(initial),
        }
    }

    pub fn snapshot(&self) -> SettingsSnapshot {
        self.inner.lock().unwrap().clone()
    }

    /// Merge `partial` into the current snapshot and persist. Unknown keys in
    /// `partial` are preserved in `extra`. Returns the new snapshot.
    pub fn merge_and_save(&self, partial: serde_json::Value) -> Result<SettingsSnapshot, SaveError> {
        let mut current = self.inner.lock().unwrap();
        let mut as_value =
            serde_json::to_value(&*current).map_err(|e| SaveError::Encode(e.to_string()))?;
        merge_in_place(&mut as_value, partial);
        let merged: SettingsSnapshot =
            serde_json::from_value(as_value).map_err(|e| SaveError::Decode(e.to_string()))?;
        let bytes = serde_json::to_vec_pretty(&merged)
            .map_err(|e| SaveError::Encode(e.to_string()))?;
        let tmp = self.path.with_extension("json.tmp");
        fs::write(&tmp, bytes).map_err(|e| SaveError::Write(e.to_string()))?;
        fs::rename(&tmp, &self.path).map_err(|e| SaveError::Write(e.to_string()))?;
        *current = merged.clone();
        Ok(merged)
    }
}

fn merge_in_place(base: &mut serde_json::Value, patch: serde_json::Value) {
    use serde_json::Value;
    match (base, patch) {
        (Value::Object(b), Value::Object(p)) => {
            for (k, v) in p {
                merge_in_place(b.entry(k).or_insert(Value::Null), v);
            }
        }
        (slot, p) => *slot = p,
    }
}

#[derive(Debug)]
pub enum SaveError {
    Encode(String),
    Decode(String),
    Write(String),
}

impl std::fmt::Display for SaveError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SaveError::Encode(s) => write!(f, "settings encode: {s}"),
            SaveError::Decode(s) => write!(f, "settings decode: {s}"),
            SaveError::Write(s) => write!(f, "settings write: {s}"),
        }
    }
}

impl std::error::Error for SaveError {}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn tmp(name: &str) -> PathBuf {
        std::env::temp_dir().join(format!(
            "nomads-settings-test-{}-{}",
            name,
            std::process::id()
        ))
    }

    #[test]
    fn opens_missing_file_with_defaults() {
        let path = tmp("opens_missing");
        let _ = fs::remove_file(&path);
        let store = SettingsStore::open(path.clone());
        let snap = store.snapshot();
        assert!(snap.player_id.is_none());
        let _ = fs::remove_file(&path);
    }

    #[test]
    fn merge_preserves_unknown_keys() {
        let path = tmp("preserves_unknown");
        let _ = fs::remove_file(&path);
        // Write a settings file with a key the current code doesn't know about.
        let initial =
            json!({ "playerId": "abc", "someFutureKey": { "nested": 42 } });
        fs::write(&path, serde_json::to_vec_pretty(&initial).unwrap()).unwrap();

        let store = SettingsStore::open(path.clone());
        let snap = store.snapshot();
        assert_eq!(snap.player_id.as_deref(), Some("abc"));
        assert_eq!(snap.extra.get("someFutureKey").and_then(|v| v.get("nested")).and_then(|v| v.as_i64()), Some(42));

        // Now merge a new partial; the unknown key MUST survive.
        let merged = store
            .merge_and_save(json!({ "playerId": "def" }))
            .expect("save");
        assert_eq!(merged.player_id.as_deref(), Some("def"));
        assert_eq!(merged.extra.get("someFutureKey").and_then(|v| v.get("nested")).and_then(|v| v.as_i64()), Some(42));

        // Re-open from disk to confirm persistence.
        drop(store);
        let store2 = SettingsStore::open(path.clone());
        let reloaded = store2.snapshot();
        assert_eq!(reloaded.player_id.as_deref(), Some("def"));
        assert_eq!(reloaded.extra.get("someFutureKey").and_then(|v| v.get("nested")).and_then(|v| v.as_i64()), Some(42));

        let _ = fs::remove_file(&path);
    }

    #[test]
    fn corrupt_file_falls_back_to_default() {
        let path = tmp("corrupt");
        fs::write(&path, b"NOT JSON").unwrap();
        let store = SettingsStore::open(path.clone());
        assert!(store.snapshot().player_id.is_none());
        let _ = fs::remove_file(&path);
    }

    #[test]
    fn window_state_round_trip() {
        let path = tmp("window_state");
        let _ = fs::remove_file(&path);
        let store = SettingsStore::open(path.clone());
        let merged = store
            .merge_and_save(
                json!({ "window": { "width": 1600.0, "height": 900.0, "maximized": false } }),
            )
            .expect("save");
        let w = merged.window.expect("window present");
        assert_eq!(w.width, Some(1600.0));
        assert_eq!(w.height, Some(900.0));
        assert_eq!(w.maximized, Some(false));
        let _ = fs::remove_file(&path);
    }
}
