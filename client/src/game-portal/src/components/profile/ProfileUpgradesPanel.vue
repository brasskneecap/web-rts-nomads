<template>
  <div class="upgrades-panel">
    <section class="upgrades-panel__balance" aria-label="Dominion Points balance">
      <div class="upgrades-panel__balance-label">Dominion Points</div>
      <div class="upgrades-panel__balance-value">{{ dominionPoints.toLocaleString() }}</div>
    </section>

    <div v-if="error" class="upgrades-panel__error" role="alert">
      {{ error }}
    </div>

    <div v-if="catalog.length === 0 && !error" class="upgrades-panel__empty">
      Loading upgrades...
    </div>

    <div
      v-for="def in catalog"
      :key="def.id"
      class="upgrade-card"
      :aria-label="`${def.name} upgrade`"
    >
      <div class="upgrade-card__header">
        <span class="upgrade-card__name">{{ def.name }}</span>
        <span class="upgrade-card__rank">
          Rank {{ currentRank(def.id) }} / {{ def.maxRanks }}
        </span>
      </div>

      <div class="upgrade-card__desc">{{ def.description }}</div>

      <div class="upgrade-card__footer">
        <div class="upgrade-card__cost">
          <template v-if="currentRank(def.id) < def.maxRanks">
            Next rank: <span class="upgrade-card__cost-value">{{ def.costPerRank[currentRank(def.id)] }} DP</span>
          </template>
          <template v-else>
            <span class="upgrade-card__maxed">Maxed</span>
          </template>
        </div>

        <div class="upgrade-card__actions">
          <button
            class="upgrade-card__btn upgrade-card__btn--buy"
            type="button"
            :disabled="isBuyDisabled(def)"
            :aria-label="`Buy rank ${currentRank(def.id) + 1} of ${def.name}`"
            @click="onBuy(def.id)"
          >
            Buy
          </button>

          <button
            class="upgrade-card__btn upgrade-card__btn--refund"
            type="button"
            :disabled="isRefundDisabled(def.id)"
            :aria-label="`Refund rank ${currentRank(def.id)} of ${def.name}${currentRank(def.id) > 0 ? ` for ${def.costPerRank[currentRank(def.id) - 1]} DP` : ''}`"
            @click="onRefund(def.id)"
          >
            Refund{{ currentRank(def.id) > 0 ? ` (${def.costPerRank[currentRank(def.id) - 1]} DP)` : '' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import type { ProfileUpgradeDef } from '@/types/profile'
import { useProfileUpgrades } from '@/composables/useProfileUpgrades'

const { catalog, ownedRanks, dominionPoints, isBusy, error, initialize, purchase, refund } =
  useProfileUpgrades()

onMounted(() => { void initialize() })

function currentRank(id: string): number {
  return ownedRanks.value[id] ?? 0
}

function isBuyDisabled(def: ProfileUpgradeDef): boolean {
  const rank = currentRank(def.id)
  if (rank >= def.maxRanks) return true
  if (dominionPoints.value < def.costPerRank[rank]) return true
  return isBusy.value
}

function isRefundDisabled(id: string): boolean {
  if (currentRank(id) === 0) return true
  return isBusy.value
}

function onBuy(id: string): void {
  void purchase(id)
}

function onRefund(id: string): void {
  void refund(id)
}
</script>

<style scoped>
.upgrades-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.upgrades-panel__balance {
  padding: 14px 16px;
  border-radius: 10px;
  border: 1px solid rgba(200, 164, 106, 0.2);
  background: linear-gradient(180deg, rgba(20, 13, 6, 0.9), rgba(10, 7, 3, 0.95));
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.upgrades-panel__balance-label {
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #d7bb84;
}

.upgrades-panel__balance-value {
  font-size: 28px;
  font-weight: 700;
  color: #f7d88e;
  line-height: 1;
}

.upgrades-panel__error {
  font-size: 12px;
  color: #f07070;
  padding: 8px 12px;
  border-radius: 6px;
  border: 1px solid rgba(240, 112, 112, 0.25);
  background: rgba(240, 112, 112, 0.08);
}

.upgrades-panel__empty {
  color: #a09070;
  font-size: 14px;
  padding: 24px 0;
  text-align: center;
}

.upgrade-card {
  padding: 16px;
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
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 4px;
  flex-wrap: wrap;
}

.upgrade-card__cost {
  font-size: 12px;
  color: #a09070;
}

.upgrade-card__cost-value {
  font-weight: 700;
  color: #f7d88e;
}

.upgrade-card__maxed {
  font-size: 12px;
  font-weight: 700;
  color: #d7bb84;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.upgrade-card__actions {
  display: flex;
  gap: 8px;
}

.upgrade-card__btn {
  padding: 7px 14px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.3);
  background: linear-gradient(180deg, rgba(90, 60, 28, 0.9), rgba(50, 32, 14, 0.95));
  color: #f5ead2;
  font-family: inherit;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.04em;
  cursor: pointer;
  transition: filter 0.1s, border-color 0.1s, opacity 0.1s;
  white-space: nowrap;
}

.upgrade-card__btn:hover:not(:disabled) {
  filter: brightness(1.18);
  border-color: rgba(220, 180, 100, 0.55);
}

.upgrade-card__btn:active:not(:disabled) {
  filter: brightness(0.88);
}

.upgrade-card__btn:disabled {
  opacity: 0.38;
  cursor: not-allowed;
  filter: none;
}

.upgrade-card__btn:focus-visible {
  outline: 2px solid rgba(247, 216, 142, 0.9);
  outline-offset: 2px;
  border-radius: 4px;
}

.upgrade-card__btn--refund {
  background: linear-gradient(180deg, rgba(50, 40, 20, 0.9), rgba(28, 22, 10, 0.95));
  border-color: rgba(160, 130, 70, 0.25);
}
</style>
