// §22 — diagnostics logging.
//
// Per-process responsibilities:
//   - Shell:       <timestamp>-shell.log via env_logger writing to file
//                  (tasks 22.1, 22.7 — panic hook flushes to same file)
//   - Go child:    <timestamp>-server.log via stdout/stderr tee in supervisor
//                  (task 22.2)
//   - SPA:         in-SPA ring buffer flushed via desktopBridge.appendLog
//                  (task 22.3 — SPA-side, lands with §15)
//
// Rotation (task 22.5): on every launch, delete oldest complete run-triples
// (all three files sharing the same timestamp prefix) until the logs dir is
// at or below LOG_DIR_CAP_BYTES. Files from the current run are never
// deleted.

use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{SystemTime, UNIX_EPOCH};

use log::{info, warn};

/// Process-wide slot for the shell log handle. Set once at startup by
/// `init` so any thread / module can write diagnostic lines that
/// actually reach `<ts>-shell.log` (the `info!()` macro writes via
/// env_logger to stderr, which is detached in windowed release builds
/// and never reaches the file). Used by modules that don't have direct
/// access to LogSession — notably the steam_net Worker thread.
static SHELL_LOG_SLOT: OnceLock<ShellLogHandle> = OnceLock::new();

/// Returns the process-wide shell log handle if `init` has been called.
/// Cheap clone (Arc-backed internally). Returns None during tests or
/// before init completes.
pub fn current_shell_log() -> Option<ShellLogHandle> {
    SHELL_LOG_SLOT.get().cloned()
}

/// 200 MiB cap on the logs/ directory per task 22.5.
pub const LOG_DIR_CAP_BYTES: u64 = 200 * 1024 * 1024;

/// Per-launch handle to the shell log file. Cloned into the panic hook so the
/// final panic message is written even when the regular logger has stopped.
#[derive(Clone)]
pub struct ShellLogHandle {
    pub path: PathBuf,
    file: Arc<Mutex<File>>,
}

impl ShellLogHandle {
    pub fn write_line(&self, level: &str, line: &str) {
        if let Ok(mut f) = self.file.lock() {
            let _ = writeln!(f, "[{level}] {line}");
            let _ = f.flush();
        }
    }
}

/// Per-launch session info. The same timestamp prefix is used across the
/// shell, server, and SPA log files so a single run's triple of files can be
/// found and rotated as a unit.
pub struct LogSession {
    pub timestamp: String,
    pub shell_log: ShellLogHandle,
    pub server_log_path: PathBuf,
}

/// Initialise the per-launch log session. Creates <timestamp>-shell.log and
/// returns paths for the supervisor to tee Go stdout/stderr into
/// <timestamp>-server.log. Also rotates by deleting oldest complete triples
/// until the logs dir is under LOG_DIR_CAP_BYTES.
pub fn init(logs_dir: &Path) -> std::io::Result<LogSession> {
    let timestamp = now_filename_stamp();
    let shell_path = logs_dir.join(format!("{timestamp}-shell.log"));
    let server_path = logs_dir.join(format!("{timestamp}-server.log"));

    let shell_file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&shell_path)?;
    let handle = ShellLogHandle {
        path: shell_path,
        file: Arc::new(Mutex::new(shell_file)),
    };
    handle.write_line("INFO", &format!("nomads-desktop session start ts={timestamp}"));
    // Make the handle accessible to threads that don't have a direct
    // reference to LogSession (e.g. the steam_net Worker thread).
    let _ = SHELL_LOG_SLOT.set(handle.clone());

    // §22.5 rotation: protect current run, evict oldest complete triples.
    if let Err(e) = rotate(logs_dir, &timestamp, LOG_DIR_CAP_BYTES) {
        warn!("log rotation: {e}");
    }

    // §22.7 panic hook
    install_panic_hook(handle.clone());

    Ok(LogSession {
        timestamp,
        shell_log: handle,
        server_log_path: server_path,
    })
}

