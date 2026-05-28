<template>
  <div class="loadout-picker">
    <!-- Top strip: read-only summary of currently-active upgrades. Empty
         when nothing is active. Hover for tooltip details. -->
    <div v-if="ownedEntries.length > 0" class="loadout-picker__active">
      <div class="loadout-picker__section-label">Active Loadout</div>
      <div v-if="activeEntries.length === 0" class="loadout-picker__active-empty">
        No upgrades active — toggle one below.
      </div>
      <div v-else class="loadout-picker__strip" role="list" aria-label="Active upgrades">
        <div
          v-for="entry in activeEntries"
          :key="entry.def.id"
          class="upgrade-chip"
          role="listitem"
        >
          <div class="upgrade-chip__icon-wrap">
            <BuffIcon :icon-key="entry.def.id" :label="entry.def.name" />
            <span class="upgrade-chip__rank">{{ entry.rank }}</span>
          </div>
          <div class="upgrade-chip__tooltip" role="tooltip">
            <div class="upgrade-chip__tooltip-title">{{ entry.def.name }}</div>
            <div class="upgrade-chip__tooltip-body">{{ entry.def.description }}</div>
            <div class="upgrade-chip__tooltip-rank">
              Rank {{ entry.rank }} / {{ entry.def.maxRanks }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Bottom list: every owned upgrade with an active/inactive toggle. -->
    <div class="loadout-picker__list-section">
      <div class="loadout-picker__section-label">Purchased Upgrades</div>

      <div v-if="error" class="loadout-picker__error" role="alert">{{ error }}</div>

      <div v-if="ownedEntries.length === 0 && !error" class="loadout-picker__empty">
        <p class="loadout-picker__empty-text">
          You haven't purchased any upgrades yet.
          <button
            type="button"
            class="loadout-picker__empty-link"
            @click="emit('switch-tab', 'upgrades')"
          >
            Visit the Upgrades tab
          </button>
          to buy your first one.
        </p>
      </div>

      <div
        v-for="entry in ownedEntries"
        :key="entry.def.id"
        class="upgrade-card"
        :aria-label="`${entry.def.name} upgrade`"
      >
        <div class="upgrade-card__header">
          <span class="upgrade-card__name">{{ entry.def.name }}</span>
          <span class="upgrade-card__rank">Rank {{ entry.rank }} / {{ entry.def.maxRanks }}</span>
        </div>

        <div class="upgrade-card__desc">{{ entry.def.description }}</div>

        <div class="upgrade-card__footer">
          <button
            type="button"
            class="upgrade-card__toggle"
            :class="{ 'upgrade-card__toggle--active': isActive(entry.def.id) }"
            :disabled="isBusy"
            :aria-pressed="isActive(entry.def.id)"
            :aria-label="`${isActive(entry.def.id) ? 'Deactivate' : 'Activate'} ${entry.def.name}`"
            @click="onToggle(entry.def.id)"
          >
            {{ isActive(entry.def.id) ? 'Active' : 'Inactive' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import type { ProfileUpgradeDef } from '@/types/profile'
import { useProfileUpgrades } from '@/composables/useProfileUpgrades'
import BuffIcon from './BuffIcon.vue'

const emit = defineEmits<{
  'switch-tab': [tab: string]
}>()

const { catalog, ownedRanks, isBusy, error, initialize, isActive, toggle } = useProfileUpgrades()

onMounted(() => { void initialize() })

type OwnedEntry = { def: ProfileUpgradeDef; rank: number }

// All purchased upgrades in catalog order — drives the bottom list.
const ownedEntries = computed<OwnedEntry[]>(() => {
  const result: OwnedEntry[] = []
  for (const def of catalog.value) {
    const rank = ownedRanks.value[def.id] ?? 0
    if (rank > 0) {
      result.push({ def, rank })
    }
  }
  return result
})

// Only the currently-active subset — drives the read-only top strip.
const activeEntries = computed<OwnedEntry[]>(() =>
  ownedEntries.value.filter((entry) => isActive(entry.def.id)),
)

function onToggle(upgradeId: string): void {
  void toggle(upgradeId, !isActive(upgradeId))
}
</script>

<style scoped>
.loadout-picker {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.loadout-picker__active {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.loadout-picker__list-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.loadout-picker__section-label {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.14em;
  text-transform: uppercase;
  color: #d7bb84;
}

.loadout-picker__active-empty {
  font-size: 12px;
  color: #8a7a5a;
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px dashed rgba(160, 130, 70, 0.3);
  background: rgba(20, 13, 6, 0.4);
}

.loadout-picker__strip {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.22);
  background: rgba(20, 13, 6, 0.55);
}

.loadout-picker__error {
  font-size: 12px;
  color: #f07070;
  padding: 8px 12px;
  border-radius: 6px;
  border: 1px solid rgba(240, 112, 112, 0.25);
  background: rgba(240, 112, 112, 0.08);
}

.loadout-picker__empty {
  padding: 24px 0;
  text-align: center;
}

.loadout-picker__empty-text {
  font-size: 13px;
  color: #a09070;
  line-height: 1.55;
  margin: 0;
}

.loadout-picker__empty-link {
  background: none;
  border: none;
  padding: 0;
  color: #d7bb84;
  font-size: 13px;
  font-weight: 700;
  font-family: inherit;
  cursor: pointer;
  text-decoration: underline;
  text-underline-offset: 2px;
}

.loadout-picker__empty-link:hover {
  color: #f7d88e;
}

.loadout-picker__empty-link:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
  border-radius: 2px;
}

/* Active-strip chips — small, gold-bordered, with rank badge + tooltip. */
.upgrade-chip {
  position: relative;
  display: inline-block;
}

.upgrade-chip__icon-wrap {
  position: relative;
  border: 1px solid rgba(247, 216, 142, 0.55);
  border-radius: 8px;
}

.upgrade-chip__rank {
  position: absolute;
  bottom: -4px;
  right: -4px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(90, 60, 28, 0.95), rgba(50, 32, 14, 0.95));
  border: 1px solid rgba(247, 216, 142, 0.6);
  color: #f7d88e;
  font-size: 10px;
  font-weight: 700;
  line-height: 14px;
  text-align: center;
  pointer-events: none;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.4);
}

.upgrade-chip__tooltip {
  position: absolute;
  top: calc(100% + 8px);
  left: 50%;
  transform: translateX(-50%);
  min-width: 200px;
  max-width: 280px;
  padding: 8px 12px;
  border-radius: 8px;
  background: linear-gradient(180deg, rgba(34, 22, 10, 0.98), rgba(20, 12, 4, 0.98));
  border: 1px solid rgba(200, 164, 106, 0.45);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.5);
  color: #f5ead2;
  text-align: left;
  opacity: 0;
  visibility: hidden;
  transition: opacity 0.12s ease-out;
  pointer-events: none;
  z-index: 30;
}

