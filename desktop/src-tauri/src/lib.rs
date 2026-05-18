// Nomads Tauri shell entrypoint. The shell is responsible for:
//   * spawning and supervising the Go server child process (§6)
//   * resolving the OS user-data dir, settings, and logs dirs (§7)
//   * (Phase 2) Steamworks init, IPC channel, and lobby/achievement bridging
//
// Rule (AI_RULES.md): NO game logic in this crate. It's window + sidecar
// supervisor + (Phase 2) Steam wrapper. Anything game-related belongs in the
// Go server.

#[cfg(feature = "steam")]
mod ipc;
mod logs;
mod migration;
mod settings;
#[cfg(feature = "steam")]
mod steam;
mod supervisor;
mod userdata;

/// Steam appid for development / playtest builds. 480 is Valve's public
/// Spacewar test appid — every Steamworks SDK feature works against it
/// without a paid developer registration. Replace with your real appid for
/// production releases (one-line change here; no other code edits needed).
#[cfg(feature = "steam")]
const STEAM_APPID: u32 = 480;

use log::{info, warn};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();

    info!("nomads-desktop starting");

    // §8.1: SteamAPI_RestartAppIfNecessary MUST be the first Steam SDK call.
    // If Steam wants to relaunch us with the correct appid attribution we
    // exit immediately — no window, no Go child, no Steamworks teardown.
    #[cfg(feature = "steam")]
    {
        if steam::restart_if_necessary(STEAM_APPID) {
            info!("steam: SteamAPI_RestartAppIfNecessary returned true; exiting so Steam can relaunch us");
            std::process::exit(0);
        }
    }

    let paths = match userdata::resolve() {
        Ok(p) => p,
        Err(e) => {
            // Pre-launch error modal is wired in §7 task 7.3 once the writable
            // check fails. For now the failure is fatal with a logged reason.
            eprintln!("FATAL: could not resolve user-data directory: {e}");
            std::process::exit(1);
        }
    };
    if let Err(e) = userdata::ensure_writable(&paths) {
        eprintln!("FATAL: user-data directory not writable ({}): {e}", paths.root.display());
        std::process::exit(1);
    }
    info!("user-data root: {}", paths.root.display());
    info!("profiles dir:   {}", paths.profiles.display());
    info!("logs dir:       {}", paths.logs.display());

    // §22 — per-launch log session (shell + server file paths, rotation,
    // panic hook). The shell-side env_logger output is the canonical session
    // log; supervisor.rs tees Go child output into <ts>-server.log.
    let log_session = match logs::init(&paths.logs) {
        Ok(s) => s,
        Err(e) => {
            eprintln!("log init failed at {}: {e}", paths.logs.display());
            std::process::exit(1);
        }
    };
    info!("log session: {}", log_session.timestamp);
    info!("shell log file: {}", log_session.shell_log.path.display());
    info!("server log file: {}", log_session.server_log_path.display());

    // §7 task 7.6: one-time legacy profile migration.
    let report = migration::run_once_if_needed(&paths.profiles);
    if !report.files_copied.is_empty() {
        info!(
            "migrated {} legacy profile file(s) from {}",
            report.files_copied.len(),
            report.source.as_ref().map(|p| p.display().to_string()).unwrap_or_default()
        );
    }

    // §7 task 7.5: settings store, used by future IPC commands.
    let settings_store = std::sync::Arc::new(settings::SettingsStore::open(paths.settings_file.clone()));
    info!("settings file: {}", settings_store.path.display());

    // §8.2: SteamAPI_Init + callback pump. Best-effort — failure (Steam not
    // running, SDK link error, appid not owned) is logged and we continue
    // offline; getSteamPlayer() returns null and the SPA hides Steam-mode
    // lobby entries.
    //
    // Log to the shell log file via the explicit write_line because
    // env_logger's stderr output is dropped under windows_subsystem =
    // "windows" (no console attached in release).
    #[cfg(feature = "steam")]
    let steam_bridge = match steam::Bridge::init(STEAM_APPID) {
        Ok(b) => {
            let p = b.local_player();
            let msg = format!(
                "steam: initialised as {} (steamid={})",
                p.persona_name, p.steam_id_64
            );
            info!("{msg}");
            log_session.shell_log.write_line("INFO", &msg);
            Some(std::sync::Arc::new(b))
        }
        Err(e) => {
            let msg = format!("steam: init failed, continuing in offline mode: {e}");
            warn!("{msg}");
            log_session.shell_log.write_line("WARN", &msg);
            None
        }
    };

    // §8.3: spawn the IPC listener so the Go child can reach the Steam
    // bridge. We start it even when steam_bridge is None — the dispatcher
    // returns steam_unavailable for Steam-side methods, but the channel
    // itself is up so the Go server's IPCBridge can construct without
    // hanging on connect.
    #[cfg(feature = "steam")]
    let ipc_path = match ipc::start(&paths.runtime, steam_bridge.clone()) {
        Ok(p) => {
            log_session.shell_log.write_line("INFO", &format!("ipc: socket at {p}"));
            Some(p)
        }
        Err(e) => {
            warn!("ipc: listener failed to start: {e}");
            log_session.shell_log.write_line(
                "WARN",
                &format!("ipc: listener failed to start: {e}; Go side falls back to NoopBridge"),
            );
            None
        }
    };
    #[cfg(not(feature = "steam"))]
    let ipc_path: Option<String> = None;

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(move |app| {
            // §6: spawn the Go child and wait for NOMADS_READY before the
            // window is shown. The supervisor blocks setup for up to 10 s; on
            // success it returns the URL to load and a handle for shutdown.
            let (ready, handle) = match supervisor::spawn_and_wait_ready(
                &paths,
                Some(log_session.server_log_path.clone()),
                ipc_path.clone(),
            ) {
                Ok(v) => v,
                Err(e) => {
                    warn!("server failed to start: {e}");
                    return Err(Box::new(std::io::Error::new(
                        std::io::ErrorKind::Other,
                        e.to_string(),
                    )));
                }
            };
            info!("server ready: url={} version={}", ready.url, ready.version);

            // Load the main window with the server URL.
            use tauri::{Manager, WebviewUrl, WebviewWindowBuilder};
            if let Some(window) = app.get_webview_window("main") {
                let url = ready.url.parse::<tauri::Url>().expect("ready url parses");
                if let Err(e) = window.navigate(url) {
                    warn!("navigate main window: {e}");
                }
                if let Err(e) = window.show() {
                    warn!("show main window: {e}");
                }
            } else {
                // If the config window was hidden, create one now.
                let url = ready.url.parse::<tauri::Url>().expect("ready url parses");
                let _ = WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))
                    .title("Nomads")
                    .inner_size(1280.0, 800.0)
                    .min_inner_size(1024.0, 600.0)
                    .resizable(true)
                    .build();
            }

            // §6 task 6.4: crash detection. Wait for the child to exit on a
            // dedicated thread; if exit happens before shutdown_initiated is
            // set, emit a `server-crashed` Tauri event so the SPA can render
            // the "Server crashed — click to restart" dialog (§17 task 17.2).
            let app_handle_for_watcher = app.handle().clone();
            let child_for_watcher = handle.child.clone();
            let stderr_for_watcher = handle.stderr_ring.clone();
            let shutdown_flag = handle.shutdown_initiated.clone();
            std::thread::spawn(move || {
                let exit_status = {
                    let mut child = child_for_watcher.lock().unwrap();
                    child.wait()
                };
                let intentional = shutdown_flag.load(std::sync::atomic::Ordering::SeqCst);
                if intentional {
                    info!("supervisor watcher: child exit was intentional ({exit_status:?})");
                    return;
                }
                warn!("supervisor watcher: UNEXPECTED child exit ({exit_status:?})");
                let payload = serde_json::json!({
                    "exitStatus": format!("{:?}", exit_status),
                    "stderrTail": stderr_for_watcher
                        .lock()
                        .unwrap()
                        .iter()
                        .cloned()
                        .collect::<Vec<_>>(),
                });
                if let Err(e) = tauri::Emitter::emit(&app_handle_for_watcher, "server-crashed", payload) {
                    warn!("emit server-crashed event: {e}");
                }
            });

            // Stash the child handle in app state so the window-close handler
            // and other commands can reach it for shutdown.
            app.manage(std::sync::Mutex::new(Some(handle)));
            // Settings store goes into managed state so the get/set IPC
            // commands (registered below) can reach it.
            app.manage(settings_store.clone());
            // Paths go in too — `open_logs_directory` reads the logs path.
            app.manage(std::sync::Arc::new(paths.clone()));
            // Steam bridge (None when feature is off or init failed). The
            // SPA's get_steam_player command branches on Option.
            #[cfg(feature = "steam")]
            app.manage(SteamBridgeState(steam_bridge.clone()));
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            get_settings,
            set_settings,
            open_logs_directory,
            get_logs_directory,
            get_steam_player,
            create_lobby,
            join_lobby,
            open_invite_overlay
        ])
        .on_window_event(|window, event| {
            use tauri::{Manager, WindowEvent};
            if let WindowEvent::CloseRequested { .. } = event {
                // §6 task 6.3: close stdin → grace 5s → kill.
                if let Some(state) = window.app_handle().try_state::<std::sync::Mutex<Option<supervisor::ChildHandle>>>() {
                    if let Some(mut h) = state.lock().unwrap().take() {
                        h.shutdown();
                    }
                }
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

// ----- Tauri IPC commands (§7 task 7.5, partial) -----------------------------

#[tauri::command]
fn get_settings(
    store: tauri::State<'_, std::sync::Arc<settings::SettingsStore>>,
) -> settings::SettingsSnapshot {
    store.snapshot()
}

#[tauri::command]
fn set_settings(
    partial: serde_json::Value,
    store: tauri::State<'_, std::sync::Arc<settings::SettingsStore>>,
) -> Result<settings::SettingsSnapshot, String> {
    store.merge_and_save(partial).map_err(|e| e.to_string())
}

// §22 task 22.6: open logs directory in the OS file manager.
#[tauri::command]
fn open_logs_directory(
    paths: tauri::State<'_, std::sync::Arc<userdata::Paths>>,
) -> Result<(), String> {
    let dir = &paths.logs;
    let result = if cfg!(target_os = "windows") {
        std::process::Command::new("explorer").arg(dir).spawn()
    } else if cfg!(target_os = "macos") {
        std::process::Command::new("open").arg(dir).spawn()
    } else {
        std::process::Command::new("xdg-open").arg(dir).spawn()
    };
    result.map(|_| ()).map_err(|e| e.to_string())
}

// §22 task 22.6 companion: returns the absolute logs directory path so the
// SPA's About / Support screen can display it (per task 15.7).
#[tauri::command]
fn get_logs_directory(
    paths: tauri::State<'_, std::sync::Arc<userdata::Paths>>,
) -> String {
    paths.logs.display().to_string()
}

// §8.5 + §14.4: SPA reads this on boot to decide whether to show Steam-mode
// lobby entries. Returns None when:
//   - The `steam` cargo feature is off (no Steamworks linked in)
//   - SteamAPI_Init failed (Steam not running, SDK not linked, etc.)
// Otherwise returns the local player's SteamID64 and persona name.
#[cfg(feature = "steam")]
#[tauri::command]
fn get_steam_player(
    bridge: tauri::State<'_, SteamBridgeState>,
) -> Option<steam::LocalPlayer> {
    bridge.0.as_ref().map(|b| b.local_player())
}

/// Newtype wrapper around the bridge slot so Tauri's State lookup matches
/// unambiguously on a unique type (not the structural Option<Arc<...>>).
#[cfg(feature = "steam")]
pub struct SteamBridgeState(pub Option<std::sync::Arc<steam::Bridge>>);

// ----- Steam lobby commands (§14.1 / Step 3) --------------------------------

#[cfg(feature = "steam")]
#[tauri::command]
async fn create_lobby(
    bridge: tauri::State<'_, SteamBridgeState>,
    max_players: Option<u32>,
) -> Result<String, String> {
    let b = bridge.0.as_ref().ok_or_else(|| "steam_unavailable".to_string())?;
    let max = max_players.unwrap_or(4).clamp(2, 8);
    let (tx, rx) = tokio::sync::oneshot::channel();
    b.client.matchmaking().create_lobby(
        steamworks::LobbyType::FriendsOnly,
        max,
        move |result| {
            let _ = tx.send(
                result
                    .map(|id| id.raw().to_string())
                    .map_err(|e| format!("{e:?}")),
            );
        },
    );
    rx.await
        .map_err(|_| "callback dropped before steam responded".to_string())
        .and_then(|r| r)
}

#[cfg(feature = "steam")]
#[tauri::command]
async fn join_lobby(
    bridge: tauri::State<'_, SteamBridgeState>,
    lobby_id: String,
) -> Result<String, String> {
    let b = bridge.0.as_ref().ok_or_else(|| "steam_unavailable".to_string())?;
    let raw = lobby_id
        .parse::<u64>()
        .map_err(|e| format!("bad lobby id: {e}"))?;
    let (tx, rx) = tokio::sync::oneshot::channel();
    b.client
        .matchmaking()
        .join_lobby(steamworks::LobbyId::from_raw(raw), move |result| {
            let _ = tx.send(
                result
                    .map(|id| id.raw().to_string())
                    .map_err(|_| {
                        "join failed (lobby full / not authorised / no longer exists)".to_string()
                    }),
            );
        });
    rx.await
        .map_err(|_| "callback dropped before steam responded".to_string())
        .and_then(|r| r)
}

#[cfg(feature = "steam")]
#[tauri::command]
fn open_invite_overlay(
    bridge: tauri::State<'_, SteamBridgeState>,
    lobby_id: String,
) -> Result<(), String> {
    let b = bridge.0.as_ref().ok_or_else(|| "steam_unavailable".to_string())?;
    let raw = lobby_id
        .parse::<u64>()
        .map_err(|e| format!("bad lobby id: {e}"))?;
    b.client
        .friends()
        .activate_invite_dialog(steamworks::LobbyId::from_raw(raw));
    Ok(())
}

// Stubs for builds without the steam feature so the invoke_handler! list
// stays constant. All return the "Steam unavailable" sentinel.
#[cfg(not(feature = "steam"))]
#[tauri::command]
fn get_steam_player() -> Option<serde_json::Value> {
    None
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
async fn create_lobby(_max_players: Option<u32>) -> Result<String, String> {
    Err("steam_unavailable".to_string())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
async fn join_lobby(_lobby_id: String) -> Result<String, String> {
    Err("steam_unavailable".to_string())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
fn open_invite_overlay(_lobby_id: String) -> Result<(), String> {
    Err("steam_unavailable".to_string())
}
