<template>
  <div class="lobby">
    <div class="lobby__layout">
      <header class="lobby__header">
        <UiButton size="sm" @click="leaveAndGoBack">Back</UiButton>
        <div class="lobby__header-info">
          <h1 class="lobby__title">{{ lobby?.mapName ?? 'Lobby' }}</h1>
          <span class="lobby__slots">{{ lobby?.players.length ?? 0 }} / {{ lobby?.maxPlayers ?? 4 }} Players</span>
        </div>
      </header>

      <div v-if="lobby" class="lobby__body">
        <UiPanel class="lobby__players-panel" :padding="16">
          <div class="lobby__section-label">Players</div>
          <LobbyPlayerList
            :players="lobby.players"
            :host-player-id="lobby.hostPlayerId"
            :max-players="lobby.maxPlayers"
          />
        </UiPanel>
        <div v-if="!isHost" class="lobby__waiting">
          Waiting for the host to start the game…
        </div>
        <div v-if="startError" class="lobby__error">{{ startError }}</div>
        <div v-if="inviteError" class="lobby__error">{{ inviteError }}</div>
      </div>

      <div v-else class="lobby__not-found">
        Lobby not found.
      </div>

      <footer class="lobby__footer">
        <span
          v-if="isHost && steamLobbyPending && !steamLobbyId"
          class="lobby__steam-pending"
        >
          Setting up Steam invite…
        </span>
        <UiButton
          v-if="isHost && steamLobbyId"
          size="md"
          :disabled="inviteBusy"
          @click="onInvite"
        >
          {{ inviteBusy ? 'Opening overlay…' : 'Invite Friend' }}
        </UiButton>
        <UiButton
          v-if="isHost"
          size="lg"
          :disabled="!lobby || isStarting"
          @click="startGame"
        >
          {{ isStarting ? 'Starting…' : 'Start Game' }}
        </UiButton>
        <UiButton size="md" @click="leaveAndGoBack">Leave</UiButton>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useLobbies } from '@/composables/useLobbies'
import { usePlayer } from '@/composables/usePlayer'
import { useMapSelection } from '@/composables/useMapSelection'
import {
  getSteamLobbyData,
  inviteFriend,
  leaveSteamLobby,
  startSteamGame,
} from '@/services/desktopBridge'
import {
  STEAM_LOBBY_ID_KEY,
  STEAM_PROXY_FLAG_KEY,
} from '@/game/network/NetworkClient'
import {
  clearSteamLobbyPairing,
  steamLobbyPairing,
} from '@/state/steamLobbyState'
import type { Lobby } from '@/game/network/protocol'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import LobbyPlayerList from '@/components/menu/LobbyPlayerList.vue'

const router = useRouter()
const route = useRoute()
const { fetchLobby, leaveLobby, startLobby } = useLobbies()
const { playerId } = usePlayer()
const { setSelectedMapId } = useMapSelection()

const lobbyId = computed(() => route.params.id as string)
const lobby = ref<Lobby | null>(null)
const isHost = computed(() => lobby.value?.hostPlayerId === playerId.value)
const isStarting = ref(false)
const startError = ref('')

// §14R-D + optimistic-nav fix: the Steam lobby id is reactive so the
// Invite Friend button appears live when CreateGame's background
// openLobby resolves (1–2s after we navigated here). Sources, in order:
//   1. The shared reactive `steamLobbyPairing` (live updates from
//      CreateGame's background promise). Only matched against the
//      current route's localLobbyId so a stale pairing from a previous
//      lobby doesn't bleed through.
//   2. sessionStorage (cold mount / reload / joiner path, where
//      FindGame stamped it before navigating).
// While CreateGame's promise is still pending, sessionStorage may also
// be empty — that's the "connecting to Steam…" hint case.
const sessionStorageSteamLobbyId = ref<string | null>(null)
const steamLobbyId = computed<string | null>(() => {
  const pairing = steamLobbyPairing.value
  console.log('[Lobby] steamLobbyId recompute', {
    pairing,
    routeLobbyId: lobbyId.value,
    sessionStorageId: sessionStorageSteamLobbyId.value,
  })
  if (pairing && pairing.localLobbyId === lobbyId.value && !pairing.pending) {
    return pairing.steamLobbyId
  }
  return sessionStorageSteamLobbyId.value
})
const steamLobbyPending = computed<boolean>(() => {
  const pairing = steamLobbyPairing.value
  return (
    pairing !== null &&
    pairing.localLobbyId === lobbyId.value &&
    pairing.pending
  )
})
const inviteBusy = ref(false)
const inviteError = ref('')

