// §6 — Go child supervisor.
//
// Spawns the sidecar Go server, parses NOMADS_READY from stdout, captures
// the last 200 stderr lines into a ring buffer, and provides shutdown
// (stdin-close → wait 5 s → kill) and crash detection (raise a Tauri event
// when the child exits while the window is open).
//
// The shell uses std::process::Command directly rather than
// tauri-plugin-shell's sidecar wrapper because we need explicit control over
// the stdin handle (closing stdin is the documented shutdown signal per
// design D7; tauri-plugin-shell's CommandChild does not expose
// drop-the-stdin-pipe as a first-class operation).

use std::collections::VecDeque;
use std::env;
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::process::{Child, ChildStdin, Command, Stdio};
use std::sync::mpsc::{self, RecvTimeoutError};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

use log::{info, warn};

use crate::userdata::Paths;

/// Stderr ring buffer cap. The full buffer surfaces in the
/// child-fails-to-start error so we can put it in the diagnostic modal.
const STDERR_RING_CAP: usize = 200;
/// Time to wait for NOMADS_READY before declaring startup failed.
const READY_TIMEOUT: Duration = Duration::from_secs(10);
/// Time to wait for the Go child to exit after stdin close before kill.
const SHUTDOWN_GRACE: Duration = Duration::from_secs(5);

#[derive(Debug)]
pub struct ReadyInfo {
    pub url: String,
    pub version: String,
}

/// Handle to the running Go child. Drop calls shutdown (close stdin → grace →
/// kill). The setup callback stashes this in Tauri's state so subsequent
/// commands and the window-close handler can find it.
pub struct ChildHandle {
    /// Held so we can drop it to close stdin (EOF → server shutdown).
    stdin: Option<ChildStdin>,
    /// Shared with the crash-watcher thread; allows shutdown() and the
    /// watcher to coordinate without racing on Child ownership.
    pub child: Arc<Mutex<Child>>,
    /// Captured stderr ring buffer; readable for diagnostics.
    pub stderr_ring: Arc<Mutex<VecDeque<String>>>,
    /// Flag set by shutdown() so the crash watcher knows an exit was intentional.
    pub shutdown_initiated: Arc<std::sync::atomic::AtomicBool>,
}

impl ChildHandle {
    /// Initiate graceful shutdown: close stdin, wait up to SHUTDOWN_GRACE for
    /// the child to exit, then kill. Idempotent.
    pub fn shutdown(&mut self) {
        self.shutdown_initiated
            .store(true, std::sync::atomic::Ordering::SeqCst);
        // Step 1: close stdin (signals the Go child's watchStdin goroutine).
        if let Some(stdin) = self.stdin.take() {
            drop(stdin);
            info!("supervisor: stdin closed, waiting up to {:?}", SHUTDOWN_GRACE);
        }

        // Step 2: poll for exit up to SHUTDOWN_GRACE.
        let start = std::time::Instant::now();
        loop {
            let mut child = self.child.lock().unwrap();
            match child.try_wait() {
                Ok(Some(status)) => {
                    info!("supervisor: child exited cleanly (status={status})");
                    return;
                }
                Ok(None) => {
                    if start.elapsed() >= SHUTDOWN_GRACE {
                        warn!("supervisor: grace period elapsed, killing child");
                        let _ = child.kill();
                        let _ = child.wait();
                        return;
                    }
                }
                Err(e) => {
                    warn!("supervisor: try_wait failed: {e}; killing child");
                    let _ = child.kill();
                    let _ = child.wait();
                    return;
                }
            }
            drop(child);
            thread::sleep(Duration::from_millis(50));
        }
    }
}

impl Drop for ChildHandle {
    fn drop(&mut self) {
        self.shutdown();
    }
}

#[derive(Debug)]
pub enum SupervisorError {
    SidecarNotFound { searched: Vec<PathBuf> },
    SpawnFailed(std::io::Error),
    ReadyTimeout { stderr_tail: Vec<String> },
    ChildExitedBeforeReady { stderr_tail: Vec<String> },
}

impl std::fmt::Display for SupervisorError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SupervisorError::SidecarNotFound { searched } => {
                write!(f, "sidecar binary not found; searched: ")?;
                for p in searched {
                    write!(f, "\n  {}", p.display())?;
                }
                Ok(())
            }
            SupervisorError::SpawnFailed(e) => write!(f, "spawn failed: {e}"),
            SupervisorError::ReadyTimeout { stderr_tail } => {
                writeln!(
                    f,
                    "server did not print NOMADS_READY within {:?}",
                    READY_TIMEOUT
                )?;
                write_stderr_tail(f, stderr_tail)
            }
            SupervisorError::ChildExitedBeforeReady { stderr_tail } => {
                writeln!(f, "server exited before printing NOMADS_READY")?;
                write_stderr_tail(f, stderr_tail)
            }
        }
    }
}

impl std::error::Error for SupervisorError {}

