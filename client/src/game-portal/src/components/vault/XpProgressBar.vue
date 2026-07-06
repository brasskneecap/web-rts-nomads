<template>
  <div class="xp">
    <template v-if="isMaxRank">
      <span class="xp__label">XP: Max Rank</span>
    </template>
    <template v-else-if="xpToNext && xpToNext > 0">
      <div class="xp__track">
        <div
          class="xp__fill"
          :style="{ width: `${fraction * 100}%`, background: rankColor }"
        />
      </div>
      <span class="xp__label">XP: {{ xpInto ?? 0 }} / {{ bandTotal }}</span>
    </template>
    <template v-else>
      <span class="xp__label">XP: —</span>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  xpInto: number | null
  xpToNext: number | null
  isMaxRank?: boolean
  rankColor?: string
}>(), {
  isMaxRank: false,
  rankColor: '#fbbf24',
})

// xpToNext is the XP REMAINING to the next rank, so the full rank band is
// (into + remaining). Dividing by xpToNext alone made the bar hit 100% at the
// band midpoint (into == remaining). Mirrors GameState.ts getUnitXpLabel.
const bandTotal = computed(() => (props.xpInto ?? 0) + (props.xpToNext ?? 0))

const fraction = computed(() => {
  const into = props.xpInto ?? 0
  const total = bandTotal.value
  if (total <= 0) return 0
  return Math.max(0, Math.min(1, into / total))
})
</script>

<style scoped>
.xp {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.xp__track {
  width: 100%;
  height: 6px;
  border-radius: 3px;
  background: rgba(0, 0, 0, 0.45);
  border: 1px solid rgba(212, 168, 79, 0.25);
  overflow: hidden;
}

.xp__fill {
  height: 100%;
  border-radius: 3px;
  transition: width 0.2s ease;
}

.xp__label {
  font-size: 11px;
  color: rgba(232, 217, 184, 0.85);
  letter-spacing: 0.02em;
}
</style>