let pollInterval: ReturnType<typeof setInterval> | null = null
// Tracks whether we've ever observed a local lobby for this route. When
// true, a transient null fetch (network hiccup) won't trigger the
// "lobby gone, redirect" fallback — that requires a sustained miss with
// no Steam-side source either.
let sawLocalLobby = false
// Number of consecutive empty poll results we tolerate before giving up
// on a cold mount. The host's flow is /create-game → router.push to
// /lobby/<id>; the local lobby is created synchronously before the
// nav, but in dev we've observed the very first GET /lobbies/<id>
// occasionally race with the in-memory map insert. Allow ~10s of
// retries before redirecting away.
let coldPollMisses = 0
const COLD_POLL_TOLERANCE = 10

function stopPoller() {
  if (pollInterval !== null) {
    clearInterval(pollInterval)
    pollInterval = null
  }
}

/** Synthesize a Lobby-shaped object from Steam metadata so the existing
 *  player-list + status UI works for the joiner without server-side
 *  mirroring (deferred to §14.5). hostPlayerId is the host's persona;
 *  it intentionally won't match the local playerId, so isHost stays
 *  false on the joiner side — the Start button + Invite button remain
 *  hidden on the joiner. */
function synthesizeLobbyFromSteam(_steamLobbyId: string, data: Awaited<ReturnType<typeof getSteamLobbyData>>): Lobby | null {
  if (!data) return null
  return {
    id: lobbyId.value,
    mapId: data.mapId,
    mapName: data.mapId || 'Unknown map',
    hostPlayerId: data.hostPersona || data.hostSteamId,
    players: data.members.map((m) => m.personaName),
    maxPlayers: data.maxPlayers > 0 ? data.maxPlayers : 4,
    createdAt: 0,
    status: data.status === 'started' ? 'started' : 'open',
    matchId: data.matchId || undefined,
  }
}

async function poll() {
  // Local lobby is authoritative for the host (their own server owns it).
  // Joiner's local server doesn't have an entry; fetchLobby returns null.
  const localUpdated = await fetchLobby(lobbyId.value).catch((e) => {
    console.warn('[Lobby] fetchLobby threw:', e)
    return null
  })

  let steamData: Awaited<ReturnType<typeof getSteamLobbyData>> = null
  if (steamLobbyId.value) {
    steamData = await getSteamLobbyData(steamLobbyId.value).catch((e) => {
      console.warn('[Lobby] getSteamLobbyData threw:', e)
      return null
    })
  }

  console.log('[Lobby] poll', {
    lobbyId: lobbyId.value,
    localFound: !!localUpdated,
    steamLobbyId: steamLobbyId.value,
    steamFound: !!steamData,
    sawLocalLobby,
    coldPollMisses,
  })

  // Pick the source for the visible lobby snapshot. Local wins when
  // present for status/match_id/etc., but the player list is overridden
  // by Steam's member list when a Steam pairing exists — the host's
  // local LobbyManager only knows about players added via HTTP /join
  // (always just themselves in Steam-MP), while Steam knows the real
  // membership including everyone who joined via SteamMatchmaking.
  if (localUpdated) {
    sawLocalLobby = true
    coldPollMisses = 0
    if (steamData && steamData.members.length > 0) {
      const steamPlayers = steamData.members.map((m) => m.personaName)
      lobby.value = {
        ...localUpdated,
        players: steamPlayers,
        // Bump maxPlayers to whatever Steam thinks if it's higher, so the
        // "N / M Players" cell doesn't read "3 / 1" if local was stale.
        maxPlayers: Math.max(localUpdated.maxPlayers, steamData.maxPlayers),
      }
    } else {
      lobby.value = localUpdated
    }
  } else if (steamData) {
    coldPollMisses = 0
    lobby.value = synthesizeLobbyFromSteam(lobbyId.value, steamData)
  } else if (sawLocalLobby) {
    // We had a local lobby and it's now gone. Treat as ended.
    stopPoller()
    void router.push('/find-game')
    return
  } else {
    // Cold mount with neither local nor Steam data yet. The host's
    // optimistic-nav path means we routed here while Steam was still
    // creating the lobby in the background — don't redirect away
    // until we've waited COLD_POLL_TOLERANCE polls (~10s). Hosts and
    // joiners both benefit: a transient HTTP 502 or a slow Steam
    // callback should never bounce the user back to /find-game.
    coldPollMisses += 1
    if (coldPollMisses >= COLD_POLL_TOLERANCE) {
      console.warn(
        `Lobby: ${COLD_POLL_TOLERANCE} consecutive empty polls for ${lobbyId.value}; ` +
          `redirecting to /find-game.`,
      )
      stopPoller()
      void router.push('/find-game')
    }
    // else: keep polling silently.
    return
  }

  // Status transitions. Either source can flip to started + matchId.
  const startedFromLocal =
    localUpdated && localUpdated.status === 'started' && localUpdated.matchId
  const startedFromSteam =
    steamData && steamData.status === 'started' && steamData.matchId
  if (startedFromLocal) {
    stopPoller()
    setSelectedMapId(localUpdated!.mapId, localUpdated!.mapName)
    void router.push(`/match/${localUpdated!.matchId}`)
    return
  }
  if (startedFromSteam) {
    stopPoller()
    setSelectedMapId(steamData!.mapId, steamData!.mapId)
    void router.push(`/match/${steamData!.matchId}`)
    return
  }

  if (localUpdated?.status === 'closed') {
    stopPoller()
    void router.push('/find-game')
  }
}

