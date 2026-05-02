<template>
  <div
    v-if="show"
    class="upgrade-center"
    :class="{ collapsed, dragging: drag.dragging.value }"
    :style="drag.style.value"
  >
    <header
      class="uc-head"
      :class="{ 'uc-head--dragging': drag.dragging.value }"
      v-bind="drag.handleBindings"
      aria-label="Drag to move"
    >
      <span class="uc-grip" aria-hidden="true">⋮⋮</span>
      <button
        class="uc-toggle"
        type="button"
        :aria-expanded="!collapsed"
        :title="collapsed ? 'Expand Upgrade Center' : 'Collapse Upgrade Center'"
        @click="collapsed = !collapsed"
      >
        <span class="uc-chevron" :class="{ open: !collapsed }">▾</span>
        <span class="uc-title">Upgrade Center</span>
      </button>
    </header>

    <div v-if="!collapsed" class="uc-body">
      <div v-if="upgrades.length === 0" class="uc-empty">
        No upgrades available.
      </div>
      <template v-else>
        <div
          v-for="upgrade in upgrades"
          :key="upgrade.track"
          class="uc-row"
        >
          <div class="uc-row__header">
            <img
              :src="unitPortrait(upgrade.track)"
              :alt="upgrade.displayName"
              class="uc-row__portrait"
            />
            <div class="uc-row__title">
              <span class="uc-row__name">{{ upgrade.displayName }}</span>
              <span class="uc-row__level">Lv {{ upgrade.level }} / {{ upgrade.cap }}</span>
            </div>
          </div>

          <!-- Stat preview strip for the next level purchase -->
          <div class="uc-row__preview">
            <span class="uc-stat uc-stat--hp">+{{ upgrade.hpPerLevel }} HP</span>
            <span class="uc-stat uc-stat--dmg">+{{ upgrade.damagePerLevel }} DMG</span>
            <span v-if="upgrade.armorPerLevel !== 0" class="uc-stat uc-stat--arm">+{{ upgrade.armorPerLevel }} ARM</span>
            <span class="uc-stat uc-stat--as">+{{ upgrade.attackSpeedPerLevel.toFixed(2) }} AS</span>
            <span class="uc-stat uc-stat--ms">+{{ upgrade.moveSpeedPerLevel }} MS</span>
          </div>

          <button
            type="button"
            class="uc-row__btn"
            :disabled="isUpgradeDisabled(upgrade)"
            :title="upgradeDisabledReason(upgrade)"
            @click="onPurchase(upgrade.track)"
          >
            Upgrade {{ upgrade.displayName }} &mdash; {{ upgrade.nextCostGold }}g
          </button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { PlayerUpgradeSnapshot } from '@/game/network/protocol'
import { useDraggablePanel } from '@/composables/useDraggablePanel'

const unitPortraitModules = import.meta.glob(
  '@/assets/units/*/rotations/south.png',
  { eager: true, import: 'default' }
) as Record<string, string>

function unitPortrait(track: string): string {
  const key = Object.keys(unitPortraitModules).find(k => k.includes(`/units/${track}/`))
  return key ? unitPortraitModules[key] : ''
}

const props = defineProps<{
  show: boolean
  upgrades: PlayerUpgradeSnapshot[]
  onPurchase: (track: string) => void
}>()

const collapsed = ref(false)
const drag = useDraggablePanel('upgrade-center')

// Returns true when the upgrade button should be disabled.
function isUpgradeDisabled(upgrade: PlayerUpgradeSnapshot): boolean {
  if (!upgrade.hasUpgradeCenter) return true
  if (upgrade.cap === 0) return true
  if (upgrade.level >= upgrade.cap) return true
  if (!upgrade.canAfford) return true
  return false
}

// Returns a tooltip explaining why the button is disabled, or empty string
// when the button is enabled.
function upgradeDisabledReason(upgrade: PlayerUpgradeSnapshot): string {
  if (!upgrade.hasUpgradeCenter) return 'Build an Upgrade Center first'
  if (upgrade.cap === 0) return 'Town Hall required'
  if (upgrade.level >= upgrade.cap) return 'Requires a higher tier Town Hall'
  if (!upgrade.canAfford) return 'Not enough gold'
  return ''
}
</script>

