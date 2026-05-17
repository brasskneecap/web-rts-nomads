// Nomads Tauri shell entrypoint. The shell is responsible for:
//   * spawning and supervising the Go server child process (§6)
//   * resolving the OS user-data dir, settings, and logs dirs (§7)
//   * (Phase 2) Steamworks init, IPC channel, and lobby/achievement bridging
//
// Rule (AI_RULES.md): NO game logic in this crate. It's window + sidecar
// supervisor + (Phase 2) Steam wrapper. Anything game-related belongs in the
// Go server.

mod logs;
mod migration;
mod settings;
mod supervisor;
mod userdata;

use log::{info, warn};

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    env_logger::Builder::from_env(env_logger::Env::default().default_filter_or("info")).init();

    info!("nomads-desktop starting");

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

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .setup(move |app| {
            // §6: spawn the Go child and wait for NOMADS_READY before the
            // window is shown. The supervisor blocks setup for up to 10 s; on
            // success it returns the URL to load and a handle for shutdown.
            let (ready, handle) = match supervisor::spawn_and_wait_ready(
                &paths,
                Some(log_session.server_log_path.clone()),
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
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            get_settings,
            set_settings,
            open_logs_directory,
            get_logs_directory
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
