<template>
  <div class="urow">
    <img :src="portraitUrl" :alt="upgrade.displayName" class="urow__portrait" draggable="false" />
    <div class="urow__main">
      <div class="urow__head">
        <span class="urow__name">{{ upgrade.displayName }}</span>
        <span class="urow__level">
          Lv {{ upgrade.level }}<template v-if="queuedCount > 0"> (+{{ queuedCount }})</template> / {{ upgrade.cap }}
        </span>
      </div>

      <!-- Per-level stats, 3 per row. -->
      <div class="urow__stats">
        <span v-for="stat in statsList" :key="stat">{{ stat }}</span>
      </div>

      <!-- Next-level cost, shown horizontally below the stats with resource
           icons (gold / wood) instead of letters. -->
      <div v-if="canQueueMore" class="urow__cost">
        <span v-if="upgrade.nextCostGold > 0" class="urow__cost-item">
          <img v-if="goldIcon" :src="goldIcon" class="urow__cost-icon" alt="gold" draggable="false" />
          <span>{{ upgrade.nextCostGold }}</span>
        </span>
        <span v-if="upgrade.nextCostWood > 0" class="urow__cost-item">
          <img v-if="woodIcon" :src="woodIcon" class="urow__cost-icon" alt="wood" draggable="false" />
          <span>{{ upgrade.nextCostWood }}</span>
        </span>
      </div>

      <button
        v-if="canQueueMore"
        type="button"
        class="urow__btn"
        :disabled="!upgrade.canStart"
        :title="disabledReason"
        @click="onPurchase(upgrade.track)"
      >
        {{ buttonLabel }}
      </button>
      <div v-else class="urow__maxed">Max level reached</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { PlayerUpgradeSnapshot } from '@/game/network/protocol'
import { getUnitPortraitUrl } from '@/game/rendering/unitSprites'
import { getResourceIconUrl } from '@/game/rendering/resourceSprites'

const goldIcon = getResourceIconUrl('gold')
const woodIcon = getResourceIconUrl('wood')

const props = defineProps<{
  upgrade: PlayerUpgradeSnapshot
  onPurchase: (track: string) => void
}>()

const portraitUrl = computed(() => getUnitPortraitUrl(undefined, props.upgrade.track) ?? '')
const queuedCount = computed(() => props.upgrade.queuedCount ?? 0)
const projectedLevel = computed(() => props.upgrade.level + queuedCount.value)
const canQueueMore = computed(() => projectedLevel.value < props.upgrade.cap)

// Per-level stat increases.
const statsList = computed(() => {
  const u = props.upgrade
  const parts = [`+${u.hpPerLevel} HP`, `+${u.damagePerLevel} DMG`]
  if (u.armorPerLevel !== 0) parts.push(`+${u.armorPerLevel} ARM`)
  parts.push(`+${u.attackSpeedPerLevel.toFixed(2)} AS`)
  parts.push(`+${u.moveSpeedPerLevel} MS`)
  return parts
})

const buttonLabel = computed(() =>
  queuedCount.value > 0 ? `Queue Lv ${projectedLevel.value + 1}` : `Upgrade ${props.upgrade.displayName}`,
)

const disabledReason = computed(() => {
  const u = props.upgrade
  if (u.canStart) return ''
  if (!u.hasBlacksmith) return 'Build a Blacksmith first'
  if (u.cap === 0) return 'Town Hall required'
  if (projectedLevel.value >= u.cap) return 'Requires a higher tier Town Hall'
  if (!u.canAfford) return 'Not enough gold or wood'
  return ''
})
</script>

<style scoped>
.urow {
  display: flex;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.16);
  background: rgba(20, 12, 7, 0.5);
}

.urow__portrait {
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.35);
  background: rgba(10, 6, 3, 0.7);
  object-fit: contain;
  image-rendering: pixelated;
}

.urow__main {
  display: flex;
  flex-direction: column;
  gap: 3px;
  flex: 1;
  min-width: 0;
}

.urow__head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.urow__name {
  font-size: 13px;
  font-weight: 700;
  color: #f5ead2;
}

.urow__level {
  font-size: 11px;
  font-weight: 600;
  color: #d4b87a;
  letter-spacing: 0.04em;
  font-variant-numeric: tabular-nums;
}

.urow__stats {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 1px 8px;
  font-size: 11px;
  line-height: 1.35;
  color: #d4b87a;
}

/* Next-level cost row: resource icon + amount, laid out horizontally. */
.urow__cost {
  display: flex;
  align-items: center;
  gap: 12px;
}

.urow__cost-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  font-weight: 700;
  color: #f0e0c0;
}

.urow__cost-icon {
  width: 14px;
  height: 14px;
  object-fit: contain;
  image-rendering: pixelated;
}

.urow__btn {
  width: 100%;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.35);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.85), rgba(61, 39, 22, 0.95));
  color: #f5ead2;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.03em;
  text-align: center;
  transition: background 0.12s, border-color 0.12s;
}

.urow__btn:not(:disabled):hover {
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.95), rgba(83, 53, 28, 1));
  border-color: rgba(220, 180, 110, 0.6);
}

.urow__btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.urow__maxed {
  padding: 6px 8px;
  text-align: center;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
  color: rgba(240, 216, 142, 0.7);
}
</style>