<style scoped>
.upgrade-center {
  position: absolute;
  top: 80px;
  right: 16px;
  z-index: 25;
  min-width: 280px;
  max-width: 340px;
  max-height: 480px;
  display: flex;
  flex-direction: column-reverse;
  border-radius: 14px;
  border: 1px solid rgba(200, 164, 106, 0.32);
  background:
    radial-gradient(circle at top, rgba(196, 140, 62, 0.14), transparent 45%),
    linear-gradient(180deg, rgba(46, 29, 16, 0.96), rgba(22, 14, 9, 0.96));
  box-shadow:
    inset 0 1px 0 rgba(246, 225, 183, 0.1),
    0 10px 24px rgba(0, 0, 0, 0.4);
  font-family: inherit;
  color: #f5ead2;
}

.upgrade-center.collapsed {
  max-height: none;
}

.uc-head {
  border-top: 1px solid rgba(200, 164, 106, 0.2);
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  cursor: grab;
  user-select: none;
  touch-action: none;
}

.uc-head--dragging {
  cursor: grabbing;
}

.upgrade-center.dragging {
  opacity: 0.92;
}

.uc-grip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 18px;
  color: rgba(200, 164, 106, 0.6);
  font-size: 12px;
  letter-spacing: -2px;
  line-height: 1;
  transform: rotate(90deg);
}

.uc-toggle {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 6px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  text-align: left;
}

.uc-chevron {
  display: inline-block;
  font-size: 12px;
  color: #d7bb84;
  transition: transform 120ms ease;
}

.uc-chevron.open {
  transform: rotate(0deg);
}

.uc-chevron:not(.open) {
  transform: rotate(-90deg);
}

.uc-title {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #f0d88e;
}

.uc-body {
  padding: 8px 10px 10px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  scrollbar-width: thin;
  scrollbar-color: rgba(210, 176, 113, 0.35) transparent;
}

.uc-body::-webkit-scrollbar {
  width: 5px;
}

.uc-body::-webkit-scrollbar-thumb {
  background: rgba(210, 176, 113, 0.35);
  border-radius: 3px;
}

.uc-empty {
  font-size: 12px;
  color: #a8946e;
  text-align: center;
  padding: 8px 0;
}

.uc-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 7px 9px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.16);
  background: rgba(20, 12, 7, 0.5);
}

.uc-row__header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.uc-row__portrait {
  width: 40px;
  height: 40px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.35);
  background: rgba(10, 6, 3, 0.7);
  object-fit: contain;
  flex-shrink: 0;
  image-rendering: pixelated;
}

.uc-row__title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}

.uc-row__name {
  font-size: 13px;
  font-weight: 700;
  color: #f5ead2;
}

.uc-row__level {
  font-size: 11px;
  font-weight: 600;
  color: #d4b87a;
  letter-spacing: 0.04em;
  font-variant-numeric: tabular-nums;
}

/* Stat preview strip */
.uc-row__preview {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 8px;
  margin: 2px 0;
}

.uc-stat {
  font-size: 11px;
  font-weight: 600;
  padding: 1px 5px;
  border-radius: 4px;
  background: rgba(40, 24, 10, 0.7);
  border: 1px solid rgba(200, 164, 106, 0.2);
}

.uc-stat--hp  { color: #7dd87a; }
.uc-stat--dmg { color: #f0a07a; }
.uc-stat--arm { color: #7ab8e0; }
.uc-stat--as  { color: #e0d07a; }
.uc-stat--ms  { color: #c07ae0; }

/* Purchase button */
.uc-row__btn {
  width: 100%;
  padding: 5px 10px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.35);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.85), rgba(61, 39, 22, 0.95));
  color: #f5ead2;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.03em;
  cursor: pointer;
  transition: background 0.12s, border-color 0.12s;
  text-align: center;
}

.uc-row__btn:not(:disabled):hover {
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.95), rgba(83, 53, 28, 1));
  border-color: rgba(220, 180, 110, 0.6);
}

.uc-row__btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
</style>