fn write_stderr_tail(
    f: &mut std::fmt::Formatter<'_>,
    tail: &[String],
) -> std::fmt::Result {
    if tail.is_empty() {
        writeln!(f, "stderr was empty")?;
    } else {
        writeln!(f, "last {} stderr lines:", tail.len())?;
        for line in tail {
            writeln!(f, "  {line}")?;
        }
    }
    Ok(())
}

/// Spawns the Go server, blocks until NOMADS_READY or timeout, and returns
/// both the parsed ready info and an owned ChildHandle the caller stashes in
/// Tauri state. Caller is responsible for window-close → ChildHandle::shutdown.
///
/// `server_log_path` (§22 task 22.2) is the file the supervisor tees the Go
/// child's stdout+stderr into, after the NOMADS_READY line has been consumed.
/// Pass None to skip the tee (tests).
pub fn spawn_and_wait_ready(
    paths: &Paths,
    server_log_path: Option<std::path::PathBuf>,
) -> Result<(ReadyInfo, ChildHandle), SupervisorError> {
    let bin = resolve_sidecar_path()?;
    info!("supervisor: spawning {}", bin.display());

    let mut cmd = Command::new(&bin);
    cmd.env("WEBRTS_PORT", "0")
        .env("WEBRTS_PROFILES_DIR", &paths.profiles)
        .env("WEBRTS_LOGS_DIR", &paths.logs)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());

    // Windows: the Go binary is built with the console subsystem, so without
    // this flag Windows would attach a new console window to the spawned
    // child when the parent (us) is GUI-subsystem. CREATE_NO_WINDOW (0x0800_0000)
    // suppresses that. No-op everywhere else.
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x0800_0000;
        cmd.creation_flags(CREATE_NO_WINDOW);
    }

    let mut child = cmd.spawn().map_err(SupervisorError::SpawnFailed)?;
    let stdin = child.stdin.take();
    let stdout = child.stdout.take().expect("stdout was piped");
    let stderr = child.stderr.take().expect("stderr was piped");

    let stderr_ring: Arc<Mutex<VecDeque<String>>> =
        Arc::new(Mutex::new(VecDeque::with_capacity(STDERR_RING_CAP)));

    // Shared file writer for §22.2 tee (None disables file logging in tests).
    let log_writer: Option<Arc<Mutex<std::fs::File>>> = server_log_path
        .as_ref()
        .and_then(|p| std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(p)
            .map_err(|e| warn!("supervisor: open server log {}: {e}", p.display()))
            .ok())
        .map(|f| Arc::new(Mutex::new(f)));

    // Stderr pump: tail into ring buffer + write to server log if open.
    let stderr_ring_clone = stderr_ring.clone();
    let stderr_log = log_writer.clone();
    thread::spawn(move || {
        let reader = BufReader::new(stderr);
        for line in reader.lines().map_while(Result::ok) {
            if let Some(w) = &stderr_log {
                if let Ok(mut f) = w.lock() {
                    use std::io::Write;
                    let _ = writeln!(f, "[stderr] {line}");
                }
            }
            let mut ring = stderr_ring_clone.lock().unwrap();
            if ring.len() == STDERR_RING_CAP {
                ring.pop_front();
            }
            ring.push_back(line);
        }
    });

    // Stdout pump: scan for NOMADS_READY, then idle (lets the child keep writing).
    let (tx, rx) = mpsc::channel::<Result<ReadyInfo, SupervisorError>>();
    let stderr_ring_for_exit = stderr_ring.clone();
    let child_arc = Arc::new(Mutex::new(child));
    let child_for_exit_watcher = child_arc.clone();

    let stdout_log = log_writer.clone();
    thread::spawn(move || {
        let reader = BufReader::new(stdout);
        let mut found = false;
        for line in reader.lines().map_while(Result::ok) {
            if !found {
                if let Some(info) = parse_ready_line(&line) {
                    let _ = tx.send(Ok(info));
                    found = true;
                    continue;
                }
            }
            // After ready, tee to server log (§22.2) and debug-log.
            if let Some(w) = &stdout_log {
                if let Ok(mut f) = w.lock() {
                    use std::io::Write;
                    let _ = writeln!(f, "[stdout] {line}");
                }
            }
            log::debug!("server stdout: {line}");
        }
        // Stdout closed → child exited. If we never saw ready, surface that.
        if !found {
            let tail = stderr_ring_for_exit
                .lock()
                .unwrap()
                .iter()
                .cloned()
                .collect();
            let _ = tx.send(Err(SupervisorError::ChildExitedBeforeReady {
                stderr_tail: tail,
            }));
        }
    });

    let result = match rx.recv_timeout(READY_TIMEOUT) {
        Ok(v) => v,
        Err(RecvTimeoutError::Timeout) => Err(SupervisorError::ReadyTimeout {
            stderr_tail: stderr_ring.lock().unwrap().iter().cloned().collect(),
        }),
        Err(RecvTimeoutError::Disconnected) => Err(SupervisorError::ChildExitedBeforeReady {
            stderr_tail: stderr_ring.lock().unwrap().iter().cloned().collect(),
        }),
    };

    match result {
        Ok(info) => {
            let handle = ChildHandle {
                stdin,
                child: child_for_exit_watcher,
                stderr_ring,
                shutdown_initiated: Arc::new(std::sync::atomic::AtomicBool::new(false)),
            };
            Ok((info, handle))
        }
        Err(e) => {
            // Make sure we don't leak the child on failure.
            let mut child = child_arc.lock().unwrap();
            let _ = child.kill();
            let _ = child.wait();
            Err(e)
        }
    }
}

