<template>
  <div class="wave-upgrade-overlay" role="dialog" aria-modal="true" aria-label="Wave upgrade">
    <!-- Waiting state — shown after the local player has picked -->
    <div v-if="resolved" class="upgrade-waiting">
      <div class="upgrade-waiting-title">Upgrade chosen!</div>
      <p class="upgrade-waiting-sub">Waiting for other players…</p>
    </div>

    <!-- Active state — offer cards -->
    <div v-else class="upgrade-panel">
      <div class="upgrade-header">
        <span class="upgrade-wave-label">Wave {{ upgrade.wave }} — Choose an Upgrade</span>
        <!-- Timer bar -->
        <div class="upgrade-timer-track" aria-label="Time remaining">
          <div
            class="upgrade-timer-fill"
            :class="timerClass"
            :style="{ width: timerPercent + '%' }"
          ></div>
        </div>
      </div>

      <!-- Unit picker (XP grant secondary step) -->
      <div v-if="pendingXpOffer" class="unit-picker">
        <div class="unit-picker-title">Choose a unit to receive {{ pendingXpOffer.description }}</div>
        <ul class="unit-picker-list">
          <li
            v-for="unit in units"
            :key="unit.id"
            class="unit-picker-item"
            tabindex="0"
            @click="pickXpTarget(unit.id)"
            @keydown.enter="pickXpTarget(unit.id)"
          >
            <span class="unit-name">{{ unit.name }}</span>
            <span class="unit-xp">XP {{ unit.xp ?? 0 }} / {{ unit.xpToNextRank ?? 0 }}</span>
          </li>
        </ul>
      </div>

      <!-- Card row -->
      <div v-else class="upgrade-cards">
        <button
          v-for="offer in upgrade.offers"
          :key="offer.id"
          class="upgrade-card"
          :class="`rarity-${offer.rarity}`"
          @click="selectOffer(offer)"
        >
          <span class="card-rarity">{{ offer.rarity }}</span>
          <span class="card-name">{{ offer.name }}</span>
          <span class="card-desc">{{ offer.description }}</span>
          <span v-if="offer.stackMax > 0" class="card-stack">Stack {{ offer.stackCurrent }} / {{ offer.stackMax }}</span>
        </button>
      </div>

      <!-- Reroll button -->
      <div v-if="!pendingXpOffer" class="upgrade-footer">
        <button
          class="reroll-button"
          :disabled="upgrade.rerollsLeft <= 0"
          @click="reroll"
        >
          ↺ Reroll ({{ upgrade.rerollsLeft }} left)
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import type { WaveUpgradeOfferSnapshot, UpgradeOffer } from '@/game/network/protocol'
import type { Unit } from '@/game/core/GameState'

const props = defineProps<{
  upgrade: WaveUpgradeOfferSnapshot
  units: Unit[]
  sendChoice: (upgradeID: string, targetUnitID?: number) => void
  sendReroll: () => void
}>()

const resolved = ref(false)
const pendingXpOffer = ref<UpgradeOffer | null>(null)
const now = ref(Date.now())

let rafId = 0
function tick() {
  now.value = Date.now()
  rafId = requestAnimationFrame(tick)
}
onMounted(() => { rafId = requestAnimationFrame(tick) })
onUnmounted(() => cancelAnimationFrame(rafId))

const timerPercent = computed(() => {
  const remaining = Math.max(0, props.upgrade.deadlineMs - now.value)
  return Math.min(100, (remaining / 25_000) * 100)
})

const timerClass = computed(() => {
  if (timerPercent.value > 40) return 'timer-green'
  if (timerPercent.value > 15) return 'timer-yellow'
  return 'timer-red'
})

function selectOffer(offer: UpgradeOffer) {
  if (offer.requiresTargetUnit) {
    pendingXpOffer.value = offer
    return
  }
  props.sendChoice(offer.id)
  resolved.value = true
}

function pickXpTarget(unitId: number) {
  if (!pendingXpOffer.value) return
  props.sendChoice(pendingXpOffer.value.id, unitId)
  pendingXpOffer.value = null
  resolved.value = true
}

function reroll() {
  if (props.upgrade.rerollsLeft <= 0) return
  props.sendReroll()
}
</script>

<style scoped>
.wave-upgrade-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.72);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
}

.upgrade-waiting {
  text-align: center;
  color: #e2e8f0;
}
.upgrade-waiting-title { font-size: 1.5rem; font-weight: bold; margin-bottom: 8px; }
.upgrade-waiting-sub { color: #94a3b8; }

.upgrade-panel {
  background: #0d1117;
  border: 1px solid #1e293b;
  border-radius: 12px;
  padding: 24px;
  width: min(860px, 94vw);
}

.upgrade-header {
  margin-bottom: 20px;
}
.upgrade-wave-label {
  display: block;
  text-align: center;
  color: #94a3b8;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  font-size: 0.75rem;
  margin-bottom: 10px;
}
.upgrade-timer-track {
  height: 5px;
  background: #1e293b;
  border-radius: 3px;
  overflow: hidden;
}
.upgrade-timer-fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.25s linear, background 0.5s;
}
.timer-green  { background: #4ade80; }
.timer-yellow { background: #fbbf24; }
.timer-red    { background: #ef4444; }

.upgrade-cards {
  display: flex;
  gap: 12px;
}

.upgrade-card {
  flex: 1;
  background: #0f172a;
  border: 2px solid #334155;
  border-radius: 10px;
  padding: 16px;
  text-align: center;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: transform 0.1s, box-shadow 0.1s;
  color: #e2e8f0;
}
.upgrade-card:hover {
  transform: translateY(-2px);
}

.rarity-common    { border-color: #64748b; }
.rarity-rare      { border-color: #6366f1; box-shadow: 0 0 14px rgba(99,102,241,0.25); }
.rarity-epic      { border-color: #f59e0b; box-shadow: 0 0 14px rgba(245,158,11,0.25); }
.rarity-legendary { border-color: #ef4444; box-shadow: 0 0 18px rgba(239,68,68,0.35); }

.card-rarity {
  font-size: 0.65rem;
  text-transform: uppercase;
  letter-spacing: 0.1em;
}
.rarity-common    .card-rarity { color: #94a3b8; }
.rarity-rare      .card-rarity { color: #818cf8; }
.rarity-epic      .card-rarity { color: #fbbf24; }
.rarity-legendary .card-rarity { color: #f87171; }

.card-name  { font-weight: bold; font-size: 1rem; }
.card-desc  { font-size: 0.78rem; color: #64748b; line-height: 1.4; }
.card-stack { font-size: 0.68rem; color: #475569; margin-top: 4px; }

.upgrade-footer {
  margin-top: 16px;
  text-align: center;
}
.reroll-button {
  background: #1e293b;
  border: 1px solid #334155;
  border-radius: 6px;
  color: #94a3b8;
  padding: 6px 18px;
  font-size: 0.82rem;
  cursor: pointer;
}
.reroll-button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.reroll-button:not(:disabled):hover {
  background: #273548;
}

.unit-picker-title {
  color: #e2e8f0;
  margin-bottom: 12px;
  text-align: center;
}
.unit-picker-list {
  list-style: none;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 300px;
  overflow-y: auto;
}
.unit-picker-item {
  background: #0f172a;
  border: 1px solid #334155;
  border-radius: 6px;
  padding: 10px 14px;
  display: flex;
  justify-content: space-between;
  cursor: pointer;
  color: #e2e8f0;
}
.unit-picker-item:hover { background: #1e293b; }
.unit-name { font-weight: 500; }
.unit-xp   { color: #64748b; font-size: 0.78rem; }
</style>