async function leaveAndGoBack() {
  stopPoller()
  // Capture the Steam lobby id before clearing reactive state so we can
  // tell Steam we've left. Host leaving destroys the lobby; joiner
  // leaving just removes them from the member list.
  const sid = steamLobbyId.value
  try {
    await leaveLobby({ id: lobbyId.value, playerId: playerId.value })
  } catch {
    // best-effort leave; navigate regardless
  }
  if (sid) {
    // Best-effort. Errors are logged inside leaveSteamLobby — we don't
    // want a Steam SDK hiccup to block the user's nav back.
    await leaveSteamLobby(sid)
  }
  // Clear Steam-paired session state so a subsequent /custom → /create-game
  // run doesn't accidentally try to proxy or invite into a dead lobby.
  try {
    sessionStorage.removeItem(STEAM_LOBBY_ID_KEY)
    sessionStorage.removeItem(STEAM_PROXY_FLAG_KEY)
  } catch {
    /* sessionStorage may be sandboxed */
  }
  clearSteamLobbyPairing()
  void router.push('/custom')
}

async function startGame() {
  if (!lobby.value || !isHost.value) return
  isStarting.value = true
  startError.value = ''
  try {
    const updated = await startLobby({ id: lobbyId.value, playerId: playerId.value })
    lobby.value = updated

    // §14R-D: stamp the matchId into the Steam lobby metadata. The
    // joiner's shell observes LobbyDataUpdate_t and navigates them
    // into /match. Failure here is non-fatal — host's local navigation
    // still works; joiners will see the change on next poll once we
    // retry or the host re-clicks.
    if (steamLobbyId.value && updated.matchId) {
      try {
        await startSteamGame(steamLobbyId.value, updated.matchId)
      } catch (err) {
        console.error('startSteamGame failed:', err)
      }
    }

    if (updated.status === 'started' && updated.matchId) {
      stopPoller()
      setSelectedMapId(updated.mapId, updated.mapName)
      void router.push(`/match/${updated.matchId}`)
    }
  } catch (e) {
    startError.value = e instanceof Error ? e.message : 'Failed to start game.'
  } finally {
    isStarting.value = false
  }
}

async function onInvite() {
  if (!steamLobbyId.value) return
  inviteBusy.value = true
  inviteError.value = ''
  try {
    await inviteFriend(steamLobbyId.value)
  } catch (e) {
    inviteError.value = e instanceof Error ? e.message : 'Invite failed'
  } finally {
    inviteBusy.value = false
  }
}

onMounted(async () => {
  try {
    sessionStorageSteamLobbyId.value = sessionStorage.getItem(STEAM_LOBBY_ID_KEY)
  } catch {
    /* sessionStorage may be sandboxed */
  }
  await poll()
  pollInterval = setInterval(() => { void poll() }, 1000)
})

onUnmounted(() => {
  stopPoller()
})
</script>

<style scoped>
.lobby {
  position: relative;
  z-index: 1;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background: radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%);
  padding: 32px;
  box-sizing: border-box;
}

.lobby__layout {
  display: flex;
  flex-direction: column;
  gap: 24px;
  width: 100%;
  max-width: 600px;
}

.lobby__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.lobby__header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.lobby__title {
  font-size: 22px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.lobby__slots {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.08em;
  color: #d7bb84;
  text-transform: uppercase;
}

.lobby__body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.lobby__players-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.lobby__section-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #d7bb84;
}

.lobby__footer {
  display: flex;
  gap: 12px;
  justify-content: flex-end;
}

.lobby__not-found {
  color: #8899bb;
  font-size: 14px;
  text-align: center;
  padding: 40px 0;
}

.lobby__error {
  font-size: 13px;
  color: #f07070;
}

.lobby__waiting {
  font-size: 13px;
  font-style: italic;
  color: rgba(245, 234, 210, 0.75);
}

.lobby__steam-pending {
  font-size: 12px;
  font-style: italic;
  color: rgba(245, 234, 210, 0.65);
  align-self: center;
  padding-right: 8px;
}
</style>
