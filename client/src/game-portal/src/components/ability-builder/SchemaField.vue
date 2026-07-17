<template>
  <!-- boolean renders as a bare `.ed-check` label (matching the convention in
       AbilityEditorPanel.vue) — wrapping it in EditorField would print the
       label twice (once as EditorField's own label, once as the checkbox's
       trailing text). Every other control gets the normal EditorField frame. -->
  <label v-if="field.control === 'boolean'" class="ed-check" :for="fieldId">
    <input :id="fieldId" type="checkbox" :checked="!!modelValue" @change="commitBoolean" />
    {{ field.label }}
  </label>

  <EditorField v-else :label="field.label" :hint="hint" :for-id="fieldId">
    <!-- number -->
    <input
      v-if="field.control === 'number'"
      :id="fieldId"
      type="number"
      :value="localText"
      @input="onLocalInput"
      @change="commitNumber"
    />

    <!-- duration: a number of SECONDS with a trailing "s" affordance. -->
    <div v-else-if="field.control === 'duration'" class="sf-suffixed">
      <input :id="fieldId" type="number" step="0.1" :value="localText" @input="onLocalInput" @change="commitNumber" />
      <span class="sf-suffix">s</span>
    </div>

    <!-- percentage: kept as a raw 0..1 number (matches slowMultiplier's
         existing model in abilityEditorForm.ts) — NOT converted to 0..100. -->
    <input
      v-else-if="field.control === 'percentage'"
      :id="fieldId"
      type="number"
      min="0"
      max="1"
      step="0.05"
      :value="localText"
      @input="onLocalInput"
      @change="commitNumber"
    />

    <!-- text -->
    <input
      v-else-if="field.control === 'text'"
      :id="fieldId"
      type="text"
      :value="localText"
      @input="onLocalInput"
      @change="commitText"
    />

    <!-- enum: FilterableSelect over resolved options; falls back to a plain
         text input when no option source can be resolved (see
         resolveOptionList's doc comment for the resolution order). -->
    <FilterableSelect
      v-else-if="field.control === 'enum' && optionList"
      :model-value="typeof modelValue === 'string' ? modelValue : ''"
      :options="optionFilterList"
      :aria-label="field.label"
      @update:model-value="commitEnumLike"
    />
    <input
      v-else-if="field.control === 'enum'"
      :id="fieldId"
      type="text"
      :value="localText"
      @input="onLocalInput"
      @change="commitText"
    />

    <!-- multiselect: a checkbox group over the same resolved option source
         enum uses. No options resolvable -> an explanatory note, not a crash. -->
    <div v-else-if="field.control === 'multiselect'" class="sf-checkgroup">
      <template v-if="optionList">
        <label v-for="opt in optionList" :key="opt" class="ed-check">
          <input
            type="checkbox"
            :checked="multiselectValue.has(opt)"
            @change="toggleMultiselect(opt, ($event.target as HTMLInputElement).checked)"
          />
          {{ opt }}
        </label>
      </template>
      <p v-else class="sf-note">No known options for this field.</p>
    </div>

    <!-- asset: FilterableSelect over catalogs.projectiles (key === 'projectile')
         or catalogs.effects (every other asset field — presentation refs,
         effectAtPoint-style fields, etc.). -->
    <FilterableSelect
      v-else-if="field.control === 'asset'"
      :model-value="typeof modelValue === 'string' ? modelValue : ''"
      :options="assetOptions"
      :aria-label="field.label"
      @update:model-value="commitEnumLike"
    />

    <!-- sentinel_number: the match-attack-range pattern from
         AbilityEditorPanel.vue's castRangeMatchesAttack — a checkbox that
         swaps the sentinel string in/out, plus a number input while unchecked. -->
    <div v-else-if="field.control === 'sentinel_number'" class="sf-sentinel">
      <label class="ed-check">
        <input type="checkbox" :checked="matchesAttackRange" @change="toggleSentinel" />
        Matches attack range
      </label>
      <input
        v-if="!matchesAttackRange"
        type="number"
        aria-label="Value"
        :value="localText"
        @input="onLocalInput"
        @change="commitNumber"
      />
    </div>

    <!-- context_ref: a select over a curated set of common context keys.
         TODO(phase-6): scope this to the keys actually available at the
         selected trigger/action's position (named outputs of prior actions,
         trigger-specific context) instead of this fixed curated list. -->
    <select v-else-if="field.control === 'context_ref'" :id="fieldId" :value="contextRefKey" @change="commitContextRef">
      <option value="">(none)</option>
      <option v-for="k in CONTEXT_REF_KEYS" :key="k" :value="k">{{ k }}</option>
    </select>

    <!-- target_query: delegate to TargetQueryEditor, forwarding its merged
         TargetQueryDef straight up as this field's value. `field.targetQueryFields`
         (declared by the server per-action, e.g. launch_projectile's 10 vs.
         nothing) picks which sub-fields actually render — see
         TargetQueryEditor's own doc comment. -->
    <TargetQueryEditor
      v-else-if="field.control === 'target_query'"
      :model-value="modelValue as TargetQueryDef | undefined"
      :enums="enums"
      :fields="field.targetQueryFields"
      @update:model-value="(v) => emit('update:modelValue', v)"
    />

    <!-- animation_marker: text input for now. TODO(phase-6): replace with a
         marker picker sourced from the ability's authored animation timeline. -->
    <div v-else-if="field.control === 'animation_marker'" class="sf-stack">
      <input :id="fieldId" type="text" :value="localText" @input="onLocalInput" @change="commitText" />
      <p class="sf-note">TODO(phase-6): pick from the animation timeline instead of free text.</p>
    </div>

    <!-- nested_triggers: read-only for now — conditional/repeat/create_zone's
         nested flows are edited in the Flow view's nested-trigger display, not
         here. TODO(phase-6): inline nested-trigger editing in the inspector. -->
    <p v-else-if="field.control === 'nested_triggers'" class="sf-note">
      Nested flow — edit in the flow view. TODO(phase-6): inline editing here.
    </p>

    <!-- Unknown control: text fallback so a newer server's control type never
         crashes the editor. -->
    <input v-else :id="fieldId" type="text" :value="localText" @input="onLocalInput" @change="commitText" />
  </EditorField>
