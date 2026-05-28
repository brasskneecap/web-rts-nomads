import { computed, ref } from 'vue'
import { getSteamPlayer } from '@/services/desktopBridge'
import { getOrCreatePlayerId } from '@/services/profileApi'

// Single source of truth for the local player's identity. Shared with
// NetworkClient (WS) and profileApi (HTTP) — getOrCreatePlayerId reads
// localStorage key 'webrts.profile.id', minting a UUID on first call.
// The Steam-persona override below replaces this value when running in
// the Tauri shell so "Acegamer" shows in lobbies instead of the UUID.
const playerId = ref(getOrCreatePlayerId())

// §14R: when Steam is available, use the persona name as the playerId so
// "Acegamer" shows in the lobby instead of "player-m4ezzo". This is a
// minimal-impact resolution of design D20 / §7.8 (which would have us
// also key profile-id storage by steamID — that's still deferred). The
// persona-as-id approach trades identity stability (your Steam rename
// changes your in-game name too) for a recognisable display name; fine
// for §14R-scope testing.
//
// Fired at module load. Synchronous app code that reads playerId in the
// same tick before this resolves sees the localStorage value; the ref
// updates reactively when the persona arrives so live UI catches up.
void (async () => {
  try {
    const steam = await getSteamPlayer()
    if (steam && steam.personaName) {
      if (playerId.value !== steam.personaName) {
        playerId.value = steam.personaName
      }
    }
  } catch {
    // Steam unavailable; keep the UUID.
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
  const displayName = computed(() => formatDisplayName(playerId.value))
  return { playerId, displayName }
}
