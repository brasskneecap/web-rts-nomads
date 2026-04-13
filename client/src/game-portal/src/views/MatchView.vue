<template>
  <div class="match-view">
    <div v-if="showResumePrompt" class="menu">
      <div class="menu-title">Return to previous game?</div>
      <div class="menu-text">
        You have a saved player session on
        <strong>{{ selectedMapName }}</strong>.
      </div>

      <div class="menu-actions">
        <button @click="returnToPreviousGame">Return</button>
        <button @click="startNewGame">Start New Game</button>
      </div>
    </div>

    <div v-else-if="showSizeMenu" class="menu">
      <div class="menu-title">Choose Map</div>

      <label for="map-id">Map:</label>
      <select id="map-id" v-model="selectedMapId" :disabled="isLoadingMaps || mapCatalog.length === 0">
        <option v-for="map in mapCatalog" :key="map.id" :value="map.id">
          {{ map.name }}
        </option>
      </select>

      <div class="menu-text" v-if="selectedMapDescription">
        {{ selectedMapDescription }}
      </div>

      <div class="menu-text" v-if="isLoadingMaps">Loading maps...</div>
      <div class="menu-text" v-else-if="mapsLoadError">{{ mapsLoadError }}</div>

      <div class="menu-actions">
        <button @click="startGame(selectedMapId, { resume: false })" :disabled="!selectedMapId || isLoadingMaps">
          Start Game
        </button>
        <button @click="editorMode = !editorMode">
          {{ editorMode ? 'Back To Maps' : 'Open Editor' }}
        </button>
      </div>
    </div>

    <MatchHud v-if="hasStarted" :ui="ui" />

    <div class="match-stage" :class="{ 'match-stage--editor': showEditor }">
      <canvas v-show="!showEditor" ref="canvas" class="game-canvas"></canvas>
      <div v-if="showEditor" class="editor-stage">
        <MapEditorPanel v-model="editorMap" />
      </div>
    </div>

    <SelectionHud v-if="hasStarted" :ui="ui" />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onBeforeUnmount, onMounted } from 'vue'
import MapEditorPanel from '@/components/MapEditorPanel.vue'
import MatchHud from '@/components/MatchHud.vue'
import SelectionHud from '@/components/SelectionHud.vue'
import { useGameClient } from '@/composables/useGameClient'
import { fetchMapCatalog } from '@/game/maps/catalog'
import type { MapCatalogEntry, MapId } from '@/game/network/protocol'
import { createEditorMapConfig } from '@/game/maps/mapConfig'

const PLAYER_ID_STORAGE_KEY = 'webrts.playerId'
const MAP_ID_STORAGE_KEY = 'webrts.mapId'
const MATCH_ID_STORAGE_KEY = 'webrts.matchId'
const HAS_ACTIVE_SESSION_KEY = 'webrts.hasActiveSession'
const MAP_EDITOR_STORAGE_KEY = 'webrts.mapEditorDraft'

const canvas = ref<HTMLCanvasElement | null>(null)
const mapCatalog = ref<MapCatalogEntry[]>([])
const isLoadingMaps = ref(true)
const mapsLoadError = ref('')
const editorMode = ref(false)

function getStoredEditorMap() {
  const stored = localStorage.getItem(MAP_EDITOR_STORAGE_KEY)

  if (!stored) {
    return createEditorMapConfig()
  }

  try {
    return createEditorMapConfig(undefined, undefined, JSON.parse(stored))
  } catch {
    return createEditorMapConfig()
  }
}

const selectedMapId = ref<MapId>(localStorage.getItem(MAP_ID_STORAGE_KEY) ?? '')
const editorMap = ref(getStoredEditorMap())

watch(selectedMapId, (val) => {
  if (val) {
    localStorage.setItem(MAP_ID_STORAGE_KEY, val)
  }
})

watch(
  editorMap,
  (val) => {
    localStorage.setItem(MAP_EDITOR_STORAGE_KEY, JSON.stringify(val))
  },
  { deep: true },
)

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

const showEditor = computed(
  () => showSizeMenu.value && editorMode.value,
)

const selectedMap = computed(
  () => mapCatalog.value.find((map) => map.id === selectedMapId.value) ?? null,
)

const selectedMapName = computed(() => {
  if (selectedMap.value?.name) return selectedMap.value.name
  if (selectedMapId.value) return selectedMapId.value
  return 'Unknown Map'
})

const selectedMapDescription = computed(
  () => selectedMap.value?.description ?? '',
)

const { init, destroy, leaveStoredMatch, ui } = useGameClient()

async function startGame(mapId: MapId, options: { resume?: boolean } = {}) {
  if (!canvas.value) return
  if (!mapId) return

  await init(canvas.value, mapId, options)
  hasStarted.value = true

  localStorage.setItem(MAP_ID_STORAGE_KEY, mapId)
  localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
}

async function returnToPreviousGame() {
  await startGame(selectedMapId.value, { resume: true })
}

async function startNewGame() {
  await leaveStoredMatch()

  localStorage.removeItem(PLAYER_ID_STORAGE_KEY)
  localStorage.removeItem(MAP_ID_STORAGE_KEY)
  localStorage.removeItem(MATCH_ID_STORAGE_KEY)
  localStorage.removeItem(HAS_ACTIVE_SESSION_KEY)

  selectedMapId.value = mapCatalog.value[0]?.id ?? ''
  hasPreviousSession.value = false
}

async function loadMapCatalog() {
  isLoadingMaps.value = true
  mapsLoadError.value = ''

  try {
    const maps = await fetchMapCatalog()
    mapCatalog.value = maps

    if (!maps.some((map) => map.id === selectedMapId.value)) {
      selectedMapId.value = maps[0]?.id ?? ''
    }
  } catch (error) {
    mapsLoadError.value =
      error instanceof Error ? error.message : 'Failed to load maps.'
  } finally {
    isLoadingMaps.value = false
  }
}

function markActiveSession() {
  if (hasStarted.value) {
    localStorage.setItem(HAS_ACTIVE_SESSION_KEY, 'true')
  }
}

window.addEventListener('beforeunload', markActiveSession)

onMounted(() => {
  void loadMapCatalog()
})

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

.match-stage--editor {
  padding: 84px 16px 16px;
}

.game-canvas {
  width: 100%;
  height: 100%;
  display: block;
  background: #111;
}

.editor-stage {
  width: 100%;
  height: 100%;
}
</style>
