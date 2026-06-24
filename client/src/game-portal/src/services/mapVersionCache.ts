// mapVersionCache — localStorage-keyed registry of "map versions the local
// client has received from a host". Keyed by `${mapId}:${contentHash}`.
//
// Purpose (task 3 & 4):
//   - Task 3: lobby/find-game preview reconciliation. When the host's mapHash
//     (from SteamLobbyData / SteamLobbyListEntry) does NOT match a version the
//     joiner has locally, show a "Host's custom map — loads at start" placeholder
//     instead of a potentially-wrong preview.
//   - Task 4: WelcomeMessage persistence. When a welcome arrives with a
//     non-empty contentHash not yet cached here, write an entry and best-effort
//     save the map via POST /maps so the joiner accumulates the host's version.
//
// Authority rule: this cache is the source of truth for the preview-match check.
// POST /maps is best-effort; even if a re-save recomputes a slightly different
// hash, the cache entry still guarantees convergence for future lobbies.
//
// Key scheme: `nomads.mapVersion:${mapId}:${contentHash}`
// Value: JSON-serialised MapVersionCacheEntry

const LS_PREFIX = 'nomads.mapVersion:'

export interface MapVersionCacheEntry {
  id: string
  name: string
  contentHash: string
  version: string
  /** Grid dimensions — enough for the lobby preview (MinimapPreview uses
   *  gridCols/gridRows/spawnPointCount from MapCatalogEntry). */
  gridCols: number
  gridRows: number
  /** Spawn-point count. May be 0 when unknown (e.g. on an older welcome that
   *  doesn't expose this). The lobby preview component null-coalesces. */
  spawnPointCount: number
}

/** Build the localStorage key for a given (mapId, contentHash) pair. */
export function mapVersionKey(mapId: string, contentHash: string): string {
  return `${LS_PREFIX}${mapId}:${contentHash}`
}

/** Returns true when the local client has a cached entry for this exact
 *  (mapId, contentHash) pair, indicating the joiner has the host's version. */
export function hasMapVersion(mapId: string, contentHash: string): boolean {
  if (!mapId || !contentHash) return false
  try {
    return localStorage.getItem(mapVersionKey(mapId, contentHash)) !== null
  } catch {
    // localStorage blocked (e.g. private browsing quota); degrade to false
    // so the preview shows the safe placeholder rather than a wrong image.
    return false
  }
}

/** Writes a cache entry if the (mapId, contentHash) pair is not already
 *  present. Idempotent — repeated calls for the same pair are no-ops. */
export function putMapVersion(entry: MapVersionCacheEntry): void {
  if (!entry.id || !entry.contentHash) return
  const key = mapVersionKey(entry.id, entry.contentHash)
  try {
    if (localStorage.getItem(key) !== null) return // already cached
    localStorage.setItem(key, JSON.stringify(entry))
  } catch {
    /* localStorage quota or access error — non-fatal, silently skip */
  }
}

/** Retrieves a cached entry, or null when absent / unparseable. */
export function getMapVersion(
  mapId: string,
  contentHash: string,
): MapVersionCacheEntry | null {
  if (!mapId || !contentHash) return null
  try {
    const raw = localStorage.getItem(mapVersionKey(mapId, contentHash))
    if (!raw) return null
    return JSON.parse(raw) as MapVersionCacheEntry
  } catch {
    return null
  }
}
