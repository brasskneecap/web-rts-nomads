<template>
  <div class="map-list" role="listbox" aria-label="Select map" :style="assetVars">
    <GameScrollArea class="map-list__scroll">
      <div class="map-list__items">
        <!-- Each row: the clickable button (map-row art + text) with a mini
             map preview overlaid on the art's baked thumbnail box. The overlay
             is a sibling (not a child of the button — a canvas wrapper div is
             invalid inside a <button>) and is pointer-events:none so clicks
             still reach the button beneath. -->
        <div v-for="map in maps" :key="map.id" class="map-list__row">
          <button
            class="map-list__item"
            :class="{ 'map-list__item--selected': map.id === selectedMapId }"
            role="option"
            :aria-selected="map.id === selectedMapId"
            type="button"
            @click="emit('update:selectedMapId', map.id)"
          >
            <span class="map-list__name">{{ map.name }}</span>
            <span class="map-list__size">{{ map.gridCols }}x{{ map.gridRows }}</span>
          </button>

          <div class="map-list__thumb" aria-hidden="true">
            <MinimapPreview :map="map" :show-metadata="false" :max-display-size="72" />
          </div>
        </div>

        <div v-if="maps.length === 0" class="map-list__empty">
          {{ loading ? 'Loading maps...' : 'No maps available.' }}
        </div>
      </div>
    </GameScrollArea>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { MapCatalogEntry } from '@/game/network/protocol'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'
import MinimapPreview from '@/components/menu/MinimapPreview.vue'
import rowUrl from '@/assets/ui/themes/updated/war-room/map-row.png'

defineProps<{
  maps: MapCatalogEntry[]
  selectedMapId: string
  loading?: boolean
}>()

const emit = defineEmits<{
  'update:selectedMapId': [id: string]
}>()

// Row art exposed to scoped CSS as a custom property.
const assetVars = computed(() => ({
  '--map-row': `url(${rowUrl})`,
}))
</script>

<style scoped>
.map-list {
  padding: 2px;
}

.map-list__scroll {
  max-height: 360px;
  min-height: 120px;
}

.map-list__items {
  display: flex;
  flex-direction: column;
  gap: calc(var(--s, 1px) * 6);
}

/* Relative wrapper so the thumbnail overlay can be positioned over the row. */
.map-list__row {
  position: relative;
}

/*
 * Each row is the map-row art (source 511x60 ≈ 8.5:1). Rendered at a similar
 * aspect in the list, `background-size: 100% 100%` keeps distortion minimal.
 * The art bakes a thumbnail box on its left ~14%; `padding-left` clears the
 * name past it (plus a 30px nudge to the right).
 */
.map-list__item {
  position: relative;
  display: grid;
  grid-template-columns: 1fr max-content;
  align-items: center;
  gap: calc(var(--s, 1px) * 12);
  width: 100%;
  min-height: calc(var(--s, 1px) * 52);
  padding: 0 calc(var(--s, 1px) * 20) 0 calc(16% + 30px);
  text-align: left;
  background: var(--map-row) center / 100% 100% no-repeat;
  border: 0;
  image-rendering: pixelated;
  color: #e9dbb8;
  font-weight: 600;
}

/* Mini map preview over the art's baked thumbnail box. Position/size are the
   measured blue-interior bounds of map-row.png (511x60): x 14..77, y 7..52 →
   left 2.74%, width 12.52%, top 11.67%, height 76.67% of the row. The row art
   is drawn with background-size:100% 100%, so these percentages track the box
   at any size. */
.map-list__thumb {
  position: absolute;
  left: 2.74%;
  top: 11.67%;
  width: 12.52%;
  height: 76.67%;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  pointer-events: none;
  border-radius: 1px;
}

/* Shrink the bare MinimapPreview down to the box: drop its 120px min-height
   and let the canvas scale to fit. */
.map-list__thumb :deep(.minimap-preview--bare) {
  min-height: 0;
  width: 100%;
  height: 100%;
  align-items: center;
  justify-content: center;
}

/* Stretch the canvas to fill the whole box (override MinimapPreview's inline
   aspect-preserving size). Distorts non-square maps by design. */
.map-list__thumb :deep(.minimap-preview__canvas) {
  width: 100% !important;
  height: 100% !important;
  max-width: none;
  max-height: none;
}

.map-list__thumb :deep(.minimap-preview__status) {
  display: none;
}

.map-list__item:hover .map-list__name {
  color: #fff2cf;
}

.map-list__item--selected {
  filter: brightness(1.12);
}

.map-list__item--selected .map-list__name {
  color: #f4e3b6;
}

.map-list__name {
  font-family: var(--font-title);
  font-size: calc(var(--s, 1px) * 16);
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #e6d3a3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.map-list__size {
  font-size: calc(var(--s, 1px) * 13);
  color: rgba(233, 219, 184, 0.65);
  font-weight: 500;
  white-space: nowrap;
}

.map-list__empty {
  padding: 20px;
  text-align: center;
  color: rgba(233, 219, 184, 0.55);
  font-size: 13px;
}
</style>
