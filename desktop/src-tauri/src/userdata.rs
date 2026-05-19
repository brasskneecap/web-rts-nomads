// §7 — per-OS user-data directory resolution + writability check.
//
// Resolves:
//   Windows:  %APPDATA%/Nomads/
//   macOS:    ~/Library/Application Support/Nomads/
//   Linux:    ~/.local/share/Nomads/
//
// Plus subdirectories: profiles/, logs/, runtime/ (Phase 2 IPC socket lives
// inside runtime/ per design D24 + the desktop-shell ACL requirement).

use std::env;
use std::fs;
use std::path::PathBuf;

#[derive(Debug, Clone)]
pub struct Paths {
    pub root: PathBuf,
    pub profiles: PathBuf,
    pub logs: PathBuf,
    /// IPC socket / runtime files. Created at startup with 0o700 perms on
    /// Unix per the desktop-shell ACL requirement. Read by §8 (Phase 2)
    /// when the IPCBridge listener is constructed.
    #[allow(dead_code)]
    pub runtime: PathBuf,
    pub settings_file: PathBuf,
}

pub fn resolve() -> Result<Paths, ResolveError> {
    let root = os_root()?;
    let profiles = root.join("profiles");
    let logs = root.join("logs");
    let runtime = root.join("runtime");
    let settings_file = root.join("settings.json");

    fs::create_dir_all(&profiles)
        .map_err(|e| ResolveError::CreateDir(profiles.clone(), e))?;
    fs::create_dir_all(&logs).map_err(|e| ResolveError::CreateDir(logs.clone(), e))?;
    fs::create_dir_all(&runtime).map_err(|e| ResolveError::CreateDir(runtime.clone(), e))?;

    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        // runtime/ must be 0700 per the desktop-shell IPC ACL requirement.
        let mut perms = fs::metadata(&runtime)
            .map_err(|e| ResolveError::ChmodRuntime(runtime.clone(), e))?
            .permissions();
        perms.set_mode(0o700);
        fs::set_permissions(&runtime, perms)
            .map_err(|e| ResolveError::ChmodRuntime(runtime.clone(), e))?;
    }

    Ok(Paths {
        root,
        profiles,
        logs,
        runtime,
        settings_file,
    })
}

#[cfg(target_os = "windows")]
fn os_root() -> Result<PathBuf, ResolveError> {
    let appdata = env::var_os("APPDATA").ok_or(ResolveError::NoUserDir("%APPDATA%"))?;
    Ok(PathBuf::from(appdata).join("Nomads"))
}

#[cfg(target_os = "macos")]
fn os_root() -> Result<PathBuf, ResolveError> {
    let home = env::var_os("HOME").ok_or(ResolveError::NoUserDir("$HOME"))?;
    Ok(PathBuf::from(home)
        .join("Library")
        .join("Application Support")
        .join("Nomads"))
}

#[cfg(all(unix, not(target_os = "macos")))]
fn os_root() -> Result<PathBuf, ResolveError> {
    if let Some(xdg) = env::var_os("XDG_DATA_HOME") {
        return Ok(PathBuf::from(xdg).join("Nomads"));
    }
    let home = env::var_os("HOME").ok_or(ResolveError::NoUserDir("$HOME"))?;
    Ok(PathBuf::from(home)
        .join(".local")
        .join("share")
        .join("Nomads"))
}

/// Writes a small probe file under root/ and removes it — proves the
/// directory is actually writable before the Go child is spawned (§7 task
/// 7.3). Failure surfaces with the resolved path and OS error.
pub fn ensure_writable(paths: &Paths) -> Result<(), std::io::Error> {
    let probe = paths.root.join(".writable_check");
    fs::write(&probe, b"ok")?;
    fs::remove_file(&probe)?;
    Ok(())
}

#[derive(Debug)]
pub enum ResolveError {
    NoUserDir(&'static str),
    CreateDir(PathBuf, std::io::Error),
    /// Only constructed under `cfg(unix)` when `chmod 0o700` on `runtime/`
    /// fails. The variant exists on every platform so callers can match
    /// exhaustively; on Windows it's just never produced.
    #[allow(dead_code)]
    ChmodRuntime(PathBuf, std::io::Error),
}

impl std::fmt::Display for ResolveError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ResolveError::NoUserDir(env) => write!(f, "environment variable {env} is unset"),
            ResolveError::CreateDir(p, e) => write!(f, "create_dir_all({}): {e}", p.display()),
            ResolveError::ChmodRuntime(p, e) => write!(f, "set runtime perms({}): {e}", p.display()),
        }
    }
}

impl std::error::Error for ResolveError {}
