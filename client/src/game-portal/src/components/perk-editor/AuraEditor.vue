<template>
  <div class="aura-editor" data-test="aura-editor">
    <div class="aura-editor__fields">
      <label>
        Radius
        <input v-model.number="radiusProxy" type="number" step="8" aria-label="Aura radius" />
      </label>
      <label>
        Targets
        <select v-model="targetsProxy" aria-label="Aura targets">
          <option value="allies">Allies</option>
          <option value="enemies">Enemies</option>
        </select>
      </label>
      <label class="aura-editor__checkbox-label">
        <input type="checkbox" v-model="includeSelfProxy" aria-label="Aura include self" />
        Include Self
      </label>
      <label>
        Per Additional Source
        <input v-model.number="perAdditionalSourceProxy" type="number" step="0.01" placeholder="0" aria-label="Aura per additional source" />
      </label>
    </div>
    <p class="aura-editor__hint-line">
      Per Additional Source is the EXTRA bonus each covering source beyond the first adds, on top
      of the strongest single source's value. Leave blank for none.
    </p>

    <div class="aura-editor__ring-color">
      <label class="aura-editor__checkbox-label">
        <input
          type="checkbox"
          :checked="ringColorEnabled"
          aria-label="Override aura ring color"
          @change="onToggleRingColor(($event.target as HTMLInputElement).checked)"
        />
        Override Ring Color
      </label>
      <label v-if="ringColorEnabled">
        Ring Color
        <input
          type="color"
          :value="modelValue.ringColor || DEFAULT_RING_COLOR"
          aria-label="Aura ring color"
          @input="patch({ ringColor: ($event.target as HTMLInputElement).value })"
        />
      </label>
    </div>
    <p class="aura-editor__hint-line">
      HUD-only: changes the color of this aura's radius ring so it can be told apart from other
      auras on the same unit. Has no effect on gameplay. Unchecked = ring uses the owning
      player's color.
    </p>

    <p v-if="modelValue.statRows.length === 0" class="aura-editor__hint-line">No stat contributions.</p>
    <div v-for="(row, idx) in modelValue.statRows" :key="idx" class="aura-editor__stat-row">
      <label>
        Stat
        <select
          :value="row.stat"
          :aria-label="`Aura Stat ${idx + 1} stat`"
          @change="updateStatRow(idx, { ...row, stat: ($event.target as HTMLSelectElement).value })"
        >
          <option v-for="d in statDefs" :key="d.id" :value="d.id">{{ d.label }}</option>
        </select>
      </label>
      <label>
        Value
        <input
          type="number"
          step="0.05"
          :value="row.value"
          :aria-label="`Aura Stat ${idx + 1} value`"
          @input="updateStatRow(idx, { ...row, value: numOrBlank(($event.target as HTMLInputElement).value) })"
        />
      </label>
      <button type="button" class="aura-editor__row-del" title="Remove" @click="removeStatRow(idx)">✕</button>
    </div>
    <button type="button" class="aura-editor__row-add" @click="addStatRow">+ Add Stat Contribution</button>
    <p class="aura-editor__hint-line">
      Aura contributions are additive only — there is no Multiply op for auras.
    </p>
  </div>
</template>

<script setup lang="ts">
// AuraEditor authors ONE PerkAura (see @/game/perks/perkEditorForm.ts) — a
// continuous area effect the perk's owner emits to nearby units. Extracted
// out of PerkEditorPanel.vue for the same reason RiderEditor.vue was: an
// aura is a nested-list-in-list (a list of auras, each with its own list of
// stat contributions), which is enough authoring surface to want its own
// component rather than inlining a second level of v-for into the panel.
//
// AuraRow/AuraStatRow are UI-draft shapes, not the wire shape (PerkAura) —
// numeric fields are `number | ''` because v-model.number leaves a cleared
// input as '' rather than undefined, same reasoning as StatModifierRow /
// AbilityModifierRow in PerkEditorPanel.vue. The parent owns converting
// AuraRow[] <-> PerkAura[] (rowsFromAuras / aurasFromRows), same
// rows-array-plus-deep-watch idiom as every other section in that file —
// this component only ever edits the row shape it's handed.
//
// `op` and `stage` are deliberately NOT rendered anywhere in this component:
// the server's aura fold site only consumes `op: "add"` with `stage` empty/
// "base" and REJECTS anything else, so offering those controls here would
// let a designer author a def the server refuses to save. The parent always
// emits `op: "add"` with `stage` omitted for every aura stat row.
import { computed } from 'vue'
import type { StatDef } from '@/game/stats/statRegistry'

export interface AuraStatRow {
  stat: string
  value: number | ''
}

