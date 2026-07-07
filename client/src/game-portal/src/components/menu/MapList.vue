<template>
  <div class="map-list" role="listbox" aria-label="Select map">
    <GameScrollArea class="map-list__scroll">
      <div class="map-list__items">
        <button
          v-for="map in maps"
          :key="map.id"
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

        <div v-if="maps.length === 0" class="map-list__empty">
          {{ loading ? 'Loading maps...' : 'No maps available.' }}
        </div>
      </div>
    </GameScrollArea>
  </div>
</template>

<script setup lang="ts">
import type { MapCatalogEntry } from '@/game/network/protocol'
import GameScrollArea from '@/components/ui/GameScrollArea.vue'

defineProps<{
  maps: MapCatalogEntry[]
  selectedMapId: string
  loading?: boolean
}>()

const emit = defineEmits<{
  'update:selectedMapId': [id: string]
}>()
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
  gap: 2px;
}

/* Parchment palette — this list lives only inside the war-room Custom Game
   panel, so it's themed to match the parchment surface (dark ink on warm
   paper) rather than the dark UI panels. */
.map-list__item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  border-radius: 6px;
  border: 1px solid rgba(58, 31, 10, 0.25);
  background: rgba(245, 234, 210, 0.45);
  color: #3a1f0a;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s, border-color 0.1s;
}

.map-list__item:hover {
  background: rgba(230, 214, 178, 0.6);
  border-color: rgba(58, 31, 10, 0.4);
  color: #2a1505;
}

.map-list__item--selected {
  background: rgba(200, 180, 110, 0.55);
  border-color: #8a5a2a;
  color: #2a1505;
  box-shadow: 0 0 0 1px rgba(138, 90, 42, 0.45);
}

.map-list__size {
  font-size: 11px;
  opacity: 0.7;
  font-weight: 500;
}

.map-list__empty {
  padding: 20px;
  text-align: center;
  color: rgba(58, 31, 10, 0.55);
  font-size: 13px;
}
</style>
