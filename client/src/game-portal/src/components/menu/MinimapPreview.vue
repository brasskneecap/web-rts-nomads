<template>
  <UiPanel class="minimap-preview" :padding="16">
    <div v-if="map" class="minimap-preview__content">
      <div class="minimap-preview__name">{{ map.name }}</div>
      <div v-if="map.description" class="minimap-preview__description">{{ map.description }}</div>
      <div class="minimap-preview__meta">
        <span class="minimap-preview__meta-item">Size: {{ map.gridCols }}x{{ map.gridRows }}</span>
        <span class="minimap-preview__meta-item">
          Players: {{ map.spawnPointCount ?? '?' }}
        </span>
      </div>
      <UiPanel class="minimap-preview__image-area" :padding="8">
        <div v-if="isLoading" class="minimap-preview__status">Loading...</div>
        <div v-else-if="loadError" class="minimap-preview__status minimap-preview__status--error">
          Failed to load preview
        </div>
        <canvas
          v-show="!isLoading && !loadError"
          ref="canvasEl"
          class="minimap-preview__canvas"
          aria-label="Minimap preview"
        />
      </UiPanel>
    </div>
    <div v-else class="minimap-preview__empty">
      Select a map to see details.
    </div>
  </UiPanel>
</template>

<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import type { MapCatalogEntry, MapCatalogFile } from '@/game/network/protocol'
import { fetchMapCatalogFile } from '@/game/maps/catalog'
import {
  DEFAULT_GRASS_COLOR,
  getTerrainColor,
  getObstacleColor,
  getBuildingColor,
} from '@/game/maps/mapConfig'
import UiPanel from '@/components/ui/UiPanel.vue'

const MAX_DISPLAY_SIZE = 240

const props = defineProps<{
  map: MapCatalogEntry | null
}>()

const canvasEl = ref<HTMLCanvasElement | null>(null)
const isLoading = ref(false)
const loadError = ref(false)

const fileCache = new Map<string, MapCatalogFile>()

async function loadAndRender(mapId: string) {
  isLoading.value = true
  loadError.value = false

  let file: MapCatalogFile
  if (fileCache.has(mapId)) {
    file = fileCache.get(mapId)!
  } else {
    try {
      file = await fetchMapCatalogFile(mapId)
      fileCache.set(mapId, file)
    } catch {
      isLoading.value = false
      loadError.value = true
      return
    }
  }

  isLoading.value = false

  await nextTick()

  const canvas = canvasEl.value
  if (!canvas) return

  const mapData = file.map
  const cols = mapData.gridCols
  const rows = mapData.gridRows

  canvas.width = cols
  canvas.height = rows

  const aspectRatio = cols / rows
  let displayW: number
  let displayH: number
  if (aspectRatio >= 1) {
    displayW = MAX_DISPLAY_SIZE
    displayH = Math.round(MAX_DISPLAY_SIZE / aspectRatio)
  } else {
    displayH = MAX_DISPLAY_SIZE
    displayW = Math.round(MAX_DISPLAY_SIZE * aspectRatio)
  }
  canvas.style.width = `${displayW}px`
  canvas.style.height = `${displayH}px`

  const ctx = canvas.getContext('2d')
  if (!ctx) return

  ctx.fillStyle = DEFAULT_GRASS_COLOR
  ctx.fillRect(0, 0, cols, rows)

  for (const tile of mapData.terrain) {
    ctx.fillStyle = getTerrainColor(tile.terrain)
    ctx.fillRect(tile.x, tile.y, 1, 1)
  }

  for (const obstacle of mapData.obstacles) {
    ctx.fillStyle = getObstacleColor(obstacle.obstacle)
    ctx.fillRect(obstacle.x, obstacle.y, obstacle.width ?? 1, obstacle.height ?? 1)
  }

  for (const building of mapData.buildings) {
    if (building.buildingType === 'enemy-spawnpoint') continue
    ctx.fillStyle = getBuildingColor(building.buildingType)
    ctx.fillRect(building.x, building.y, building.width, building.height)
  }
}

watch(
  () => props.map?.id,
  (mapId) => {
    if (!mapId) return
    void loadAndRender(mapId)
  },
  { immediate: true },
)
</script>

<style scoped>
.minimap-preview {
  display: flex;
  flex-direction: column;
  height: 100%;
  box-sizing: border-box;
}

.minimap-preview__content {
  display: flex;
  flex-direction: column;
  gap: 10px;
  height: 100%;
}

.minimap-preview__name {
  font-size: 18px;
  font-weight: 700;
  color: #f5ead2;
}

.minimap-preview__description {
  font-size: 13px;
  color: #cbb893;
  line-height: 1.5;
}

.minimap-preview__meta {
  display: flex;
  gap: 20px;
}

.minimap-preview__meta-item {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: #d7bb84;
}

.minimap-preview__image-area {
  flex: 1 1 auto;
  min-height: 120px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.minimap-preview__canvas {
  image-rendering: pixelated;
  display: block;
  max-width: 100%;
  max-height: 100%;
}

.minimap-preview__status {
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
  color: rgba(203, 184, 147, 0.4);
}

.minimap-preview__status--error {
  color: rgba(240, 112, 112, 0.7);
}

.minimap-preview__empty {
  color: #8899bb;
  font-size: 13px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: 1 1 auto;
  min-height: 120px;
}
</style>