export interface AuraRow {
  radius: number | ''
  targets: 'allies' | 'enemies'
  includeSelf: boolean
  perAdditionalSource: number | ''
  statRows: AuraStatRow[]
  // ringColor: '' means "no override" (ring uses the owning player's color,
  // unchanged from before this field existed). A non-empty value is a CSS
  // hex color authored via the native color picker below. Mirrors the
  // '' == unset convention every other optional numeric field in this row
  // uses (see the module doc above) — the parent's aurasFromRows only ever
  // writes PerkAura.ringColor when this is non-empty.
  ringColor: string
}

const props = defineProps<{
  modelValue: AuraRow
  statDefs: StatDef[]
}>()
const emit = defineEmits<{ 'update:modelValue': [value: AuraRow] }>()

function patch(p: Partial<AuraRow>) {
  emit('update:modelValue', { ...props.modelValue, ...p })
}

function computedProxy<K extends keyof AuraRow>(key: K) {
  return {
    get: () => props.modelValue[key],
    set: (v: AuraRow[K]) => patch({ [key]: v } as Partial<AuraRow>),
  }
}

const radiusProxy = computed(computedProxy('radius'))
const targetsProxy = computed(computedProxy('targets'))
const includeSelfProxy = computed(computedProxy('includeSelf'))
const perAdditionalSourceProxy = computed(computedProxy('perAdditionalSource'))

// Ring color: a native <input type="color"> can't express "no value" — it
// always resolves to SOME color. "Unset" (ring falls back to the owning
// player's color) is represented by ringColor === '' on the row, and
// controlled by this separate checkbox rather than trying to overload the
// color input itself. Checking the box seeds a visible default so the
// picker doesn't silently start on black; unchecking clears back to ''.
const DEFAULT_RING_COLOR = '#fef08a'
const ringColorEnabled = computed(() => props.modelValue.ringColor !== '')
function onToggleRingColor(enabled: boolean) {
  patch({ ringColor: enabled ? (props.modelValue.ringColor || DEFAULT_RING_COLOR) : '' })
}

function numOrBlank(raw: string): number | '' {
  if (raw === '') return ''
  const n = Number(raw)
  return Number.isNaN(n) ? '' : n
}

function addStatRow() {
  patch({ statRows: [...props.modelValue.statRows, { stat: props.statDefs[0]?.id ?? '', value: 0 }] })
}
function removeStatRow(idx: number) {
  const next = props.modelValue.statRows.slice()
  next.splice(idx, 1)
  patch({ statRows: next })
}
function updateStatRow(idx: number, row: AuraStatRow) {
  const next = props.modelValue.statRows.slice()
  next[idx] = row
  patch({ statRows: next })
}
</script>

<style scoped>
.aura-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1 1 auto;
  min-width: 0;
  padding: 8px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.35);
}

.aura-editor__fields {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.aura-editor__fields label {
  display: grid;
  gap: 4px;
  flex: 1 1 140px;
  min-width: 120px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.aura-editor__fields input,
.aura-editor__fields select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  font-family: inherit;
}

.aura-editor__checkbox-label {
  flex-direction: row !important;
  align-items: center;
  gap: 6px !important;
}

.aura-editor__ring-color {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
}

.aura-editor__ring-color label:not(.aura-editor__checkbox-label) {
  display: grid;
  gap: 4px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.aura-editor__ring-color input[type='color'] {
  width: 48px;
  height: 30px;
  padding: 2px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
}

.aura-editor__stat-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 10px;
  padding: 8px;
  border: 1px solid rgba(148, 163, 184, 0.16);
  border-radius: 10px;
  background: rgba(8, 14, 24, 0.4);
}

.aura-editor__stat-row label {
  display: grid;
  gap: 4px;
  min-width: 120px;
  color: rgba(226, 232, 240, 0.86);
  font-size: 0.75rem;
}

.aura-editor__stat-row input,
.aura-editor__stat-row select {
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 10px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 7px 9px;
  font-size: 0.78rem;
  font-family: inherit;
}

.aura-editor__row-del {
  flex: 0 0 auto;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 6px;
  background: rgba(15, 23, 42, 0.6);
  color: #f8fafc;
  padding: 4px 8px;
  font-size: 0.72rem;
}

.aura-editor__row-add {
  align-self: flex-start;
  border: 1px solid rgba(215, 187, 132, 0.5);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #d7bb84;
  padding: 6px 10px;
  font-size: 0.76rem;
  font-weight: 700;
}

.aura-editor__hint-line {
  margin: 0;
  color: rgba(226, 232, 240, 0.55);
  font-size: 0.72rem;
  font-style: italic;
}
</style>
