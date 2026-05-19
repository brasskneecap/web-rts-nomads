import { computed, ref } from 'vue'
import { getSteamPlayer } from '@/services/desktopBridge'

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'

function generateRandom(): string {
  return `player-${Math.random().toString(36).slice(2, 8)}`
}

function persist(id: string): void {
  try {
    localStorage.setItem(PLAYER_ID_STORAGE_KEY, id)
  } catch {
    /* localStorage may be sandboxed; non-fatal */
  }
}

function loadInitial(): string {
  return localStorage.getItem(PLAYER_ID_STORAGE_KEY) || generateRandom()
}

// Initialise synchronously from localStorage (or a fresh random) so the
// rest of the app can render without waiting for Steam. The Steam-persona
// upgrade below is a best-effort async refresh that fires on app boot.
const playerId = ref(loadInitial())
persist(playerId.value)

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
        persist(steam.personaName)
      }
    }
  } catch {
    // Steam unavailable; keep the localStorage value.
  }
})()

export function usePlayer() {
  const displayName = computed(() => playerId.value)
  return { playerId, displayName }
}