/// parse_ready_line returns Some(ReadyInfo) iff line is exactly the
/// NOMADS_READY format. Strict; anything malformed yields None so the
/// scanner skips it and keeps looking.
pub fn parse_ready_line(line: &str) -> Option<ReadyInfo> {
    let rest = line.strip_prefix("NOMADS_READY ")?;
    let mut url = None;
    let mut version = None;
    for part in rest.split_whitespace() {
        if let Some(v) = part.strip_prefix("url=") {
            url = Some(v.to_string());
        } else if let Some(v) = part.strip_prefix("version=") {
            version = Some(v.to_string());
        }
    }
    Some(ReadyInfo {
        url: url?,
        version: version?,
    })
}

fn resolve_sidecar_path() -> Result<PathBuf, SupervisorError> {
    let triple = current_target_triple();
    let base = "nomads-server";
    let ext = if cfg!(windows) { ".exe" } else { "" };
    // Tauri's bundler strips the target-triple suffix when copying sidecars
    // into the bundled app, so the file next to the running shell is named
    // `nomads-server.exe` rather than `nomads-server-<triple>.exe`. The dev
    // iteration path (binaries/ under cargo's cwd) keeps the triple suffix
    // because that's where we stage it before `cargo tauri build` copies it.
    let bundled_name = format!("{base}{ext}");
    let staged_name = format!("{base}-{triple}{ext}");

    let mut searched = Vec::new();

    // 1. Bundled / installed: alongside the running shell binary. Try both
    //    naming conventions so the same code path handles bundled artefacts
    //    and a developer running target/release/nomads-desktop.exe directly.
    if let Ok(exe) = env::current_exe() {
        if let Some(dir) = exe.parent() {
            for name in [&bundled_name, &staged_name] {
                let candidate = dir.join(name);
                if candidate.is_file() {
                    return Ok(candidate);
                }
                searched.push(candidate);
            }
        }
    }

    // 2. Dev: src-tauri/binaries/, relative to cargo's working dir. Tauri's
    //    sidecar convention requires the triple suffix here.
    let dev = PathBuf::from("binaries").join(&staged_name);
    if dev.is_file() {
        return Ok(dev);
    }
    searched.push(dev);

    // 3. Last resort: src-tauri/binaries/ relative to env::current_dir().
    if let Ok(cwd) = env::current_dir() {
        let candidate = cwd.join("binaries").join(&staged_name);
        if candidate.is_file() {
            return Ok(candidate);
        }
        searched.push(candidate);
    }

    Err(SupervisorError::SidecarNotFound { searched })
}

fn current_target_triple() -> &'static str {
    // Generated by tauri-build into env at compile time.
    env!("TAURI_ENV_TARGET_TRIPLE")
}

#[allow(dead_code)]
fn binary_basename() -> &'static str {
    if cfg!(target_os = "windows") {
        "nomads-server.exe"
    } else {
        "nomads-server"
    }
}

#[allow(dead_code)]
fn sidecar_resource_path() -> &'static Path {
    Path::new("binaries")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_ready_line_happy_path() {
        let info = parse_ready_line("NOMADS_READY url=http://127.0.0.1:51092 version=abc123")
            .expect("expected Some");
        assert_eq!(info.url, "http://127.0.0.1:51092");
        assert_eq!(info.version, "abc123");
    }

    #[test]
    fn parse_ready_line_ignores_non_prefixed_lines() {
        assert!(parse_ready_line("2026/05/17 16:08:37 server listening on [::]:51092").is_none());
        assert!(parse_ready_line("ready").is_none());
        assert!(parse_ready_line("").is_none());
    }

    #[test]
    fn parse_ready_line_rejects_missing_url() {
        assert!(parse_ready_line("NOMADS_READY version=abc").is_none());
    }

    #[test]
    fn parse_ready_line_rejects_missing_version() {
        assert!(parse_ready_line("NOMADS_READY url=http://127.0.0.1:1234").is_none());
    }

    #[test]
    fn parse_ready_line_accepts_arbitrary_field_order() {
        let info = parse_ready_line("NOMADS_READY version=v1.2.3 url=http://127.0.0.1:1").unwrap();
        assert_eq!(info.url, "http://127.0.0.1:1");
        assert_eq!(info.version, "v1.2.3");
    }
}
