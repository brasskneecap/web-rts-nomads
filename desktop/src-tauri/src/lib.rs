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
#[cfg(feature = "steam")]
mod steam_net;
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

    // Smoke-test argv: `--steam-net-selftest host` or
    // `--steam-net-selftest connect:<steamid64>`. When set, we pass the value
    // through to the Go child via NOMADS_SELFTEST; the Go server interprets
    // it (see cmd/api/main.go). Pre-§14 helper for two-machine verification
    // that the Steam Sockets pipeline carries real bytes end to end.
    let selftest = parse_selftest_mode_from_args();
    if let Some(mode) = &selftest {
        info!("nomads-desktop: --steam-net-selftest active (mode={mode})");
    }

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
                selftest.clone(),
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

            // §14.3: LobbyDataUpdate_t callback fires on every member when
            // any lobby metadata changes. We read `match_id`; if set, the
            // host has clicked Start and we emit a Tauri event so the
            // joiner SPA can navigate into the match. Host-side this is a
            // no-op (the host already knows the match_id it just set).
            // The CallbackHandle returned by register_callback must be
            // kept alive — drop = deregister — so we stash it in managed
            // state alongside the bridge.
            #[cfg(feature = "steam")]
            if let Some(bridge) = steam_bridge.as_ref() {
                let app_handle_for_lobby = app.handle().clone();
                let bridge_for_callback = bridge.clone();
                let cb_handle = bridge.client.register_callback(
                    move |evt: steamworks::LobbyDataUpdate| {
                        if !evt.success {
                            return;
                        }
                        // Only act on lobby-scope updates (member==lobby
                        // means the lobby metadata changed; otherwise it's
                        // a per-member update we don't need here).
                        if evt.member.raw() != evt.lobby.raw() {
                            return;
                        }
                        let match_id = bridge_for_callback
                            .client
                            .matchmaking()
                            .lobby_data(evt.lobby, "match_id");
                        if let Some(mid) = match_id {
                            if !mid.is_empty() {
                                let payload = serde_json::json!({
                                    "lobbyId": evt.lobby.raw().to_string(),
                                    "matchId": mid,
                                });
                                if let Err(e) = tauri::Emitter::emit(
                                    &app_handle_for_lobby,
                                    "steam_lobby_started",
                                    payload,
                                ) {
                                    warn!("emit steam_lobby_started: {e}");
                                }
                            }
                        }
                    },
                );
                app.manage(SteamLobbyCallbackHandle(cb_handle));
            }

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
            // Shell log handle in managed state so Tauri commands can
            // write diagnostic lines into <ts>-shell.log without going
            // through env_logger (whose stderr output is detached in
            // windowed release builds and never reaches the log file).
            app.manage(log_session.shell_log.clone());
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
            open_invite_overlay,
            start_steam_game,
            list_steam_lobbies,
            get_steam_lobby_data,
            leave_steam_lobby
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

