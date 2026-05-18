// desktopBridge — the SINGLE FILE in the SPA permitted to import from
// `@tauri-apps/api`. Every other file in the SPA that needs shell-side
// functionality imports `desktopBridge` and uses the typed methods below.
//
// At runtime, the bridge probes `window.__TAURI__`:
//   - Present (packaged Tauri build): real IPC commands are issued.
//   - Absent (browser dev loop, no Tauri):
//       * stub methods return safe defaults (null player, no-op log buffer);
//       * methods backed by HTTP equivalents call into the existing fetch
//         layer where applicable (e.g., direct-connect toggle).
//
// This contract is what lets the SPA run unchanged in `npm run dev` and in
// the packaged build — pages don't have to branch on "am I inside Tauri?"
// at every call site; they just call `desktopBridge.something()`.

import type {} from '@tauri-apps/api'

// We intentionally do NOT import `invoke` / `listen` at the top level —
// that would pull `@tauri-apps/api` into the dev bundle even when running in
// a plain browser. Lazy-resolved via dynamic import in `tauriApi()`.

export interface LocalSteamPlayer {
  steamId64: string
  personaName: string
}

export interface WindowStatePayload {
  width?: number
  height?: number
  x?: number
  y?: number
  maximized?: boolean
}

export interface SettingsSnapshot {
  playerId?: string
  window?: WindowStatePayload
  // forward-compat: unknown shell-injected keys survive across reads
  [key: string]: unknown
}

export interface LogEntry {
  level: 'debug' | 'info' | 'warn' | 'error'
  message: string
  context?: Record<string, unknown>
}

/** True when the SPA is running inside the Tauri shell. Checks every
 *  global the v1, v2.0, and v2.x runtimes have used: `window.isTauri`
 *  (current v2 helper-blessed flag), `window.__TAURI_INTERNALS__` (early
 *  v2 internal), and `window.__TAURI__` (v1 legacy). At least one of these
 *  is set by the Tauri runtime; none should be present in plain browser dev. */