fn now_filename_stamp() -> String {
    // YYYYMMDD-HHMMSS using UTC; trivial format that sorts lexicographically
    // and avoids the chrono dep.
    let secs = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0);
    let (y, mo, d, h, mi, s) = epoch_to_ymdhms(secs);
    format!("{y:04}{mo:02}{d:02}-{h:02}{mi:02}{s:02}")
}

// Civil-from-days algorithm (Howard Hinnant) — keeps us free of chrono. Good
// for any plausible Unix epoch second value, returns UTC date components.
fn epoch_to_ymdhms(secs: u64) -> (i32, u32, u32, u32, u32, u32) {
    let days = (secs / 86400) as i64;
    let day_of_seconds = (secs % 86400) as u32;
    let h = day_of_seconds / 3600;
    let mi = (day_of_seconds % 3600) / 60;
    let s = day_of_seconds % 60;
    // Shift to 0000-03-01 epoch so leap-year math is simpler.
    let z = days + 719468;
    let era = if z >= 0 { z } else { z - 146096 } / 146097;
    let doe = (z - era * 146097) as u32;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y0 = yoe as i64 + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = (doy - (153 * mp + 2) / 5 + 1) as u32;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    let y = (y0 + if m <= 2 { 1 } else { 0 }) as i32;
    (y, m, d, h, mi, s)
}

fn install_panic_hook(handle: ShellLogHandle) {
    let prev_hook = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |info| {
        let location = info
            .location()
            .map(|l| format!("{}:{}", l.file(), l.line()))
            .unwrap_or_else(|| "<unknown>".to_string());
        let payload = info
            .payload()
            .downcast_ref::<&str>()
            .map(|s| s.to_string())
            .or_else(|| info.payload().downcast_ref::<String>().cloned())
            .unwrap_or_else(|| "<no message>".to_string());
        handle.write_line("PANIC", &format!("at {location}: {payload}"));
        prev_hook(info);
    }));
}

/// Rotation: scan the logs dir, group files by timestamp prefix, and remove
/// the oldest groups until total size is at or below cap. Never removes files
/// in the current_ts group.
pub fn rotate(logs_dir: &Path, current_ts: &str, cap_bytes: u64) -> std::io::Result<()> {
    let entries = match fs::read_dir(logs_dir) {
        Ok(it) => it,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(e) => return Err(e),
    };

    // Group files by timestamp prefix (the part before the first '-' after
    // YYYYMMDD-HHMMSS). Track size per group for eviction.
    let mut groups: std::collections::BTreeMap<String, (u64, Vec<PathBuf>)> = Default::default();
    for entry in entries.flatten() {
        let path = entry.path();
        let Some(name) = path.file_name().and_then(|n| n.to_str()) else {
            continue;
        };
        if let Some(stamp) = extract_timestamp(name) {
            let meta = entry.metadata()?;
            let group = groups.entry(stamp).or_default();
            group.0 += meta.len();
            group.1.push(path);
        }
    }

    let total_size: u64 = groups.values().map(|(s, _)| *s).sum();
    if total_size <= cap_bytes {
        return Ok(());
    }

    // Iterate oldest-first (BTreeMap is sorted by key — timestamps sort
    // lexicographically and chronologically).
    let mut current_size = total_size;
    for (ts, (size, paths)) in groups.iter() {
        if current_size <= cap_bytes {
            break;
        }
        if ts == current_ts {
            continue; // never delete current run
        }
        for p in paths {
            let _ = fs::remove_file(p);
        }
        info!("logs: rotated out triple {ts} ({size} bytes)");
        current_size = current_size.saturating_sub(*size);
    }
    Ok(())
}

