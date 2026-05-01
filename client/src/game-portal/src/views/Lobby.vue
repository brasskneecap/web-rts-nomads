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
        <div v-if="startError" class="lobby__error">{{ startError }}</div>
      </div>

      <div v-else class="lobby__not-found">
        Lobby not found.
      </div>

      <footer class="lobby__footer">
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

let pollInterval: ReturnType<typeof setInterval> | null = null

function stopPoller() {
  if (pollInterval !== null) {
    clearInterval(pollInterval)
    pollInterval = null
  }
}

function handleLobbyUpdate(updated: Lobby | null) {
  if (updated === null) {
    stopPoller()
    void router.push('/find-game')
    return
  }

  lobby.value = updated

  if (updated.status === 'started' && updated.matchId) {
    stopPoller()
    setSelectedMapId(updated.mapId, updated.mapName)
    void router.push(`/match/${updated.matchId}`)
    return
  }

  if (updated.status === 'closed') {
    stopPoller()
    void router.push('/find-game')
  }
}

async function poll() {
  const updated = await fetchLobby(lobbyId.value)
  handleLobbyUpdate(updated)
}

async function leaveAndGoBack() {
  stopPoller()
  try {
    await leaveLobby({ id: lobbyId.value, playerId: playerId.value })
  } catch {
    // best-effort leave; navigate regardless
  }
  void router.push('/custom')
}

async function startGame() {
  if (!lobby.value || !isHost.value) return
  isStarting.value = true
  startError.value = ''
  try {
    const updated = await startLobby({ id: lobbyId.value, playerId: playerId.value })
    handleLobbyUpdate(updated)
  } catch (e) {
    startError.value = e instanceof Error ? e.message : 'Failed to start game.'
  } finally {
    isStarting.value = false
  }
}

onMounted(async () => {
  await poll()
  pollInterval = setInterval(() => { void poll() }, 1000)
})

onUnmounted(() => {
  stopPoller()
})
</script>

<style scoped>
.lobby {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  background:
    radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%),
    #05080d;
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
</style>