/// Newtype wrapper around the LobbyDataUpdate callback handle. Held in
/// Tauri's managed state so the callback survives until the app exits —
/// dropping the CallbackHandle deregisters the callback on the Steam SDK
/// side.
#[cfg(feature = "steam")]
struct SteamLobbyCallbackHandle(#[allow(dead_code)] steamworks::CallbackHandle);

// ----- Steam lobby commands (§14.1 / Step 3) --------------------------------

#[cfg(feature = "steam")]
#[tauri::command]
async fn create_lobby(
    bridge: tauri::State<'_, SteamBridgeState>,
    shell_log: tauri::State<'_, logs::ShellLogHandle>,
    max_players: Option<u32>,
    map_id: Option<String>,
    local_lobby_id: Option<String>,
    host_persona: Option<String>,
) -> Result<String, String> {
    let sl = shell_log.inner().clone();
    sl.write_line(
        "INFO",
        &format!(
            "create_lobby: entered (maxPlayers={max_players:?} mapId={map_id:?} localLobbyId={local_lobby_id:?})"
        ),
    );
    let b = bridge
        .0
        .as_ref()
        .ok_or_else(|| {
            sl.write_line(
                "WARN",
                "create_lobby: bridge slot is None — steam_unavailable",
            );
            "steam_unavailable".to_string()
        })?
        .clone();
    let max = max_players.unwrap_or(4).clamp(2, 8);
    let map_id = map_id.unwrap_or_default();
    let local_lobby_id = local_lobby_id.unwrap_or_default();
    let host_persona_param = host_persona.unwrap_or_default();
    let host_steam_id = b.client.user().steam_id().raw();
    sl.write_line(
        "INFO",
        &format!("create_lobby: bridge OK hostSteamId={host_steam_id} max={max}"),
    );
    let host_persona_resolved = if host_persona_param.is_empty() {
        b.client.friends().name()
    } else {
        host_persona_param
    };

    let (tx, rx) = tokio::sync::oneshot::channel();
    sl.write_line(
        "INFO",
        "create_lobby: registering SteamMatchmaking::CreateLobby callback",
    );
    let sl_cb = sl.clone();
    {
        let mm = b.client.matchmaking();
        // TEMP DEBUG: Public so the friend can see the lobby without
        // depending on Steam friend graph + region visibility quirks.
        // Revert to FriendsOnly once we've confirmed the rest of the
        // discovery pipeline works.
        mm.create_lobby(
            steamworks::LobbyType::Public,
            max,
            move |result| {
                sl_cb.write_line(
                    "INFO",
                    &format!("create_lobby: LobbyCreated_t callback fired result={result:?}"),
                );
                let _ = tx.send(
                    result
                        .map(|id| id.raw())
                        .map_err(|e| format!("{e:?}")),
                );
            },
        );
    }
    sl.write_line(
        "INFO",
        "create_lobby: awaiting LobbyCreated_t (callback pump should fire it)",
    );
    let raw_lobby_id = rx
        .await
        .map_err(|_| {
            sl.write_line("WARN", "create_lobby: oneshot dropped before steam responded");
            "callback dropped before steam responded".to_string()
        })
        .and_then(|r| {
            if let Err(e) = &r {
                sl.write_line("WARN", &format!("create_lobby: steam returned error: {e}"));
            }
            r
        })?;
    sl.write_line(
        "INFO",
        &format!("create_lobby: got rawLobbyId={raw_lobby_id}"),
    );

    let lobby = steamworks::LobbyId::from_raw(raw_lobby_id);
    sl.write_line("INFO", "create_lobby: stamping set_lobby_data …");
    // §14R-A: stamp the lobby metadata both /find-game and the Steam
    // Sockets handoff depend on. Best-effort — set_lobby_data returns
    // false if we don't own the lobby, which can't happen here.
    {
        let mm = b.client.matchmaking();
        let r1 = mm.set_lobby_data(lobby, "host_steam_id", &host_steam_id.to_string());
        sl.write_line("INFO", &format!("create_lobby: set host_steam_id → {r1}"));
        let r2 = mm.set_lobby_data(lobby, "host_persona", &host_persona_resolved);
        sl.write_line("INFO", &format!("create_lobby: set host_persona → {r2}"));
        let r3 = mm.set_lobby_data(lobby, "status", "waiting");
        sl.write_line("INFO", &format!("create_lobby: set status → {r3}"));
        if !map_id.is_empty() {
            let r4 = mm.set_lobby_data(lobby, "map_id", &map_id);
            sl.write_line("INFO", &format!("create_lobby: set map_id → {r4}"));
        }
        if !local_lobby_id.is_empty() {
            let r5 = mm.set_lobby_data(lobby, "local_lobby_id", &local_lobby_id);
            sl.write_line("INFO", &format!("create_lobby: set local_lobby_id → {r5}"));
        }
    }
    sl.write_line("INFO", "create_lobby: set_lobby_data block complete");

    sl.write_line("INFO", "create_lobby: looking up Go IPC writer …");
    let go_writer = ipc::current_go_writer();
    sl.write_line(
        "INFO",
        &format!("create_lobby: Go IPC writer present={}", go_writer.is_some()),
    );
    if let Some(writer) = go_writer {
        sl.write_line("INFO", "create_lobby: pushing lobby_hosted notification …");
        ipc::push_notification(
            &writer,
            "lobby_hosted",
            serde_json::json!({
                "lobbyId": raw_lobby_id.to_string(),
                "hostSteamId64": host_steam_id.to_string(),
                "localLobbyId": local_lobby_id,
            }),
        );
        sl.write_line("INFO", "create_lobby: lobby_hosted pushed");
    }

    sl.write_line("INFO", "create_lobby: returning Ok to JS");
    Ok(raw_lobby_id.to_string())
}

/// Joiner-side response shape: the SPA needs the local_lobby_id (read
/// from the host's lobby metadata) so it can navigate to /lobby/<id>
/// with the same id the host's local server uses.
#[cfg(feature = "steam")]
#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JoinLobbyResponse {
    pub lobby_id: String,
    pub host_steam_id_64: String,
    pub local_lobby_id: String,
    pub map_id: String,
}

