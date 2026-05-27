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
  buildTerrainSurface,
  drawMinimapBase,
  drawMinimapPOIs,
} from '@/game/rendering/minimapLayers'
import { isTerrainTilesetReady, onSheetReady } from '@/game/rendering/terrainTileset'
import UiPanel from '@/components/ui/UiPanel.vue'

const MAX_DISPLAY_SIZE = 240

const props = defineProps<{
  map: MapCatalogEntry | null
}>()

const canvasEl = ref<HTMLCanvasElement | null>(null)
const isLoading = ref(false)
const loadError = ref(false)

const fileCache = new Map<string, MapCatalogFile>()

// Tracks which map is currently "displayed" so an async tileset-ready
// callback doesn't redraw a stale map after the user switched selections.
let currentMapId: string | null = null
let tilesetReadyHookInstalled = false

async function loadAndRender(mapId: string) {
  isLoading.value = true
  loadError.value = false
  currentMapId = mapId

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
  if (currentMapId !== mapId) return // user already switched maps

  renderMap(file)
}

function renderMap(file: MapCatalogFile) {
  const canvas = canvasEl.value
  if (!canvas) return

  const mapData = file.map
  const cols = mapData.gridCols
  const rows = mapData.gridRows
  const aspectRatio = cols / rows

  // Canvas resolution is the actual display size (in CSS pixels). This is
  // the same approach the in-game minimap uses — the world is downscaled
  // into a screen-pixel-resolution rect with smoothing, so the result
  // matches the in-game look.
  let displayW: number
  let displayH: number
  if (aspectRatio >= 1) {
    displayW = MAX_DISPLAY_SIZE
    displayH = Math.round(MAX_DISPLAY_SIZE / aspectRatio)
  } else {
    displayH = MAX_DISPLAY_SIZE
    displayW = Math.round(MAX_DISPLAY_SIZE * aspectRatio)
  }
  canvas.width = displayW
  canvas.height = displayH
  canvas.style.width = `${displayW}px`
  canvas.style.height = `${displayH}px`

  const ctx = canvas.getContext('2d')
  if (!ctx) return

  const terrainSurface = buildTerrainSurface(mapData)
  const bounds = { x: 0, y: 0, width: displayW, height: displayH }
  drawMinimapBase(ctx, mapData, bounds, terrainSurface)
  drawMinimapPOIs(ctx, mapData, bounds, null)

  // First-visit case: the sprite tileset may still be loading. The base
  // renderer falls back to category-color terrain, but we want the real
  // sprite look as soon as the asset arrives. Re-render once the sheet
  // becomes ready, but only if the user hasn't switched maps in the
  // meantime. Install the hook at most once.
  if (!isTerrainTilesetReady() && !tilesetReadyHookInstalled) {
    tilesetReadyHookInstalled = true
    onSheetReady('tileset', () => {
      const id = currentMapId
      if (!id) return
      const cached = fileCache.get(id)
      if (cached) renderMap(cached)
    })
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