</template>

<script setup lang="ts">
// SchemaField renders ONE inspector control for a server-described
// SchemaField descriptor + a bound value. It is the schema-driven leaf the
// whole Inspector payoff rests on: adding a new action config field on the
// server (ability_exec_*.go) needs NO client change here, as long as its
// `control` is one of the known ControlType values (an unrecognized control
// still renders — as a text fallback — rather than crashing).
//
// COMMIT-ON-CHANGE / UNDO DISCIPLINE: every text/number-ish control keeps a
// local editable string (`localText`) that mirrors `modelValue` on mount and
// on every EXTERNAL change (see the watch below — this is what keeps the
// field in sync across ability switches, undo/redo, and another control
// editing the same underlying object). The value is only pushed back out via
// `update:modelValue` on the native `change` event (fires on blur or Enter),
// never on `input` — each builder op snapshots for undo, so committing per
// keystroke would flood the undo stack with one entry per character typed.
// Discrete controls (checkbox, select/FilterableSelect, sentinel-range
// toggle) have no such keystroke stream, so they commit immediately.
import { computed, ref, watch } from 'vue'
import type { SchemaField as SchemaFieldDescriptor } from '@/game/abilities/program/programSchema'
import type { TargetQueryDef } from '@/game/abilities/program/abilityProgram'
import EditorField from '@/components/editor/EditorField.vue'
import FilterableSelect, { type FilterableOption } from '@/components/editor/FilterableSelect.vue'
import TargetQueryEditor from './TargetQueryEditor.vue'
import type { AbilityBuilderCatalogs } from './useAbilityBuilder'

const props = defineProps<{
  field: SchemaFieldDescriptor
  modelValue: unknown
  /** Program enums bundle (relations, targetOrderings, ...) for enum/multiselect resolution. */
  enums: Record<string, string[]>
  /** Display catalogs (effects, projectiles, damageTypes, ...) for enum/asset resolution. */
  catalogs: AbilityBuilderCatalogs
}>()

const emit = defineEmits<{ 'update:modelValue': [value: unknown] }>()

let uidCounter = 0
const uid = uidCounter++
const fieldId = computed(() => `sf-${props.field.key}-${uid}`)

const hint = computed(() => (props.field.control === 'percentage' ? '(0–1)' : undefined))

const CONTEXT_REF_KEYS = [
  'caster',
  'initialTarget',
  'castPoint',
  'impactPosition',
  'zoneCenter',
  'previous_action_targets',
]

// ── text/number-ish controls: local editable copy, committed on change ────
const localText = ref(toDisplayString(props.modelValue))

function toDisplayString(v: unknown): string {
  if (v === undefined || v === null) return ''
  return String(v)
}

watch(
  () => props.modelValue,
  (v) => {
    localText.value = toDisplayString(v)
  },
)