#[cfg(feature = "steam")]
#[tauri::command]
async fn join_lobby(
    bridge: tauri::State<'_, SteamBridgeState>,
    lobby_id: String,
) -> Result<JoinLobbyResponse, String> {
    let b = bridge
        .0
        .as_ref()
        .ok_or_else(|| "steam_unavailable".to_string())?
        .clone();
    let raw = lobby_id
        .parse::<u64>()
        .map_err(|e| format!("bad lobby id: {e}"))?;
    let (tx, rx) = tokio::sync::oneshot::channel();
    {
        let mm = b.client.matchmaking();
        mm.join_lobby(steamworks::LobbyId::from_raw(raw), move |result| {
            let _ = tx.send(result.map(|id| id.raw()).map_err(|_| {
                "join failed (lobby full / not authorised / no longer exists)".to_string()
            }));
        });
    }
    let raw_lobby_id = rx
        .await
        .map_err(|_| "callback dropped before steam responded".to_string())
        .and_then(|r| r)?;

    let lobby = steamworks::LobbyId::from_raw(raw_lobby_id);
    let (host_steam_id_64, local_lobby_id, map_id) = {
        let mm = b.client.matchmaking();
        (
            mm.lobby_data(lobby, "host_steam_id").unwrap_or_default(),
            mm.lobby_data(lobby, "local_lobby_id").unwrap_or_default(),
            mm.lobby_data(lobby, "map_id").unwrap_or_default(),
        )
    };

    if host_steam_id_64.is_empty() {
        return Err(
            "lobby_missing_host_id: host_steam_id metadata absent or malformed".to_string(),
        );
    }

    // §14R-C: tell the Go server it should install the proxy-storing
    // peer handler and fire ConnectTo on the host's SteamID.
    if let Some(writer) = ipc::current_go_writer() {
        ipc::push_notification(
            &writer,
            "lobby_joined",
            serde_json::json!({
                "lobbyId": raw_lobby_id.to_string(),
                "hostSteamId64": host_steam_id_64,
                "localLobbyId": local_lobby_id,
            }),
        );
    }

    Ok(JoinLobbyResponse {
        lobby_id: raw_lobby_id.to_string(),
        host_steam_id_64,
        local_lobby_id,
        map_id,
    })
}

#[cfg(feature = "steam")]
#[tauri::command]
fn open_invite_overlay(
    bridge: tauri::State<'_, SteamBridgeState>,
    shell_log: tauri::State<'_, logs::ShellLogHandle>,
    lobby_id: String,
) -> Result<(), String> {
    let sl = shell_log.inner().clone();
    sl.write_line(
        "INFO",
        &format!("open_invite_overlay: entered lobbyId={lobby_id}"),
    );
    let b = bridge.0.as_ref().ok_or_else(|| {
        sl.write_line("WARN", "open_invite_overlay: bridge None — steam_unavailable");
        "steam_unavailable".to_string()
    })?;
    let raw = lobby_id.parse::<u64>().map_err(|e| {
        sl.write_line(
            "WARN",
            &format!("open_invite_overlay: bad lobby id {lobby_id}: {e}"),
        );
        format!("bad lobby id: {e}")
    })?;
    // ActivateGameOverlayInviteDialog returns void — Steam silently no-ops
    // if the overlay hasn't been injected into this process (the classic
    // Spacewar-appid-with-non-Spacewar-binary failure). We log that we
    // called it so the operator can verify the command reached Steam even
    // when the overlay doesn't visibly appear.
    b.client
        .friends()
        .activate_invite_dialog(steamworks::LobbyId::from_raw(raw));
    sl.write_line(
        "INFO",
        &format!(
            "open_invite_overlay: ActivateGameOverlayInviteDialog called (lobby={raw}); if no overlay appears, Steam overlay injection failed — see desktop/README.md 'Steam overlay injection'"
        ),
    );
    Ok(())
}

