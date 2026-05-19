// Steamworks integration. All Steam SDK calls live here (and nowhere else
// in the codebase per design D3 / AI_RULES). Feature-gated on `steam` so
// default builds — and CI — don't require the Steamworks SDK installed.
//
// Lifecycle:
//   1. main calls `restart_if_necessary(appid)` BEFORE any other Steam call
//      (§8.1). If true, the process exits immediately and Steam relaunches
//      us under its management.
//   2. main calls `Bridge::init(appid)` which runs SteamAPI_Init and spawns
//      a background callback-pump thread (§8.2).
//   3. Tauri commands route through the returned Bridge to read player info,
//      open the invite overlay, report achievements, and (later) drive
//      Matchmaking lobbies.
//   4. On window close the Bridge is dropped → SteamAPI_Shutdown via the
//      steamworks crate's Drop impl, after the pump thread is joined.

#![cfg(feature = "steam")]

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use log::info;
use serde::Serialize;
use steamworks::{AppId, Client};

/// LocalPlayer is the serializable view returned to the SPA via
/// `getSteamPlayer()`. Mirrors the Go-side type in server/internal/steam/.
/// camelCase rename so the JSON shape matches the SPA's LocalSteamPlayer
/// interface (`steamId64`, `personaName`) without manual mapping.
#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LocalPlayer {
    pub steam_id_64: u64,
    pub persona_name: String,
}

/// The live Steamworks client + an opaque handle to the callback pump.
/// Held in Tauri's managed state for the lifetime of the application.
pub struct Bridge {
    pub client: Client,
    _pump: PumpHandle,
}

impl Bridge {
    /// Initialise Steamworks. Returns Err when SteamAPI_Init fails (Steam
    /// not running, appid missing/wrong, SDK linkage broken). Caller decides
    /// whether to fall back to no-Steam behaviour or exit.
    pub fn init(appid: u32) -> Result<Self, String> {
        // Pass the appid explicitly via init_app so we're robust whether
        // `steam_appid.txt` is present (dev) or absent (release launched
        // via Steam, which provides the id through the environment).
        let client = Client::init_app(AppId(appid))
            .map_err(|e| format!("SteamAPI_Init: {e}"))?;

        let persona = client.friends().name();
        let id = client.user().steam_id().raw();
        info!("steam: initialised as {persona} (steamid={id})");

        let pump = spawn_callback_pump(client.clone());
        Ok(Bridge { client, _pump: pump })
    }

    /// Snapshot of the current local player. Cheap, no IPC; reads from the
    /// in-memory Steamworks client state.
    pub fn local_player(&self) -> LocalPlayer {
        LocalPlayer {
            steam_id_64: self.client.user().steam_id().raw(),
            persona_name: self.client.friends().name(),
        }
    }
}

/// Calls SteamAPI_RestartAppIfNecessary. Returns true iff Steam needs to
/// relaunch us with the correct appid attribution. Caller must exit
/// immediately on true — NO Steam SDK calls, no window, no Go child spawn.
pub fn restart_if_necessary(appid: u32) -> bool {
    steamworks::restart_app_if_necessary(AppId(appid))
}

/// Background thread that pumps Steamworks callbacks at the standard
/// ~10 Hz cadence. Owned by Bridge so dropping the bridge stops the pump
/// (atomic flag flips → next loop iteration breaks → thread joined).
struct PumpHandle {
    quit: Arc<AtomicBool>,
    join: Option<thread::JoinHandle<()>>,
}

impl Drop for PumpHandle {
    fn drop(&mut self) {
        self.quit.store(true, Ordering::SeqCst);
        if let Some(j) = self.join.take() {
            let _ = j.join();
        }
    }
}

fn spawn_callback_pump(client: Client) -> PumpHandle {
    let quit = Arc::new(AtomicBool::new(false));
    let quit_clone = quit.clone();
    let join = thread::spawn(move || {
        while !quit_clone.load(Ordering::SeqCst) {
            client.run_callbacks();
            thread::sleep(Duration::from_millis(100));
        }
    });
    PumpHandle { quit, join: Some(join) }
}
