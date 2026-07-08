// Lobby-room composable — owns the live lobby state, Steam-aware polling, and
// the start / invite / leave handlers for a single lobby. Extracted from the
// former Lobby.vue so the same behavior can back two presentations:
//   1. The routed dark full-page lobby (Lobby.vue, Custom Game flow).
//   2. The parchment in-panel lobby embedded in the Campaign and Custom
//      Game panels (PanelLobby.vue), which stays inside the war-room
//      parchment.
//
// The caller supplies the lobby id (route param or prop) and an `onLeave`
// callback. `onLeave` fires for every "we're done here" outcome — the user
// leaving, or the lobby vanishing / closing server-side — so each caller
// decides where to go (route back to Find Game, or pop back to the campaign
// level list) without this composable knowing about routes.
//
// Match-start navigation (→ /match/:id) stays here: starting a match always
// leaves the current screen entirely regardless of which presentation hosted
// the lobby, so it's a shared concern.

import { computed, onMounted, onUnmounted, ref, type Ref } from 'vue'
import { useRouter } from 'vue-router'
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
import { hasMapVersion } from '@/services/mapVersionCache'
import type { Lobby } from '@/game/network/protocol'

export interface UseLobbyRoomOptions {
  /** Called when the lobby is done being shown: the user left, or the lobby
   *  vanished / closed server-side. The caller navigates / pops as needed. */
  onLeave: () => void
}

export function useLobbyRoom(lobbyId: Readonly<Ref<string>>, opts: UseLobbyRoomOptions) {
  const router = useRouter()
  const { fetchLobby, leaveLobby, startLobby } = useLobbies()
  const { playerId } = usePlayer()
  const { setSelectedMapId } = useMapSelection()

  const lobby = ref<Lobby | null>(null)
  const isHost = computed(() => lobby.value?.hostPlayerId === playerId.value)
  const isStarting = ref(false)
  const startError = ref('')

  // Latest mapHash from the Steam lobby poll. Used for version-mismatch detection.
  const latestSteamMapHash = ref<string>('')

  /** True when the host's mapHash is non-empty AND the joiner does not have a
   *  locally-cached entry for this exact (mapId, hash) pair. Triggers the
   *  "Host's custom map — loads at start" placeholder label in the header.
   *  Falls back to false (existing behavior) when mapHash is absent/empty
   *  (older host) or when the joiner already has the host's version. */
  const showMapVersionPlaceholder = computed<boolean>(() => {
    const hash = latestSteamMapHash.value
    if (!hash) return false
    const mapId = lobby.value?.mapId
    if (!mapId) return false
    return !hasMapVersion(mapId, hash)
  })

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
  // on a cold mount. The host's flow is Create Lobby → nav to the lobby;
  // the local lobby is created synchronously before the nav, but in dev
  // we've observed the very first GET /lobbies/<id> occasionally race with
  // the in-memory map insert. Allow ~10s of retries before giving up.
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
  function synthesizeLobbyFromSteam(
    _steamLobbyId: string,
    data: Awaited<ReturnType<typeof getSteamLobbyData>>,
  ): Lobby | null {
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

    // Capture the host's map hash for version-mismatch detection.
    if (steamData?.mapHash) {
      latestSteamMapHash.value = steamData.mapHash
    }

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
      opts.onLeave()
      return
    } else {
      // Cold mount with neither local nor Steam data yet. The host's
      // optimistic-nav path means we landed here while Steam was still
      // creating the lobby in the background — don't give up until we've
      // waited COLD_POLL_TOLERANCE polls (~10s). Hosts and joiners both
      // benefit: a transient HTTP 502 or a slow Steam callback should
      // never bounce the user out of the lobby.
      coldPollMisses += 1
      if (coldPollMisses >= COLD_POLL_TOLERANCE) {
        console.warn(
          `Lobby: ${COLD_POLL_TOLERANCE} consecutive empty polls for ${lobbyId.value}; leaving.`,
        )
        stopPoller()
        opts.onLeave()
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
      opts.onLeave()
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
    // Clear Steam-paired session state so a subsequent Custom Game → Start Game
    // run doesn't accidentally try to proxy or invite into a dead lobby.
    try {
      sessionStorage.removeItem(STEAM_LOBBY_ID_KEY)
      sessionStorage.removeItem(STEAM_PROXY_FLAG_KEY)
    } catch {
      /* sessionStorage may be sandboxed */
    }
    clearSteamLobbyPairing()
    opts.onLeave()
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

  return {
    lobby,
    isHost,
    isStarting,
    startError,
    inviteError,
    inviteBusy,
    steamLobbyId,
    steamLobbyPending,
    showMapVersionPlaceholder,
    startGame,
    onInvite,
    leaveAndGoBack,
  }
}