/// §14R-A list_steam_lobbies. Calls RequestLobbyList (FriendsOnly distance
/// filter — friends' lobbies only, no public listing) and reads the metadata
/// each lobby has stamped (host_persona, map_id, local_lobby_id, status,
/// host_steam_id) so the SPA's /find-game can render a useful list without
/// each entry needing a follow-up call. Lobbies missing required metadata
/// (host_steam_id) are dropped from the result.
///
/// Async; resolves after the LobbyMatchList_t callback fires.
#[cfg(feature = "steam")]
#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SteamLobbyListEntry {
    pub steam_lobby_id: String,
    pub host_steam_id: String,
    pub host_persona: String,
    pub map_id: String,
    pub local_lobby_id: String,
    pub status: String,
    pub player_count: u32,
    pub max_players: u32,
}

#[cfg(feature = "steam")]
#[tauri::command]
async fn list_steam_lobbies(
    bridge: tauri::State<'_, SteamBridgeState>,
    shell_log: tauri::State<'_, logs::ShellLogHandle>,
) -> Result<Vec<SteamLobbyListEntry>, String> {
    let sl = shell_log.inner().clone();
    let b = bridge
        .0
        .as_ref()
        .ok_or_else(|| {
            sl.write_line("WARN", "list_steam_lobbies: bridge None — steam_unavailable");
            "steam_unavailable".to_string()
        })?
        .clone();
    // Send the RequestLobbyList call and drop the Matchmaking handle
    // BEFORE the await — Matchmaking holds a raw `*mut ISteamMatchmaking`
    // pointer and isn't Send, so it can't be held across an await point.
    // Tauri command futures need Send. We re-acquire the handle after.
    let (tx, rx) = tokio::sync::oneshot::channel();
    {
        let mm = b.client.matchmaking();
        // Worldwide distance filter: the default is geography-scoped (close
        // regions only), which can hide friend lobbies in different regions
        // even for FriendsOnly visibility. Explicit Worldwide removes any
        // regional gating so a single Steam-friend check is the only thing
        // determining whether the friend sees the lobby.
        mm.set_request_lobby_list_distance_filter(
            steamworks::DistanceFilter::Worldwide,
        );
        mm.request_lobby_list(move |result| {
            let _ = tx.send(result.map_err(|e| format!("{e:?}")));
        });
    }
    let lobby_ids = rx
        .await
        .map_err(|_| {
            sl.write_line("WARN", "list_steam_lobbies: oneshot dropped");
            "callback dropped before steam responded".to_string()
        })
        .and_then(|r| {
            if let Err(e) = &r {
                sl.write_line(
                    "WARN",
                    &format!("list_steam_lobbies: RequestLobbyList error: {e}"),
                );
            }
            r
        })?;
    sl.write_line(
        "INFO",
        &format!(
            "list_steam_lobbies: RequestLobbyList returned {} lobbies",
            lobby_ids.len()
        ),
    );

    let mm = b.client.matchmaking();
    let mut out = Vec::with_capacity(lobby_ids.len());
    for id in lobby_ids {
        let host_steam_id = mm.lobby_data(id, "host_steam_id").unwrap_or_default();
        let host_persona = mm.lobby_data(id, "host_persona").unwrap_or_default();
        let map_id = mm.lobby_data(id, "map_id").unwrap_or_default();
        let local_lobby_id = mm.lobby_data(id, "local_lobby_id").unwrap_or_default();
        let status = mm.lobby_data(id, "status").unwrap_or_default();
        let player_count = mm.lobby_member_count(id) as u32;
        let max_players = mm.lobby_member_limit(id).unwrap_or(0) as u32;
        sl.write_line(
            "INFO",
            &format!(
                "list_steam_lobbies: lobby={} host_steam_id={:?} host_persona={:?} map_id={:?} status={:?} players={}/{}",
                id.raw(),
                host_steam_id,
                host_persona,
                map_id,
                status,
                player_count,
                max_players
            ),
        );
        if host_steam_id.is_empty() {
            sl.write_line(
                "INFO",
                &format!(
                    "list_steam_lobbies: skipping lobby={} (no host_steam_id metadata — not a Nomads lobby or stale entry)",
                    id.raw()
                ),
            );
            continue;
        }
        // §14R follow-up: hide lobbies that have already started. The host's
        // start_steam_game stamps status="started"; once that's set the
        // lobby is no longer joinable from /find-game and shouldn't clutter
        // the list. (Steam-side we ALSO call SetLobbyJoinable(false) so
        // Steam itself eventually drops it from RequestLobbyList, but
        // that's eventually-consistent — this filter is the immediate
        // user-visible behaviour.)
        if status == "started" {
            sl.write_line(
                "INFO",
                &format!(
                    "list_steam_lobbies: skipping lobby={} (status=started; already in a match)",
                    id.raw()
                ),
            );
            continue;
        }
        out.push(SteamLobbyListEntry {
            steam_lobby_id: id.raw().to_string(),
            host_steam_id,
            host_persona,
            map_id,
            local_lobby_id,
            status,
            player_count,
            max_players,
        });
    }
    sl.write_line(
        "INFO",
        &format!("list_steam_lobbies: returning {} entries to SPA", out.len()),
    );
    Ok(out)
}

