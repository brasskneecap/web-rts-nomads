<template>
  <div class="match-view">
    <div v-if="showResumePrompt" class="menu">
      <div class="menu-title">Return to previous game?</div>
      <div class="menu-text">
        You have a saved player session with map size
        <strong>{{ selectedSize }}</strong>.
      </div>

      <div class="menu-actions">
        <button @click="returnToPreviousGame">Return</button>
        <button @click="startNewGame">Start New Game</button>
      </div>
    </div>

    <div v-else-if="showSizeMenu" class="menu">
      <div class="menu-title">Choose Map Size</div>

      <label for="map-size">Map Size:</label>
      <select id="map-size" v-model="selectedSize">
        <option value="small">Small</option>
        <option value="medium">Medium</option>
        <option value="large">Large</option>
      </select>

      <div class="menu-actions">
        <button @click="startGame(selectedSize, { resume: false })">Start Game</button>
      </div>
    </div>

    <MatchHud v-if="hasStarted" :ui="ui" />

    <div class="match-stage">
      <canvas ref="canvas" class="game-canvas"></canvas>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount } from 'vue'
import MatchHud from '@/components/MatchHud.vue'
import { useGameClient } from '@/composables/useGameClient'
import type { MapSize } from '@/game/network/protocol'

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_SIZE_STORAGE_KEY = 'webrts.mapSize'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'

const canvas = ref<HTMLCanvasElement | null>(null)

const selectedSize = ref<MapSize>(
  (localStorage.getItem(MAP_SIZE_STORAGE_KEY) as MapSize) || 'large',
)

watch(selectedSize, (val) => {
  localStorage.setItem(MAP_SIZE_STORAGE_KEY, val)
})

const hasPreviousSession = ref(
  localStorage.getItem(HAS_ACTIVE_SESSION_KEY) === 'true' &&
    !!localStorage.getItem(PLAYER_ID_STORAGE_KEY) &&
    !!localStorage.getItem(MATCH_ID_STORAGE_KEY),
)

const hasStarted = ref(false)

const showResumePrompt = computed(
  () => !hasStarted.value && hasPreviousSession.value,
)

const showSizeMenu = computed(
  () => !hasStarted.value && !hasPreviousSession.value,
)

const { init, destroy, leaveStoredMatch, ui } = useGameClient()

async function startGame(size: MapSize, options: { resume?: boolean } = {}) {
  if (!canvas.value) return

  await init(canvas.value, size, options)
  hasStarted.value = true

  localStorage.setItem(MAP_SIZE_STORAGE_KEY, size)
  localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
}

async function returnToPreviousGame() {
  await startGame(selectedSize.value, { resume: true })
}

async function startNewGame() {
  await leaveStoredMatch()

  localStorage.removeItem(PLAYER_ID_STORAGE_KEY)
  localStorage.removeItem(MAP_SIZE_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)

  selectedSize.value = 'large'
  hasPreviousSession.value = false
}

function markActiveSession() {
  if (hasStarted.value) {
    localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
  }
}

window.addEventListener('beforeunload', markActiveSession)

onBeforeUnmount(() => {
  window.removeEventListener('beforeunload', markActiveSession)
  destroy()
})
</script>

<style scoped>
.match-view {
  width: 100vw;
  height: 100vh;
  position: relative;
  overflow: hidden;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  background:
    radial-gradient(circle at top, rgba(36, 55, 87, 0.35), transparent 48%),
    #05080d;
}

.menu {
  position: absolute;
  top: 16px;
  left: 16px;
  z-index: 20;
  min-width: 260px;
  background: rgba(0, 0, 0, 0.75);
  color: white;
  padding: 12px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  backdrop-filter: blur(10px);
}

.menu-title {
  font-weight: 700;
  margin-bottom: 8px;
}

.menu-text {
  margin-bottom: 10px;
}

.menu-actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.match-stage {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
}

.game-canvas {
  width: 100%;
  height: 100%;
  display: block;
  background: #111;
}
</style>
