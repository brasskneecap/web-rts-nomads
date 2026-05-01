<template>
  <div class="map-list" role="listbox" aria-label="Select map">
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
</template>

<script setup lang="ts">
import type { MapCatalogEntry } from '@/game/network/protocol'

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
  display: flex;
  flex-direction: column;
  gap: 2px;
  overflow-y: auto;
  max-height: 360px;
  min-height: 120px;
  padding: 2px;
}

.map-list__item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.16);
  background: rgba(255, 255, 255, 0.04);
  color: #cbb893;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s, border-color 0.1s;
}

.map-list__item:hover {
  background: rgba(255, 255, 255, 0.09);
  border-color: rgba(200, 164, 106, 0.35);
  color: #f5ead2;
}

.map-list__item--selected {
  background: rgba(200, 164, 106, 0.15);
  border-color: rgba(200, 164, 106, 0.5);
  color: #f7d88e;
}

.map-list__size {
  font-size: 11px;
  opacity: 0.65;
  font-weight: 500;
}

.map-list__empty {
  padding: 20px;
  text-align: center;
  color: #8899bb;
  font-size: 13px;
}
</style>
