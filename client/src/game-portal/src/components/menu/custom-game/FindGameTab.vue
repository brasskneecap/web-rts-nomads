<template>
  <div class="cg-find">
    <div class="cg-find__header">
      <span class="cg-find__label">Open Lobbies</span>
      <span v-if="refreshError" class="cg-find__refresh-error">Couldn't refresh</span>
    </div>

    <UiPanel variant="innerPanel" :padding="0" class="cg-find__list">
      <GameScrollArea class="cg-find__scroll">
        <LobbyList :lobbies="lobbies" @join="onJoin" />
      </GameScrollArea>
    </UiPanel>

    <div class="cg-find__footer">
      <BackButton @click="emit('back')" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useLobbies } from '@/composables/useLobbies'
import { usePlayer } from '@/composables/usePlayer'
import { joinLobby as steamJoinLobby, listSteamLobbies } from '@/services/desktopBridge'
import {
  STEAM_LOBBY_ID_KEY,
  STEAM_PROXY_FLAG_KEY,
} from '@/game/network/NetworkClient'
import { hasMapVersion } from '@/services/mapVersionCache'
import type { Lobby } from '@/game/network/protocol'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import LobbyList from '@/components/menu/LobbyList.vue'
import UiPanel from '@/components/ui/UiPanel.vue'
import BackButton from '@/components/menu/custom-game/BackButton.vue'

const { lobbies: localLobbies, refreshList, joinLobby: joinLocalLobby } = useLobbies()
const { playerId } = usePlayer()

// Opens the joined lobby in-panel (CustomGame hosts PanelLobby inline) instead
// of routing to /lobby/:id — so the joiner stays in the war room exactly like
// the host, with no page/background change. `back` closes the Custom Game
// popup (mirrors the Start Game tab's Back button).
const emit = defineEmits<{
  (e: 'lobby-joined', lobbyId: string): void
  (e: 'back'): void
}>()

const refreshError = ref(false)
const steamLobbies = ref<readonly Lobby[]>([])
let pollInterval: ReturnType<typeof setInterval> | null = null

// §14R-C: distinguish a local-lobby id from a Steam-lobby id by prefix.
// Steam entries surface in Find Game as `steam:<steamLobbyId>` so the
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
  steamLobbies.value = entries.map((e) => {
    // Version-mismatch detection: when the host has a non-empty mapHash and
    // the joiner does NOT have a locally-cached entry for that exact
    // (mapId, hash) pair, suffix the map name with a placeholder so the
    // player knows the preview may not match what they'll play.
    // Falls back to the plain mapId label when mapHash is absent (older host).
    const hasHash = !!e.mapHash
    const hashMatches = hasHash && hasMapVersion(e.mapId, e.mapHash)
    const mapNameLabel = hasHash && !hashMatches
      ? `${e.mapId || '(unknown map)'} — Host's custom map`
      : (e.mapId || '(unknown map)')

    return {
      id: `${STEAM_ID_PREFIX}${e.steamLobbyId}`,
      mapId: e.mapId,
      mapName: mapNameLabel,
      hostPlayerId: e.hostPersona || '(unknown host)',
      players: Array.from(
        { length: Math.max(e.playerCount, 1) },
        (_, i) => (i === 0 ? e.hostPersona || 'host' : `slot-${i + 1}`),
      ),
      maxPlayers: e.maxPlayers > 0 ? e.maxPlayers : 4,
      createdAt: 0,
      status: e.status === 'started' ? 'started' : 'open',
    }
  })
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
      // Open the lobby in-panel (same PanelLobby the host sees).
      emit('lobby-joined', result.localLobbyId || steamLobbyId)
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
    // If join fails (e.g. 409 already in lobby), still open the lobby — the
    // lobby polling will surface the real state.
  }
  emit('lobby-joined', id)
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
.cg-find {
  display: flex;
  flex-direction: column;
  min-height: 0;
  flex: 1 1 auto;
}

.cg-find__header {
  flex: 0 0 auto;
  display: flex;
  align-items: baseline;
  gap: calc(var(--s) * 12);
  margin-bottom: calc(var(--s) * 8);
}

/* Gold section label flanked by short rules — matches the Start Game tab. */
.cg-find__label {
  display: flex;
  align-items: center;
  gap: calc(var(--s) * 8);
  font-family: var(--font-title);
  font-size: calc(var(--s) * 15);
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: #e0bd7f;
}

.cg-find__label::before,
.cg-find__label::after {
  content: '';
  height: 1px;
  width: calc(var(--s) * 16);
  background: rgba(224, 189, 127, 0.6);
}

.cg-find__refresh-error {
  font-size: calc(var(--s) * 12);
  color: #e88a6a;
  margin-left: auto;
}

/* Inner-panel well framing the lobby list (matches the map list). */
.cg-find__list {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.cg-find__scroll {
  flex: 1 1 auto;
  min-height: 0;
  padding: calc(var(--s) * 6);
  box-sizing: border-box;
}

/* Footer with the Back button (bottom-left) that closes the popup. Matches the
   Start Game footer geometry so the Back button stays put across tabs. */
.cg-find__footer {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  padding-top: calc(var(--s) * 12);
}
</style>