export function isInTauri(): boolean {
  if (typeof window === 'undefined') return false
  const w = window as any
  return w.isTauri === true || w.__TAURI_INTERNALS__ !== undefined || w.__TAURI__ !== undefined
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/** Returns the current Steam-signed-in player, or null when Steam is
 * unavailable / not initialised / running outside Tauri. */
export async function getSteamPlayer(): Promise<LocalSteamPlayer | null> {
  if (!isInTauri()) return null
  try {
    const result = await invoke<LocalSteamPlayer | null>('get_steam_player')
    return result ?? null
  } catch {
    // Steam unavailable; documented degradation path.
    return null
  }
}

/** Opens the Steam friend-invite overlay for the given lobby. No-op in browser dev. */
export async function inviteFriend(lobbyId: string): Promise<void> {
  if (!isInTauri()) return
  await invoke('open_invite_overlay', { lobbyId })
}

/** Result of a successful lobby create/join. Returned to the SPA UI so it
 *  can display the lobby id (for copy/share) and the player count. */
export interface LobbyHandle {
  lobbyId: string
}

/** Reports an achievement to Steam. Fire-and-forget (design D19) — errors are swallowed. */
export async function reportAchievement(id: string): Promise<void> {
  if (!isInTauri()) return
  try {
    await invoke('report_achievement', { id })
  } catch {
    /* fire-and-forget */
  }
}

/** Creates a Steam Matchmaking lobby (FriendsOnly). Returns the lobby's
 *  SteamID64-as-string, or null when running outside Tauri. Throws on
 *  Steam-side errors (steam_unavailable, callback dropped, etc.). */
export async function openLobby(opts: { maxPlayers?: number } = {}): Promise<LobbyHandle | null> {
  if (!isInTauri()) return null
  const lobbyId = await invoke<string>('create_lobby', { maxPlayers: opts.maxPlayers ?? 4 })
  return { lobbyId }
}

/** Joins an existing Steam lobby by SteamID64 string. Returns the joined
 *  lobby's id (echoed back so the SPA can confirm). */
export async function joinLobby(lobbyId: string): Promise<LobbyHandle | null> {
  if (!isInTauri()) return null
  const joined = await invoke<string>('join_lobby', { lobbyId })
  return { lobbyId: joined }
}

/** Signals the shell that the SPA is mounted and ready to receive deferred
 * events (e.g., a `+connect_lobby` argv parsed at startup before the SPA
 * loaded). The shell holds those events in memory until this is called. */
export async function ready(): Promise<void> {
  if (!isInTauri()) return
  await invoke('desktop_bridge_ready')
}

/** Loads the persisted settings (or returns defaults in browser dev). */
export async function getSettings(): Promise<SettingsSnapshot> {
  if (!isInTauri()) {
    // Browser-dev fallback: synthesise from localStorage so SP profile
    // identity still survives reloads in the dev loop.
    const playerId = browserLocalStoragePlayerId()
    return playerId ? { playerId } : {}
  }
  return invoke<SettingsSnapshot>('get_settings')
}

/** Merges `partial` into the persisted settings. Returns the new snapshot. */
export async function setSettings(partial: Partial<SettingsSnapshot>): Promise<SettingsSnapshot> {
  if (!isInTauri()) {
    if (typeof partial.playerId === 'string') {
      try {
        window.localStorage.setItem('nomads.playerId', partial.playerId)
      } catch {
        /* ignore quota errors in the dev loop */
      }
    }
    return getSettings()
  }
  return invoke<SettingsSnapshot>('set_settings', { partial })
}

/** Returns the absolute path to the logs directory (for display in the SPA's
 * support / about screen). Returns null in browser dev. */
export async function getLogsDirectory(): Promise<string | null> {
  if (!isInTauri()) return null
  return invoke<string>('get_logs_directory')
}

/** Opens the logs directory in the OS file manager. No-op in browser dev. */
export async function openLogsDirectory(): Promise<void> {
  if (!isInTauri()) return
  await invoke('open_logs_directory')
}

/** Appends SPA log entries to the per-launch <ts>-spa.log file (task 22.3).
 * In browser dev, console.log them so they're visible to devs. */
export async function appendLog(entries: LogEntry[]): Promise<void> {
  if (!isInTauri()) {
    for (const e of entries) {
      // eslint-disable-next-line no-console
      console[e.level](`[spa] ${e.message}`, e.context ?? '')
    }
    return
  }
  await invoke('append_log', { entries })
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

let cachedInvoke: ((cmd: string, args?: any) => Promise<any>) | undefined
async function invoke<T>(cmd: string, args?: Record<string, unknown>): Promise<T> {
  if (!cachedInvoke) {
    // Dynamic import keeps `@tauri-apps/api` out of the browser-dev bundle.
    const mod = await import('@tauri-apps/api/core')
    cachedInvoke = mod.invoke
  }
  return cachedInvoke(cmd, args) as Promise<T>
}

function browserLocalStoragePlayerId(): string | undefined {
  try {
    return window.localStorage.getItem('nomads.playerId') ?? undefined
  } catch {
    return undefined
  }
}

/**
 * One-shot migration helper: when running inside Tauri, if `settings.json`
 * lacks a player-id but `localStorage` has one (carry-over from a previous
 * browser-dev session against the same install), copy the localStorage value
 * into settings.json. Idempotent: subsequent calls with both present are no-ops.
 *
 * Per §7.10 + design D20 rationale: the packaged build's `port=0` policy makes
 * `localStorage` non-durable across launches, so the canonical store must be
 * settings.json from Phase 1 onward.
 */
export async function migratePlayerIdFromLocalStorageIfNeeded(): Promise<void> {
  if (!isInTauri()) return
  const current = await getSettings()
  if (current.playerId) return
  const fromLS = browserLocalStoragePlayerId()
  if (!fromLS) return
  await setSettings({ playerId: fromLS })
}
