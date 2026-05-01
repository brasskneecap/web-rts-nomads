<template>
  <div class="find-game">
    <div class="find-game__layout">
      <header class="find-game__header">
        <UiButton size="sm" @click="router.push('/custom')">Back</UiButton>
        <h1 class="find-game__title">Find Game</h1>
      </header>

      <UiPanel class="find-game__list-panel" :padding="16">
        <LobbyList :lobbies="sortedLobbies" @join="onJoin" />
      </UiPanel>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useLobbies } from '@/composables/useLobbies'
import { useMapSelection } from '@/composables/useMapSelection'
import UiPanel from '@/components/ui/UiPanel.vue'
import UiButton from '@/components/ui/UiButton.vue'
import LobbyList from '@/components/menu/LobbyList.vue'

const router = useRouter()
const { lobbies, joinLobby, getLobby } = useLobbies()
const { setSelectedMapId } = useMapSelection()

const sortedLobbies = computed(() =>
  [...lobbies.value].sort((a, b) => b.createdAt - a.createdAt),
)

function onJoin(id: string) {
  joinLobby(id)
  const lobby = getLobby(id)
  if (lobby) {
    setSelectedMapId(lobby.mapId, lobby.mapName)
  }
  void router.push(`/lobby/${id}`)
}
</script>

<style scoped>
.find-game {
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

.find-game__list-panel {
  max-height: 500px;
  overflow-y: auto;
}
</style>
