<template>
  <div class="find-game">
    <div class="find-game__layout">
      <header class="find-game__header">
        <UiButton size="sm" @click="router.push('/custom')">Back</UiButton>
        <h1 class="find-game__title">Find Game</h1>
        <span v-if="refreshError" class="find-game__refresh-error">Couldn't refresh</span>
      </header>

      <UiPanel class="find-game__list-panel" :padding="16">
        <LobbyList :lobbies="lobbies" @join="onJoin" />
      </UiPanel>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useLobbies } from '@/composables/useLobbies'
import { usePlayer } from '@/composables/usePlayer'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import LobbyList from '@/components/menu/LobbyList.vue'

const router = useRouter()
const { lobbies, refreshList, joinLobby } = useLobbies()
const { playerId } = usePlayer()

const refreshError = ref(false)
let pollInterval: ReturnType<typeof setInterval> | null = null

async function poll() {
  try {
    await refreshList()
    refreshError.value = false
  } catch {
    refreshError.value = true
  }
}

async function onJoin(id: string) {
  try {
    await joinLobby({ id, playerId: playerId.value })
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
  overflow-y: auto;
}
</style>
