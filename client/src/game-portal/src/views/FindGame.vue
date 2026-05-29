<template>
  <div class="find-game">
    <div class="find-game__layout">
      <header class="find-game__header">
        <UiButton size="sm" @click="router.push('/custom')">Back</UiButton>
        <h1 class="find-game__title">Find Game</h1>
        <span v-if="refreshError" class="find-game__refresh-error">Couldn't refresh</span>
      </header>

      <UiPanel class="find-game__list-panel" :padding="16">
        <GameScrollArea class="find-game__scroll">
          <LobbyList :lobbies="lobbies" @join="onJoin" />
        </GameScrollArea>
      </UiPanel>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useLobbies } from '@/composables/useLobbies'
import { usePlayer } from '@/composables/usePlayer'
import { joinLobby as steamJoinLobby, listSteamLobbies } from '@/services/desktopBridge'
import {
  STEAM_LOBBY_ID_KEY,
  STEAM_PROXY_FLAG_KEY,
} from '@/game/network/NetworkClient'
import type { Lobby } from '@/game/network/protocol'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import LobbyList from '@/components/menu/LobbyList.vue'

const router = useRouter()
const { lobbies: localLobbies, refreshList, joinLobby: joinLocalLobby } = useLobbies()
const { playerId } = usePlayer()

const refreshError = ref(false)
const steamLobbies = ref<readonly Lobby[]>([])
let pollInterval: ReturnType<typeof setInterval> | null = null

// §14R-C: distinguish a local-lobby id from a Steam-lobby id by prefix.
// Steam entries surface in /find-game as `steam:<steamLobbyId>` so the
// click handler can dispatch to the right join path. The local Lobby
// shape has no source-discriminator field; encoding it in the id keeps
// the LobbyList component generic.
const STEAM_ID_PREFIX = 'steam:'

/** Union of local lobby entries and Steam friend lobby entries, both
 *  shaped as the existing `Lobby` type so LobbyList can render them
 *  uniformly. Steam entries get the `steam:` id prefix; the playersList
 *  fakes one persona per slot so the "X / Y players" cell renders right
 *  without needing a real member list (the joiner's /lobby will fetch
 *  the real one). */
const lobbies = computed<readonly Lobby[]>(() => {
  const merged: Lobby[] = [...localLobbies.value]
  for (const s of steamLobbies.value) {
    merged.push(s)
  }
  return merged
})

async function pollLocal() {
  try {
    await refreshList()
    refreshError.value = false
  } catch {
    refreshError.value = true
  }
}

async function pollSteam() {
  const entries = await listSteamLobbies()
  // Adapt to the existing Lobby type so LobbyList doesn't need changes.
  steamLobbies.value = entries.map((e) => ({
    id: `${STEAM_ID_PREFIX}${e.steamLobbyId}`,
    mapId: e.mapId,
    mapName: e.mapId || '(unknown map)',
    hostPlayerId: e.hostPersona || '(unknown host)',
    players: Array.from(
      { length: Math.max(e.playerCount, 1) },
      (_, i) => (i === 0 ? e.hostPersona || 'host' : `slot-${i + 1}`),
    ),
    maxPlayers: e.maxPlayers > 0 ? e.maxPlayers : 4,
    createdAt: 0,
    status: e.status === 'started' ? 'started' : 'open',
  }))
}

async function poll() {
  await Promise.all([pollLocal(), pollSteam()])
}

async function onJoin(id: string) {
  if (id.startsWith(STEAM_ID_PREFIX)) {
    const steamLobbyId = id.slice(STEAM_ID_PREFIX.length)
    try {
      const result = await steamJoinLobby(steamLobbyId)
      if (!result) {
        refreshError.value = true
        return
      }
      // §14R-C: stash the steam-proxy intent + the steam lobby id so
      // (1) the next WS open uses ?proxy=steam and (2) /lobby polls
      // Steam metadata for the player list / status.
      try {
        sessionStorage.setItem(STEAM_PROXY_FLAG_KEY, '1')
        sessionStorage.setItem(STEAM_LOBBY_ID_KEY, steamLobbyId)
      } catch {
        /* sessionStorage may be sandboxed */
      }
      // Navigate to the same /lobby/<id> the host's view uses.
      void router.push(`/lobby/${result.localLobbyId || steamLobbyId}`)
    } catch (e) {
      console.error('Steam joinLobby failed:', e)
      refreshError.value = true
    }
    return
  }

  // Local lobby — existing path.
  try {
    await joinLocalLobby({ id, playerId: playerId.value })
  } catch {
    // If join fails (e.g. 409 already in lobby), still navigate — the lobby
    // polling will surface the real state.
  }
  void router.push(`/lobby/${id}`)
}

onMounted(() => {
  void poll()
  pollInterval = setInterval(() => { void poll() }, 2000)
})

onUnmounted(() => {
  if (pollInterval !== null) {
    clearInterval(pollInterval)
    pollInterval = null
  }
})
</script>

<style scoped>
.find-game {
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

.find-game__layout {
  display: flex;
  flex-direction: column;
  gap: 24px;
  width: 100%;
  max-width: 720px;
}

.find-game__header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.find-game__title {
  font-size: 24px;
  font-weight: 700;
  color: #f5ead2;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  margin: 0;
}

.find-game__refresh-error {
  font-size: 12px;
  color: #f07070;
  margin-left: auto;
}

.find-game__list-panel {
  max-height: 500px;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.find-game__scroll {
  flex: 1 1 auto;
  min-height: 0;
}
</style>
