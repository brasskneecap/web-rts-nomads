<template>
  <div class="oip" data-test="overhead-icon-picker">
    <div class="oip__grid" role="listbox" :aria-label="ariaLabel">
      <button
        type="button"
        class="oip__item"
        :class="{ 'oip__item--sel': !modelValue }"
        role="option"
        :aria-selected="!modelValue"
        title="(none)"
        data-test="overhead-icon-option-none"
        @click="emit('update:modelValue', '')"
      >
        <span class="oip__none">&times;</span>
      </button>

      <button
        v-for="id in options"
        :key="id"
        type="button"
        class="oip__item"
        :class="[kindClass(id), { 'oip__item--sel': modelValue === id }]"
        role="option"
        :aria-selected="modelValue === id"
        :title="id"
        :data-test="`overhead-icon-option-${id}`"
        @click="emit('update:modelValue', id)"
      >
        <svg
          v-if="pathFor(id)"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
          class="oip__svg"
        >
          <path :d="pathFor(id)!" />
        </svg>
        <!-- ACTION_ICON_MAP not loaded (e.g. a test that skips load()) — fall
             back to the raw id as a label rather than rendering nothing or
             crashing on a missing path. -->
        <span v-else class="oip__fallback">{{ id }}</span>
      </button>

      <p v-if="options.length === 0" class="oip__empty">No overhead icons published yet.</p>
    </div>

    <p v-if="modelValue" class="oip__selected">
      <code>{{ modelValue }}</code>
      <span v-if="kindFor(modelValue)" class="oip__kind">({{ kindFor(modelValue) }})</span>
    </p>
  </div>
</template>

<script setup lang="ts">
// OverheadIconPicker: a flat, always-visible grid of the candidate overhead
// buff/debuff icons (apply_mark's `icon` field), each rendered as its ACTUAL
// art rather than a bare id string — matching the SVG convention
// ACTION_ICON_MAP paths are authored for (0..24 viewBox, stroke-only,
// currentColor) exactly as ActionIcon.vue's own fallback branch and
// CanvasRenderer's drawUnitActiveBuffs/drawUnitActiveDebuffs render them, so
// what you pick here is what shows over the unit in-game. Deliberately
// inline (not a modal, unlike AbilityIconPicker) — the candidate set is a
// short, flat list of debuff-*/buff-* ids, small enough that a scrollable
// grid right in the inspector is simpler than a second dialog.
//
// This component is presentation + selection only: it does not know about
// iconKind. The caller (InspectorBar's commitApplyMarkIcon) derives iconKind
// from the chosen id's prefix via iconKindForId and commits both keys in one
// action-config patch.
import { computed } from 'vue'
import { ACTION_ICON_MAP, iconKindForId } from '@/game/maps/actionIconDefs'

const props = defineProps<{
  /** Candidate icon ids — sourced from schema.enums.icon (server-published). */
  options: string[]
  /** Currently configured icon id, or empty/undefined for "none". */
  modelValue?: string
  ariaLabel?: string
}>()

const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

const ariaLabel = computed(() => props.ariaLabel ?? 'Overhead Icon')

function pathFor(id: string): string | undefined {
  return ACTION_ICON_MAP.get(id)
}

function kindFor(id: string): 'buff' | 'debuff' | undefined {
  return iconKindForId(id)
}

// Presentational tint only (amber for buff, red for debuff) — mirrors the
// pill/stroke coloring CanvasRenderer uses in-game so the picker reads the
// same way the overhead icon will once it's on a unit.
function kindClass(id: string): string {
  const kind = iconKindForId(id)
  return kind ? `oip__item--${kind}` : ''
}
</script>

<style scoped>
.oip {
  display: flex;
  flex-direction: column;
  gap: 6px;
  width: 100%;
}

.oip__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(40px, 1fr));
  gap: 6px;
  max-height: 168px;
  overflow-y: auto;
  padding: 2px;
}

.oip__item {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  padding: 4px;
  background: rgba(8, 14, 24, 0.4);
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  color: var(--ed-text-dim);
}

.oip__item:hover {
  border-color: var(--ed-line-strong);
}

.oip__item--sel {
  border-color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.14);
}

.oip__item--buff {
  color: #fde68a;
}

.oip__item--debuff {
  color: #fecaca;
}

.oip__svg {
  width: 100%;
  height: 100%;
}

.oip__none {
  font-size: 1rem;
  line-height: 1;
}

.oip__fallback {
  font-size: 0.5rem;
  line-height: 1.1;
  text-align: center;
  overflow: hidden;
  word-break: break-word;
}

.oip__empty {
  grid-column: 1 / -1;
  margin: 0;
  font-size: 0.76rem;
  font-style: italic;
  color: var(--ed-text-dim);
}

.oip__selected {
  margin: 0;
  font-size: 0.74rem;
  color: var(--ed-text-dim);
}

.oip__kind {
  margin-left: 4px;
  text-transform: capitalize;
}
</style>