function onLocalInput(e: Event) {
  localText.value = (e.target as HTMLInputElement).value
}

function commitText() {
  emit('update:modelValue', localText.value)
}

function commitNumber() {
  const n = Number(localText.value)
  emit('update:modelValue', Number.isFinite(n) ? n : 0)
}

function commitBoolean(e: Event) {
  emit('update:modelValue', (e.target as HTMLInputElement).checked)
}

function commitEnumLike(value: string) {
  emit('update:modelValue', value)
}

// ── enum / multiselect option resolution ───────────────────────────────────
// resolveOptionList: (1) explicit field.options wins outright; (2) a small
// hand-picked set of common field-key aliases maps onto the server's
// ProgramEnums bundle (relations/ordering/source/origin/anchor) or a display
// catalog (type|school|damageType -> damageTypes, category -> categories,
// unitType -> unitTypes, autoCastTargetSelector -> autoCastSelectors);
// (3) a same-named key in the enums bundle (forward-compat for a future field
// literally named after its enum); (4) null -> caller falls back to text.
function resolveOptionList(
  field: SchemaFieldDescriptor,
  enums: Record<string, string[]>,
  catalogs: AbilityBuilderCatalogs,
): string[] | null {
  if (field.options && field.options.length > 0) return field.options
  const key = field.key.toLowerCase()
  if (key === 'relations') return enums.relations ?? null
  if (key === 'ordering') return enums.targetOrderings ?? null
  if (key === 'source') return enums.targetSources ?? null
  if (key === 'origin') return enums.targetOrigins ?? null
  if (key === 'anchor') return enums.zoneAnchors ?? null
  if (key === 'type' || key === 'school' || key === 'damagetype') {
    return catalogs.damageTypes.length > 0 ? catalogs.damageTypes : null
  }
  if (key === 'category') return catalogs.categories.length > 0 ? catalogs.categories : null
  if (key === 'unittype') return catalogs.unitTypes.length > 0 ? catalogs.unitTypes : null
  if (key === 'autocasttargetselector') return catalogs.autoCastSelectors.length > 0 ? catalogs.autoCastSelectors : null
  if (enums[field.key]) return enums[field.key]
  return null
}

const optionList = computed(() => resolveOptionList(props.field, props.enums, props.catalogs))

const optionFilterList = computed<FilterableOption[]>(() =>
  (optionList.value ?? []).map((v) => ({ id: v, label: v === '' ? '(none)' : v })),
)

const multiselectValue = computed<Set<string>>(
  () => new Set(Array.isArray(props.modelValue) ? (props.modelValue as unknown[]).map(String) : []),
)

function toggleMultiselect(opt: string, checked: boolean) {
  const next = new Set(multiselectValue.value)
  if (checked) next.add(opt)
  else next.delete(opt)
  emit('update:modelValue', [...next])
}

// ── asset ────────────────────────────────────────────────────────────────
// Heuristic: a field literally named "projectile" resolves against the
// projectile catalog; every other asset field (presentation refs, etc.)
// resolves against the effect catalog.
const assetOptions = computed<FilterableOption[]>(() => {
  const list = props.field.key.toLowerCase() === 'projectile' ? props.catalogs.projectiles : props.catalogs.effects
  return list.map((v) => ({ id: v, label: v }))
})

// ── sentinel_number ─────────────────────────────────────────────────────
const matchesAttackRange = computed(() => props.modelValue === 'match_attack_range')

function toggleSentinel(e: Event) {
  const checked = (e.target as HTMLInputElement).checked
  emit('update:modelValue', checked ? 'match_attack_range' : 0)
}

// ── context_ref ──────────────────────────────────────────────────────────
const contextRefKey = computed(() => {
  const v = props.modelValue
  if (v && typeof v === 'object' && 'key' in (v as Record<string, unknown>)) {
    return String((v as { key: unknown }).key ?? '')
  }
  return ''
})

function commitContextRef(e: Event) {
  const key = (e.target as HTMLSelectElement).value
  emit('update:modelValue', key ? { key } : undefined)
}
</script>

<style scoped>
.sf-suffixed {
  display: flex;
  align-items: center;
  gap: 6px;
}

.sf-suffixed input {
  flex: 1 1 auto;
  min-width: 0;
}

.sf-suffix {
  flex: 0 0 auto;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.sf-checkgroup {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
}

.sf-sentinel {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.sf-stack {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.sf-note {
  margin: 0;
  font-size: 0.72rem;
  font-style: italic;
  color: var(--ed-text-dim);
}
</style>
