import { computed, ref } from 'vue'
import { getSteamPlayer } from '@/services/desktopBridge'
import { getOrCreatePlayerId } from '@/services/profileApi'

// Single source of truth for the local player's identity. Shared with
// NetworkClient (WS), profileApi (HTTP), and the lobby system —
// getOrCreatePlayerId reads localStorage key 'webrts.profile.id',
// minting a UUID on first call. This value NEVER changes at runtime:
// all server-side identity matching (lobby host, lobby roster, WS join,
// match-status preflight) joins on the UUID. Steam persona names are
// display-only and live on personaName below.
const playerId = ref(getOrCreatePlayerId())

// personaName starts empty (browser dev loop) and is populated when the
// Tauri shell reports a Steam persona — used purely for friendly display
// labels (HUD player name, lobby roster). Never sent to the Go server as
// an identity field.
const personaName = ref<string>('')

void (async () => {
  try {
    const steam = await getSteamPlayer()
    if (steam && steam.personaName) {
      personaName.value = steam.personaName
    }
  } catch {
    // Steam unavailable; persona stays empty and displayName falls back
    // to the truncated-UUID label.
  }
})()

// formatDisplayName returns a friendly label for a player ID:
//   - Steam persona names and other non-UUID strings pass through.
//   - UUIDs (36-char `[0-9a-f-]`) collapse to `Player <first 6>` so the
//     UI never renders the raw 36-char identity. Match the regex used
//     server-side in extractPlayerID.
export function formatDisplayName(id: string): string {
  if (!id) return ''
  if (/^[0-9a-f-]{36}$/.test(id)) return `Player ${id.slice(0, 6)}`
  return id
}

export function usePlayer() {
  // Local display name: Steam persona when available, else truncated UUID.
  // Remote players in lobby rosters and battle trackers still resolve via
  // formatDisplayName(theirID) since we don't know remote personas.
  const displayName = computed(() =>
    personaName.value || formatDisplayName(playerId.value),
  )
  return { playerId, displayName }
}
