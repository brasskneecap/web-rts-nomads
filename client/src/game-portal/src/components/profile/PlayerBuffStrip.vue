<template>
  <div v-if="visibleBuffs.length > 0" class="buff-strip" aria-label="Active player buffs">
    <div
      v-for="buff in visibleBuffs"
      :key="buff.id"
      class="buff-strip__item"
    >
      <BuffIcon :icon-key="buff.iconKey" :label="buff.displayName" />
      <div class="buff-tooltip" role="tooltip">
        <div class="buff-tooltip__title">
          {{ buff.displayName }}
          <span v-if="isDebuff(buff)" class="buff-tooltip__tag">Debuff</span>
        </div>
        <div v-if="buff.description" class="buff-tooltip__body">{{ buff.description }}</div>
        <div v-if="modifierLines(buff).length > 0" class="buff-tooltip__stat-preview">
          <div
            v-for="line in modifierLines(buff)"
            :key="line"
            class="buff-tooltip__stat-row"
          >{{ line }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useProfile } from '@/composables/useProfile'
import BuffIcon from './BuffIcon.vue'
import type { PlayerBuffDef, PlayerBuffModifiers } from '@/types/profile'

const { equippedBuffs } = useProfile()

// The backend will add activeBuffs: ActiveEffectIcon[] to PlayerSnapshot.
// Until that field exists, we display the profile's equipped buffs as a
// static representation of what's active in-match.
// TODO: wire to PlayerSnapshot.activeBuffs once the backend ships it — resolve
// each ActiveEffectIcon.id against buffCatalog for display name + description.
const visibleBuffs = computed<PlayerBuffDef[]>(() => equippedBuffs.value)

function isDebuff(buff: PlayerBuffDef): boolean {
  return buff.appliesTo === 'enemyUnits'
}

// Format a signed integer with a leading + when positive.
function formatFlat(n: number): string {
  return n >= 0 ? `+${n}` : `${n}`
}

// Format a multiplier (0.05 → "+5%", -0.10 → "-10%").
function formatPercent(n: number): string {
  const pct = Math.round(n * 100)
  return pct >= 0 ? `+${pct}%` : `${pct}%`
}

// Build a human-readable line for each non-zero modifier. Empty array when
// the buff has no modifier data (description-only buff).
function modifierLines(buff: PlayerBuffDef): string[] {
  const m: PlayerBuffModifiers = buff.modifiers ?? {}
  const lines: string[] = []
  if (m.hpBonus) lines.push(`HP ${formatFlat(m.hpBonus)}`)
  if (m.damageBonus) lines.push(`Damage ${formatFlat(m.damageBonus)}`)
  if (m.armorBonus) lines.push(`Armor ${formatFlat(m.armorBonus)}`)
  if (m.attackSpeedBonus) lines.push(`Attack Speed ${formatPercent(m.attackSpeedBonus)}`)
  if (m.moveSpeedMultBonus) lines.push(`Move Speed ${formatPercent(m.moveSpeedMultBonus)}`)
  if (m.bonusDamageMult) lines.push(`Damage ${formatPercent(m.bonusDamageMult)}`)
  return lines
}
</script>

<style scoped>
.buff-strip {
  display: flex;
  gap: 4px;
  align-items: center;
}

.buff-strip__item {
  position: relative;
  cursor: default;
}

/* Shared visual language with .perk-tooltip / .action-tooltip / .stat-tooltip
   in SelectionHud. Differences: anchored BELOW the icon (the buff strip lives
   near the top of the viewport, so a top-anchored tooltip would clip) and
   right-aligned (the strip sits on the right edge of the screen, so a centered
   tooltip on the rightmost icon would overflow off-screen). */
.buff-tooltip {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  min-width: 180px;
  max-width: 260px;
  padding: 7px 10px;
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

.buff-strip__item:hover .buff-tooltip {
  opacity: 1;
  visibility: visible;
}

.buff-tooltip__title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
  line-height: 1.5;
}

/* Small pill next to the name when appliesTo === 'enemyUnits' — same tan tone
   as the body text so it reads as a tag rather than a primary element. */
.buff-tooltip__tag {
  padding: 1px 6px;
  border-radius: 999px;
  border: 1px solid rgba(200, 100, 80, 0.6);
  background: rgba(80, 24, 18, 0.7);
  color: #f3c5b4;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.buff-tooltip__body {
  font-size: 12px;
  line-height: 1.5;
  color: #d4b87a;
}

.buff-tooltip__stat-preview {
  margin-top: 5px;
  padding-top: 5px;
  border-top: 1px solid rgba(200, 164, 106, 0.22);
}

.buff-tooltip__stat-row {
  font-size: 11px;
  line-height: 1.5;
  color: #d4b87a;
  letter-spacing: 0.02em;
}
</style>