.upgrade-chip:hover .upgrade-chip__tooltip,
.upgrade-chip:focus-within .upgrade-chip__tooltip {
  opacity: 1;
  visibility: visible;
}

.upgrade-chip__tooltip-title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
}

.upgrade-chip__tooltip-body {
  font-size: 12px;
  color: #d7bb84;
  line-height: 1.45;
  margin-bottom: 4px;
}

.upgrade-chip__tooltip-rank {
  font-size: 11px;
  color: #8a7a5a;
}

/* Purchased-upgrades list cards (toggle UI). */
.upgrade-card {
  padding: 14px 16px;
  border-radius: 10px;
  border: 1px solid rgba(200, 164, 106, 0.2);
  background: linear-gradient(180deg, rgba(20, 13, 6, 0.9), rgba(10, 7, 3, 0.95));
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.upgrade-card__header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.upgrade-card__name {
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  color: #d7bb84;
}

.upgrade-card__rank {
  font-size: 11px;
  font-weight: 700;
  color: #f7d88e;
  white-space: nowrap;
}

.upgrade-card__desc {
  font-size: 12px;
  color: #8a7a5a;
  line-height: 1.45;
}

.upgrade-card__footer {
  display: flex;
  justify-content: flex-end;
  margin-top: 2px;
}

.upgrade-card__toggle {
  padding: 7px 20px;
  border-radius: 999px;
  border: 1px solid rgba(200, 164, 106, 0.3);
  background: linear-gradient(180deg, rgba(50, 40, 20, 0.9), rgba(28, 22, 10, 0.95));
  color: #a09070;
  font-family: inherit;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  cursor: pointer;
  transition: background 0.1s, border-color 0.1s, color 0.1s, opacity 0.1s;
  white-space: nowrap;
}

.upgrade-card__toggle:hover:not(:disabled) {
  filter: brightness(1.18);
  border-color: rgba(220, 180, 100, 0.55);
}

.upgrade-card__toggle:active:not(:disabled) {
  filter: brightness(0.88);
}

.upgrade-card__toggle--active {
  background: linear-gradient(180deg, rgba(90, 60, 28, 0.9), rgba(50, 32, 14, 0.95));
  border-color: rgba(247, 216, 142, 0.5);
  color: #f7d88e;
}

.upgrade-card__toggle:disabled {
  opacity: 0.38;
  cursor: not-allowed;
  filter: none;
}

.upgrade-card__toggle:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
}
</style>
