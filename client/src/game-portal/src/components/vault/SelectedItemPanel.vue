<template>
  <div class="sel">
    <template v-if="item">
      <div class="sel__head">
        <div class="sel__icon-frame" :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }">
          <ActionIcon
            class="sel__icon"
            :action="{ id: item.itemId, label: item.displayName, iconDef: { kind: 'item', type: item.itemId } }"
          />
        </div>
        <div class="sel__heading">
          <div class="sel__name">{{ item.displayName }}</div>
          <div v-if="item.tier" class="sel__tier" :style="{ color: item.tierColor }">
            {{ capitalize(item.tier) }}
          </div>
        </div>
      </div>
      <div v-if="item.stats" class="sel__stats">{{ item.stats }}</div>
      <p v-if="item.description" class="sel__desc">{{ item.description }}</p>
      <div class="sel__hint">Drag onto a unit's inventory slot to equip.</div>
    </template>
    <template v-else>
      <div class="sel__empty">
        <span class="sel__empty-icon" aria-hidden="true">ⓘ</span>
        <span>Select an item to see its stats, or drag it onto a unit to equip.</span>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import ActionIcon from '@/components/ActionIcon.vue'
import type { VaultSelectedItem } from './types'

defineProps<{
  item: VaultSelectedItem | null
  iconContainerUrl: string
}>()

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}
</script>

<style scoped>
.sel {
  border: 1px solid rgba(212, 168, 79, 0.22);
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.3);
  padding: 12px;
  /* Reserve a consistent footprint so the empty state occupies the same space
     as a populated item (icon row + stats + description + hint). */
  min-height: 150px;
  box-sizing: border-box;
}

.sel__head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.sel__icon-frame {
  position: relative;
  flex: 0 0 56px;
  width: 56px;
  height: 56px;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
}

.sel__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.sel__heading {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.sel__name {
  font-size: 15px;
  font-weight: 700;
  color: #f5e4c0;
}

.sel__tier {
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.sel__stats {
  margin-top: 10px;
  font-size: 12px;
  font-weight: 700;
  line-height: 1.5;
  color: #ffe9a0;
}

.sel__desc {
  margin: 8px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: rgba(232, 217, 184, 0.8);
}

.sel__hint {
  margin-top: 10px;
  font-size: 11px;
  color: rgba(96, 165, 250, 0.85);
}

.sel__empty {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 12px;
  line-height: 1.5;
  color: rgba(232, 217, 184, 0.55);
}

.sel__empty span {
  /* Let the message wrap onto multiple lines within the panel. */
  overflow-wrap: anywhere;
}

.sel__empty-icon {
  flex: 0 0 auto;
  font-size: 15px;
  color: rgba(212, 168, 79, 0.7);
}
</style>