/// §14R-A get_steam_lobby_data. Joiner-side /lobby polls this to read
/// metadata + member personas without server-side mirroring (deferred to
/// §14.5). The joiner can poll at ~1Hz; lobby metadata updates fire fast
/// enough through Steam's relay that this is non-load-bearing.
#[cfg(feature = "steam")]
#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SteamLobbyMember {
    pub steam_id_64: String,
    pub persona_name: String,
}

#[cfg(feature = "steam")]
#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SteamLobbyData {
    pub steam_lobby_id: String,
    pub host_steam_id: String,
    pub host_persona: String,
    pub map_id: String,
    pub local_lobby_id: String,
    pub status: String,
    pub match_id: String,
    pub max_players: u32,
    pub members: Vec<SteamLobbyMember>,
}

#[cfg(feature = "steam")]
#[tauri::command]
fn get_steam_lobby_data(
    bridge: tauri::State<'_, SteamBridgeState>,
    steam_lobby_id: String,
) -> Result<SteamLobbyData, String> {
    let b = bridge.0.as_ref().ok_or_else(|| "steam_unavailable".to_string())?;
    let raw = steam_lobby_id
        .parse::<u64>()
        .map_err(|e| format!("bad steam_lobby_id: {e}"))?;
    let lobby = steamworks::LobbyId::from_raw(raw);
    let mm = b.client.matchmaking();
    let friends = b.client.friends();
    let members: Vec<SteamLobbyMember> = mm
        .lobby_members(lobby)
        .into_iter()
        .map(|sid| {
            let name = friends.get_friend(sid).name();
            SteamLobbyMember {
                steam_id_64: sid.raw().to_string(),
                persona_name: name,
            }
        })
        .collect();
    Ok(SteamLobbyData {
        steam_lobby_id: steam_lobby_id.clone(),
        host_steam_id: mm.lobby_data(lobby, "host_steam_id").unwrap_or_default(),
        host_persona: mm.lobby_data(lobby, "host_persona").unwrap_or_default(),
        map_id: mm.lobby_data(lobby, "map_id").unwrap_or_default(),
        local_lobby_id: mm.lobby_data(lobby, "local_lobby_id").unwrap_or_default(),
        status: mm.lobby_data(lobby, "status").unwrap_or_default(),
        match_id: mm.lobby_data(lobby, "match_id").unwrap_or_default(),
        max_players: mm.lobby_member_limit(lobby).unwrap_or(0) as u32,
        members,
    })
}

/// §14.3 start-game signal. Host SPA calls this after the host's own
/// `welcome` message returns a real `matchId`. We stamp the matchId into
/// the Steam lobby metadata; joiners observe LobbyDataUpdate_t and emit
/// `steam_lobby_started` to their SPA, which navigates them into /match.
///
/// Idempotent — calling twice just rewrites the same metadata. The host
/// SPA is expected to call this once per session.
#[cfg(feature = "steam")]
#[tauri::command]
fn start_steam_game(
    bridge: tauri::State<'_, SteamBridgeState>,
    lobby_id: String,
    match_id: String,
) -> Result<(), String> {
    let b = bridge.0.as_ref().ok_or_else(|| "steam_unavailable".to_string())?;
    if match_id.is_empty() {
        return Err("empty match_id".to_string());
    }
    let raw = lobby_id
        .parse::<u64>()
        .map_err(|e| format!("bad lobby id: {e}"))?;
    let mm = b.client.matchmaking();
    let lobby = steamworks::LobbyId::from_raw(raw);
    if !mm.set_lobby_data(lobby, "match_id", &match_id) {
        return Err("set_lobby_data(match_id) failed".to_string());
    }
    if !mm.set_lobby_data(lobby, "status", "started") {
        return Err("set_lobby_data(status) failed".to_string());
    }
    // Tell Steam to stop advertising this lobby in RequestLobbyList
    // results. Combined with the SPA-side status="started" filter in
    // list_steam_lobbies, this ensures started matches don't appear in
    // /find-game on any peer (immediately on our side, eventually-
    // consistent on Steam's side).
    mm.set_lobby_joinable(lobby, false);
    Ok(())
}

