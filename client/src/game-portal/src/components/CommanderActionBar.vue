<template>
  <section
    v-if="abilities.length > 0"
    class="commander-bar"
    :class="{ 'commander-bar--embedded': embedded }"
    :style="{ '--ui-icon-container-image': `url(${iconContainerUrl})` }"
    aria-label="Commander abilities"
  >
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
      :aria-label="`${ability.displayName ?? ability.id} — ${descriptionFor(ability)}`"
      @click="onSlotClick(ability.id, cooldownRatio(ability) > 0)"
    >
      <img
        v-if="iconForAbility(ability)"
        :src="iconForAbility(ability)!"
        :alt="ability.displayName ?? ability.id"
        class="ability-slot__icon"
        draggable="false"
      />
      <span v-else class="slot-label">{{ ability.displayName ?? ability.id }}</span>

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

      <div class="ability-tooltip" role="tooltip">
        <div class="ability-tooltip__title">{{ ability.displayName ?? ability.id }}</div>
        <div v-if="descriptionFor(ability)" class="ability-tooltip__desc">
          {{ descriptionFor(ability) }}
        </div>
        <div v-if="(ability.damage ?? 0) > 0" class="ability-tooltip__stat">
          Damage: <kbd>{{ ability.damage }}</kbd>
        </div>
        <div v-if="(ability.heal ?? 0) > 0" class="ability-tooltip__stat">
          Healing: <kbd>{{ ability.heal }}</kbd>
        </div>
        <div v-if="(ability.cooldownTotal ?? 0) > 0" class="ability-tooltip__stat">
          Cooldown: <kbd>{{ ability.cooldownTotal }}s</kbd>
        </div>
      </div>
    </button>
  </section>
</template>

<script setup lang="ts">
import type { CommanderAbilitySnapshot } from '@/game/network/protocol'
import iconContainerUrl from '@/assets/ui/themes/default/icon-container.png'

// Eagerly resolve all PNGs in `assets/ui/abilities/`. The key is the file
// stem (e.g. `blessing.png` -> `blessing`); ability ids are matched after
// stripping the `commander_` prefix so `commander_blessing` -> `blessing`.
const abilityIconGlob = import.meta.glob<string>(
  '../assets/ui/abilities/*.png',
  { eager: true, query: '?url', import: 'default' },
)
const abilityIcons = new Map<string, string>()
for (const [path, url] of Object.entries(abilityIconGlob)) {
  const m = path.match(/\/assets\/ui\/abilities\/([^/]+)\.png$/)
  if (m) abilityIcons.set(m[1].toLowerCase(), url)
}

function iconForAbility(ability: CommanderAbilitySnapshot): string | null {
  const key = ability.id.replace(/^commander_/, '').toLowerCase()
  return abilityIcons.get(key) ?? null
}

// Human-readable descriptions for the hover tooltip. Keyed by the ability id
// with the `commander_` prefix stripped. Add an entry here when a new
// commander ability is introduced.
const ABILITY_DESCRIPTIONS: Record<string, string> = {
  blessing: 'Heals friendly units in a small area at the target location.',
  smite: 'Deals damage to enemy units in a small area at the target location.',
}

function descriptionFor(ability: CommanderAbilitySnapshot): string {
  const key = ability.id.replace(/^commander_/, '').toLowerCase()
  return ABILITY_DESCRIPTIONS[key] ?? ''
}

const props = withDefaults(defineProps<{
  abilities: CommanderAbilitySnapshot[]
  activeAbilityId: string | null
  /** When true: drop the floating panel chrome (position, background,
   *  border, padding, blur) so the host container places and frames the
   *  bar. Ability-slot styling and cooldown rendering are unchanged. */
  embedded?: boolean
}>(), {
  embedded: false,
})

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
  /* Sit just above the MatchMenuLauncher strip (bottom: 168px, height: 40px →
     top edge at 208px) with a small breathing gap so the bar reads as a
     separate panel above the launcher row. */
  bottom: 220px;
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

/* Embedded inside another container (e.g. MatchMenuLauncher's action row):
   drop the floating panel so the host container handles framing/positioning.
   Slot sizing and cooldown styling are unchanged. */
.commander-bar--embedded {
  position: static;
  left: auto;
  transform: none;
  z-index: auto;
  padding: 0;
  background: none;
  border: 0;
  border-radius: 0;
  box-shadow: none;
  backdrop-filter: none;
  /* Match the gap of the host action row so ability spacing reads the
     same as the launcher pill spacing on either side. */
  gap: 8px;
}

.ability-slot {
  position: relative;
  width: 70px;
  height: 70px;
  padding: 0;
  border: 0;
  border-radius: 0;
  background: var(--ui-icon-container-image) center / 100% 100% no-repeat;
  image-rendering: pixelated;
  color: #f7d88e;
  font-weight: 700;
  letter-spacing: 0.06em;
  cursor: pointer;
  transition: filter 0.12s;
}

/* Lift the hovered/focused slot so its tooltip (and the filter-induced
   stacking context) sits above neighboring slots and the surrounding HUD. */
.ability-slot:hover,
.ability-slot:focus-visible {
  z-index: 2;
}

.ability-slot:hover:not(.is-disabled) {
  filter: brightness(1.1);
}

.ability-slot.is-active {
  box-shadow:
    inset 0 0 0 2px rgba(255, 226, 138, 0.7),
    0 0 18px rgba(255, 200, 80, 0.45);
  filter: brightness(1.15);
}

.ability-slot.is-disabled {
  cursor: not-allowed;
  color: #b39a6b;
  filter: brightness(0.7) saturate(0.6);
}

/* Action art rendered inside the icon-container frame at 70% so the
   container's outer edge stays visible — same idiom as inventory slots. */
.ability-slot__icon {
  position: absolute;
  top: 50%;
  left: 50%;
  width: 70%;
  height: 70%;
  transform: translate(-50%, -50%);
  object-fit: contain;
  image-rendering: pixelated;
  pointer-events: none;
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

/* Hover tooltip — mirrors .menu-launcher__tooltip so the visual language
   stays consistent across the in-match action bar and the launcher row. */
.ability-tooltip {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%);
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
  z-index: 10;
  letter-spacing: normal;
}

.ability-slot:hover .ability-tooltip,
.ability-slot:focus-visible .ability-tooltip {
  opacity: 1;
  visibility: visible;
}

.ability-tooltip__title {
  font-size: 13px;
  font-weight: 700;
  color: #fff2d6;
  margin-bottom: 4px;
  line-height: 1.5;
  letter-spacing: 0.02em;
}

.ability-tooltip__desc {
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.5;
  font-weight: 400;
}

.ability-tooltip__stat {
  margin-top: 5px;
  padding-top: 5px;
  border-top: 1px solid rgba(200, 164, 106, 0.22);
  font-size: 11px;
  color: #d4b87a;
  line-height: 1.5;
  font-weight: 400;
}

.ability-tooltip__stat kbd {
  display: inline-block;
  padding: 1px 6px;
  margin-left: 4px;
  border-radius: 4px;
  background: rgba(20, 12, 4, 0.6);
  border: 1px solid rgba(200, 164, 106, 0.35);
  color: #ffe9a0;
  font-family: inherit;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.04em;
}
</style>
