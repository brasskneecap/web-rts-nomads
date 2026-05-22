<template>
  <section v-if="abilities.length > 0" class="commander-bar" aria-label="Commander abilities">
    <button
      v-for="ability in abilities"
      :key="ability.id"
      type="button"
      class="ability-slot"
      :class="{
        'is-active': activeAbilityId === ability.id,
        'is-cooling': cooldownRatio(ability) > 0,
        'is-disabled': cooldownRatio(ability) > 0,
      }"
      :disabled="cooldownRatio(ability) > 0"
      :title="ability.displayName ?? ability.id"
      @click="onSlotClick(ability.id, cooldownRatio(ability) > 0)"
    >
      <span class="slot-label">{{ ability.displayName ?? ability.id }}</span>

      <div
        v-if="cooldownRatio(ability) > 0"
        class="cooldown-overlay"
        aria-hidden="true"
        :style="{ height: `${Math.round(cooldownRatio(ability) * 100)}%` }"
      ></div>

      <div
        v-if="cooldownRemaining(ability) > 0"
        class="cooldown-label"
        aria-hidden="true"
      >{{ Math.ceil(cooldownRemaining(ability)) }}</div>
    </button>
  </section>
</template>

<script setup lang="ts">
import type { CommanderAbilitySnapshot } from '@/game/network/protocol'

const props = defineProps<{
  abilities: CommanderAbilitySnapshot[]
  activeAbilityId: string | null
}>()

const emit = defineEmits<{
  cast: [abilityId: string]
}>()

function cooldownRemaining(ability: CommanderAbilitySnapshot): number {
  return ability.cooldownRemaining ?? 0
}

function cooldownRatio(ability: CommanderAbilitySnapshot): number {
  const total = ability.cooldownTotal ?? 0
  const remaining = cooldownRemaining(ability)
  if (total <= 0 || remaining <= 0) return 0
  const ratio = remaining / total
  if (ratio <= 0) return 0
  if (ratio >= 1) return 1
  return ratio
}

function onSlotClick(abilityId: string, onCooldown: boolean) {
  if (onCooldown) return
  emit('cast', abilityId)
}
// keep an unused reference to props so the linter doesn't complain about
// the prop being read-only via the template.
void props
</script>

<style scoped>
.commander-bar {
  position: absolute;
  /* Sit just above the SelectionHud footer (200px tall, anchored bottom:0)
     with a small breathing gap so the bar reads as a separate panel. */
  bottom: 212px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 25;
  display: flex;
  gap: 10px;
  padding: 8px 12px;
  background: rgba(8, 10, 16, 0.78);
  border: 1px solid rgba(220, 180, 110, 0.35);
  border-radius: 10px;
  box-shadow:
    inset 0 1px 0 rgba(255, 240, 200, 0.08),
    0 12px 26px rgba(0, 0, 0, 0.45);
  pointer-events: auto;
  backdrop-filter: blur(6px);
}

.ability-slot {
  position: relative;
  width: 64px;
  height: 64px;
  padding: 0;
  border-radius: 8px;
  border: 1px solid rgba(220, 180, 110, 0.4);
  background: linear-gradient(180deg, rgba(95, 65, 30, 0.9), rgba(40, 25, 12, 0.95));
  color: #f7d88e;
  font-weight: 700;
  letter-spacing: 0.06em;
  cursor: pointer;
  overflow: hidden;
  transition: border-color 0.12s, transform 0.08s;
}

.ability-slot:hover:not(.is-disabled) {
  border-color: rgba(255, 220, 140, 0.85);
  transform: translateY(-1px);
}

.ability-slot.is-active {
  border-color: #ffe28a;
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 18px rgba(255, 200, 80, 0.45);
}

.ability-slot.is-disabled {
  cursor: not-allowed;
  color: #b39a6b;
  border-color: rgba(160, 130, 80, 0.35);
}

.slot-label {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  text-transform: uppercase;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.8);
}

.cooldown-overlay {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.55);
  pointer-events: none;
  transition: height 0.15s linear;
}

.cooldown-label {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 20px;
  font-weight: 800;
  color: #fff2d6;
  text-shadow: 0 2px 4px rgba(0, 0, 0, 0.9);
  pointer-events: none;
}
</style>