/// §14R follow-up: leave the Steam lobby. Called when the host (or
/// joiner) clicks Back from /lobby before the match starts. Without
/// this, the lobby stays alive on Steam as long as the binary is
/// running, polluting /find-game lists. The host leaving destroys the
/// lobby Steam-side (Matchmaking auto-removes lobbies when the owner
/// disconnects); a joiner leaving just removes them from the member
/// list.
#[cfg(feature = "steam")]
#[tauri::command]
fn leave_steam_lobby(
    bridge: tauri::State<'_, SteamBridgeState>,
    lobby_id: String,
) -> Result<(), String> {
    let b = bridge
        .0
        .as_ref()
        .ok_or_else(|| "steam_unavailable".to_string())?;
    let raw = lobby_id
        .parse::<u64>()
        .map_err(|e| format!("bad lobby id: {e}"))?;
    b.client
        .matchmaking()
        .leave_lobby(steamworks::LobbyId::from_raw(raw));
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
async fn create_lobby(
    _max_players: Option<u32>,
    _map_id: Option<String>,
    _local_lobby_id: Option<String>,
    _host_persona: Option<String>,
) -> Result<String, String> {
    Err("steam_unavailable".to_string())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
async fn join_lobby(_lobby_id: String) -> Result<serde_json::Value, String> {
    Err("steam_unavailable".to_string())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
fn open_invite_overlay(_lobby_id: String) -> Result<(), String> {
    Err("steam_unavailable".to_string())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
fn start_steam_game(_lobby_id: String, _match_id: String) -> Result<(), String> {
    Err("steam_unavailable".to_string())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
fn leave_steam_lobby(_lobby_id: String) -> Result<(), String> {
    Ok(())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
async fn list_steam_lobbies() -> Result<Vec<serde_json::Value>, String> {
    Ok(Vec::new())
}

#[cfg(not(feature = "steam"))]
#[tauri::command]
fn get_steam_lobby_data(_steam_lobby_id: String) -> Result<serde_json::Value, String> {
    Err("steam_unavailable".to_string())
}

/// Parses `--steam-net-selftest` from `std::env::args()`. Accepts both
/// `--steam-net-selftest <value>` and `--steam-net-selftest=<value>`.
/// Returns the raw value string (`"host"` or `"connect:<steamid>"`); the Go
/// side validates the contents.
fn parse_selftest_mode_from_args() -> Option<String> {
    parse_selftest_mode(std::env::args().skip(1).collect::<Vec<_>>())
}

fn parse_selftest_mode<I, S>(args: I) -> Option<String>
where
    I: IntoIterator<Item = S>,
    S: AsRef<str>,
{
    let mut iter = args.into_iter();
    while let Some(arg) = iter.next() {
        let s = arg.as_ref();
        if let Some(rest) = s.strip_prefix("--steam-net-selftest=") {
            if !rest.is_empty() {
                return Some(rest.to_string());
            }
        } else if s == "--steam-net-selftest" {
            if let Some(next) = iter.next() {
                let v = next.as_ref().to_string();
                if !v.is_empty() {
                    return Some(v);
                }
            }
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn selftest_mode_space_separated() {
        let v = parse_selftest_mode(vec!["--steam-net-selftest", "host"]);
        assert_eq!(v.as_deref(), Some("host"));
    }

    #[test]
    fn selftest_mode_equals_form() {
        let v = parse_selftest_mode(vec!["--steam-net-selftest=connect:76561197960287930"]);
        assert_eq!(v.as_deref(), Some("connect:76561197960287930"));
    }

    #[test]
    fn selftest_mode_absent_returns_none() {
        let v = parse_selftest_mode(vec!["--other-flag", "value"]);
        assert!(v.is_none());
    }

    #[test]
    fn selftest_mode_empty_value_returns_none() {
        // `--steam-net-selftest=` and `--steam-net-selftest ` (no following
        // token) are both treated as "not set" rather than the empty string.
        assert!(parse_selftest_mode(vec!["--steam-net-selftest="]).is_none());
        assert!(parse_selftest_mode(vec!["--steam-net-selftest"]).is_none());
    }
}
