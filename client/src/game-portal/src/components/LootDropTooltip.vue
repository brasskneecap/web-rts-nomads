<template>
  <!-- Positioned at the cursor; only rendered when a loot drop is hovered. -->
  <div
    v-if="drop"
    class="loot-tooltip"
    :style="tooltipStyle"
    role="tooltip"
    aria-live="polite"
  >
    <div class="loot-tooltip__title">Treasure Chest</div>

    <!-- Resources block -->
    <div v-if="resourceRows.length > 0" class="loot-tooltip__resources">
      <span
        v-for="row in resourceRows"
        :key="row.label"
        class="loot-tooltip__resource-row"
      >
        <span class="loot-tooltip__resource-amount">+{{ row.amount }}</span>
        <span class="loot-tooltip__resource-label">{{ row.label }}</span>
      </span>
    </div>

    <!-- Items block -->
    <div v-if="itemRows.length > 0" class="loot-tooltip__items">
      <div
        v-for="item in itemRows"
        :key="item.id"
        class="loot-tooltip__item-row"
      >
        <span class="loot-tooltip__item-name">{{ item.displayName }}</span>
        <span v-if="item.category" class="loot-tooltip__item-category">{{ item.category }}</span>
      </div>
    </div>

    <div v-if="resourceRows.length === 0 && itemRows.length === 0" class="loot-tooltip__empty">
      Unknown contents
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { LootDropSnapshot } from '@/game/network/protocol'
import { ITEM_DEF_MAP } from '@/game/maps/itemDefs'

const props = defineProps<{
  drop: LootDropSnapshot | null
  // Viewport-relative cursor position (event.clientX / clientY). Pinned
  // directly into position:fixed coordinates — no canvas rect needed.
  cursorClientX: number
  cursorClientY: number
}>()

// Tooltip offset from cursor so it doesn't sit directly under the pointer.
const OFFSET_X = 14
const OFFSET_Y = -8

const tooltipStyle = computed(() => ({
  left: `${props.cursorClientX + OFFSET_X}px`,
  top:  `${props.cursorClientY + OFFSET_Y}px`,
}))

const resourceRows = computed(() => {
  const resources = props.drop?.resources
  if (!resources) return []
  return Object.entries(resources).map(([key, amount]) => ({
    label: key,
    amount,
  }))
})

const itemRows = computed(() => {
  const ids = props.drop?.itemIds
  if (!ids) return []
  return ids.map((id) => {
    const def = ITEM_DEF_MAP.get(id)
    return {
      id,
      displayName: def?.displayName ?? id,
      category: def?.category ?? undefined,
    }
  })
})
</script>

<style scoped>
.loot-tooltip {
  position: fixed;
  z-index: 9999;
  pointer-events: none;
  background: rgba(20, 15, 5, 0.92);
  border: 1px solid rgba(245, 180, 0, 0.65);
  border-radius: 6px;
  padding: 8px 10px;
  min-width: 120px;
  max-width: 220px;
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.6);
  font-family: inherit;
  color: #f5ead2;
  font-size: 12px;
  line-height: 1.5;
}

.loot-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #f5b400;
  margin-bottom: 6px;
  letter-spacing: 0.04em;
}

.loot-tooltip__resources {
  display: flex;
  flex-direction: column;
  gap: 2px;
  margin-bottom: 4px;
}

.loot-tooltip__resource-row {
  display: flex;
  gap: 5px;
  align-items: baseline;
}

.loot-tooltip__resource-amount {
  color: #86efac;
  font-weight: 700;
  font-size: 12px;
}

.loot-tooltip__resource-label {
  color: #d7bb84;
  text-transform: capitalize;
  font-size: 11px;
}

.loot-tooltip__items {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.loot-tooltip__item-row {
  display: flex;
  gap: 4px;
  align-items: baseline;
  flex-wrap: wrap;
}

.loot-tooltip__item-name {
  color: #f0e6ce;
  font-weight: 600;
}

.loot-tooltip__item-category {
  color: #9ca3af;
  font-size: 10px;
}

.loot-tooltip__empty {
  color: #6b7280;
  font-style: italic;
  font-size: 11px;
}
</style>