fn extract_timestamp(name: &str) -> Option<String> {
    // Filename shape: <YYYYMMDD-HHMMSS>-{shell,server,spa}.log
    // Take the first 15 chars iff they match YYYYMMDD-HHMMSS.
    if name.len() < 16 {
        return None;
    }
    let stamp = &name[..15];
    let valid = stamp.chars().enumerate().all(|(i, c)| {
        if i == 8 {
            c == '-'
        } else {
            c.is_ascii_digit()
        }
    });
    if !valid {
        return None;
    }
    Some(stamp.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    fn unique_dir(name: &str) -> PathBuf {
        let p = std::env::temp_dir().join(format!(
            "nomads-logs-test-{}-{}-{}",
            name,
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        fs::create_dir_all(&p).unwrap();
        p
    }

    #[test]
    fn extract_timestamp_happy() {
        assert_eq!(extract_timestamp("20260517-160837-shell.log").as_deref(), Some("20260517-160837"));
        assert_eq!(extract_timestamp("20260517-160837-server.log").as_deref(), Some("20260517-160837"));
    }

    #[test]
    fn extract_timestamp_rejects_garbage() {
        assert!(extract_timestamp("README.md").is_none());
        assert!(extract_timestamp("server.log").is_none());
        assert!(extract_timestamp("xxxxxxxx-xxxxxx-shell.log").is_none());
    }

    #[test]
    fn rotate_keeps_current_run_evicts_oldest() {
        let dir = unique_dir("rotate");
        // Create 3 triples of timestamps with 1 MB files each.
        for ts in &["20260101-000000", "20260102-000000", "20260103-000000"] {
            for kind in &["shell", "server", "spa"] {
                let p = dir.join(format!("{ts}-{kind}.log"));
                fs::write(&p, vec![0u8; 1024 * 1024]).unwrap();
            }
        }
        // Cap at 5 MB → must evict at least the oldest (3 MB worth), keeping
        // 6 MB - 3 MB = 3 MB. Current run is the newest; don't touch it.
        let cap = 5 * 1024 * 1024;
        rotate(&dir, "20260103-000000", cap).unwrap();
        assert!(!dir.join("20260101-000000-shell.log").exists(), "oldest should be gone");
        assert!(dir.join("20260103-000000-shell.log").exists(), "current should remain");
        fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn rotate_never_deletes_current_run_even_above_cap() {
        let dir = unique_dir("current_safe");
        for kind in &["shell", "server", "spa"] {
            let p = dir.join(format!("20260101-000000-{kind}.log"));
            fs::write(&p, vec![0u8; 10 * 1024 * 1024]).unwrap();
        }
        // 30 MB total, cap 5 MB, current is the only triple — must NOT delete it.
        rotate(&dir, "20260101-000000", 5 * 1024 * 1024).unwrap();
        assert!(dir.join("20260101-000000-shell.log").exists());
        fs::remove_dir_all(&dir).ok();
    }

    #[test]
    fn epoch_to_ymdhms_sanity() {
        // Unix epoch zero is 1970-01-01T00:00:00 UTC.
        let (y, mo, d, h, mi, s) = epoch_to_ymdhms(0);
        assert_eq!((y, mo, d, h, mi, s), (1970, 1, 1, 0, 0, 0));
        // 86400 = 1970-01-02T00:00:00 UTC
        let (y, mo, d, _, _, _) = epoch_to_ymdhms(86400);
        assert_eq!((y, mo, d), (1970, 1, 2));
        // 1735689600 = 2025-01-01T00:00:00 UTC
        let (y, mo, d, _, _, _) = epoch_to_ymdhms(1735689600);
        assert_eq!((y, mo, d), (2025, 1, 1));
    }

    #[test]
    fn now_filename_stamp_shape() {
        let s = now_filename_stamp();
        assert_eq!(s.len(), 15, "stamp = {s}");
        assert_eq!(s.chars().nth(8).unwrap(), '-');
        for (i, c) in s.chars().enumerate() {
            if i == 8 {
                continue;
            }
            assert!(c.is_ascii_digit(), "non-digit at {i} in {s}");
        }
    }
}
