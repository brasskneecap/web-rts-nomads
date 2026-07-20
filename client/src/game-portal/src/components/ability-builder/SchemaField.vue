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
    <!-- number: inside a loop (variableCapable), a Number/Variable mode
         selector swaps a literal number input for a dropdown of the loop's
         variables (blank when the loop has none). Outside a loop it's a plain
         number input, unchanged. -->
    <div v-if="field.control === 'number' && variableCapable" class="sf-numvar" data-test="numvar">
      <select class="sf-numvar__mode" :value="mode" :aria-label="`${field.label} value kind`" data-test="numvar-mode" @change="onModeChange">
        <option value="number">Number</option>
        <option value="variable">Variable</option>
      </select>
      <input
        v-if="mode === 'number'"
        :id="fieldId"
        type="number"
        :value="localText"
        data-test="numvar-number"
        @input="onLocalInput"
        @change="commitNumber"
      />
      <select v-else class="sf-numvar__pick" :value="variablePick" :aria-label="`${field.label} variable`" data-test="numvar-variable" @change="onVariablePick">
        <option value="">—</option>
        <option v-for="v in (loopVars ?? [])" :key="v" :value="v">{{ v }}</option>
      </select>
    </div>
    <input
      v-else-if="field.control === 'number'"
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
          {{ optionLabelFor(opt) }}<span v-if="optionNoteFor(opt)" class="sf-opt-note"> ({{ optionNoteFor(opt) }})</span>
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
      :saved-names="savedNames"
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

    <!-- color: a native color picker paired with a text field, so a tint can be
         picked OR typed/pasted (shorthand #rgb, alpha #rrggbbaa). The picker
         itself only produces #rrggbb; the text field carries the authored value. -->
    <div v-else-if="field.control === 'color'" class="sf-color">
      <input
        type="color"
        :aria-label="`${field.label} picker`"
        :value="colorPickerValue"
        @input="commitColorPicker"
      />
      <input :id="fieldId" type="text" :value="localText" placeholder="#96d6ff" @input="onLocalInput" @change="commitText" />
    </div>

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
import { ALIVE_STATE_OPTIONS, targetQueryOptionHint, targetQueryOptionLabel } from './targetQueryHints'
import type { AbilityBuilderCatalogs } from './useAbilityBuilder'

const props = defineProps<{
  field: SchemaFieldDescriptor
  modelValue: unknown
  /** Program enums bundle (relations, targetOrderings, ...) for enum/multiselect resolution. */
  enums: Record<string, string[]>
  /** Display catalogs (effects, projectiles, damageTypes, ...) for enum/asset resolution. */
  catalogs: AbilityBuilderCatalogs
  /** When this field sits inside a loop, a `number` control offers a
   *  Number/Variable choice; the loop's variable names go here. */
  loopVars?: string[]
  /** True when this field is inside a loop — enables the number field's
   *  literal-or-variable selector. Off (default) keeps every existing field
   *  a plain number input. */
  variableCapable?: boolean
  /** Named-context keys this ability saves to (outputs + store_targets),
   *  forwarded to the target_query control's "Saved Value" picker. */
  savedNames?: string[]
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

// colorPickerValue feeds the native <input type="color">, which ONLY accepts a
// full #rrggbb. Fall back to a sensible default when the authored value isn't
// a full 6-digit hex (empty, shorthand #rgb, or #rrggbbaa) so the swatch never
// silently snaps to black.
const colorPickerValue = computed(() =>
  /^#[0-9a-fA-F]{6}$/.test(localText.value) ? localText.value : '#96d6ff',
)

// commitColorPicker mirrors the swatch's #rrggbb into both the local text field
// and the committed value.
function commitColorPicker(e: Event) {
  const v = (e.target as HTMLInputElement).value
  localText.value = v
  emit('update:modelValue', v)
}

function commitNumber() {
  const n = Number(localText.value)
  emit('update:modelValue', Number.isFinite(n) ? n : 0)
}

// ── number field's literal-or-variable selector (loop bodies only) ──────────
// A value is a variable reference when it's a single lowercase letter string
// (matching the a..z loop-variable grammar). `mode` is a local toggle seeded
// from the value and re-seeded whenever the value changes externally, but the
// user can flip it to author a variable before one is picked.
const isVariableValue = computed(() => typeof props.modelValue === 'string' && /^[a-z]$/.test(props.modelValue))
const mode = ref<'number' | 'variable'>(isVariableValue.value ? 'variable' : 'number')
watch(isVariableValue, (isVar) => {
  mode.value = isVar ? 'variable' : 'number'
})

const variablePick = computed(() => (isVariableValue.value ? (props.modelValue as string) : ''))

function onModeChange(e: Event) {
  const next = (e.target as HTMLSelectElement).value
  if (next === 'variable') {
    mode.value = 'variable' // leave the value until a variable is picked (dropdown blank)
  } else {
    mode.value = 'number'
    const n = Number(localText.value)
    emit('update:modelValue', Number.isFinite(n) ? n : 0) // commit a real number
  }
}

function onVariablePick(e: Event) {
  const v = (e.target as HTMLSelectElement).value
  if (v) emit('update:modelValue', v) // the "—" placeholder emits nothing
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
  // aliveState has no ProgramEnums bundle (see ALIVE_STATE_OPTIONS) — supply
  // its list here so filter_targets gets a real dropdown, not a free-text box.
  if (key === 'alivestate') return ALIVE_STATE_OPTIONS.map((o) => o.id)
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

// Field keys whose enum values are the shared targeting enums — humanize their
// options so the same relation/ordering/state/origin reads identically here
// and in TargetQueryEditor (the target_query control). The map value is the
// LABEL DOMAIN to look up: usually the same key, but `spawnOrigin`
// (launch_projectile / beam) borrows the `origin` labels — so "targets_center"
// reads "Center of Targets", etc. Scoped deliberately so nothing else
// (apply_force's own `origin`, create_zone's anchor, catalog enums) is touched.
const TARGETING_ENUM_DOMAINS: Record<string, string> = {
  relations: 'relations',
  ordering: 'ordering',
  aliveState: 'aliveState',
  spawnOrigin: 'origin',
}

function optionLabelFor(id: string): string {
  const domain = TARGETING_ENUM_DOMAINS[props.field.key]
  if (domain) return targetQueryOptionLabel(domain, id)
  return id === '' ? '(none)' : id
}

function optionNoteFor(id: string): string {
  const domain = TARGETING_ENUM_DOMAINS[props.field.key]
  return domain ? targetQueryOptionHint(domain, id) : ''
}

const optionFilterList = computed<FilterableOption[]>(() =>
  (optionList.value ?? []).map((v) => {
    const label = optionLabelFor(v)
    const note = optionNoteFor(v)
    return { id: v, label: note ? `${label} — ${note}` : label }
  }),
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
//
// Projectiles have no display name of their own (ProjectileDef is id-only), so
// the picker shows the id Title-Cased ("frost_bolt" → "Frost Bolt") — the
// readable name authors expect — while the STORED value stays the raw id.
function titleCaseAssetId(id: string): string {
  return id
    .split(/[_\s]+/)
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

const assetOptions = computed<FilterableOption[]>(() => {
  const isProjectile = props.field.key.toLowerCase() === 'projectile'
  const list = isProjectile ? props.catalogs.projectiles : props.catalogs.effects
  return list.map((v) => ({ id: v, label: isProjectile ? titleCaseAssetId(v) : v }))
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
.sf-numvar {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  min-width: 0;
}

/* The global .ed-shell rule forces width:100% on every input/select, which
   makes both children of this flex row overflow the narrow inspector column
   (one gets shoved off). Reset to auto here so flex sizes them instead: the
   mode select shrinks to its content, the value input takes the rest and can
   shrink to fit. */
.sf-numvar__mode {
  flex: 0 0 auto;
  width: auto;
  max-width: 7em;
}

.sf-numvar input,
.sf-numvar__pick {
  flex: 1 1 0;
  width: auto;
  min-width: 0;
}

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

/* Inline "(unavailable)"-style note after a multiselect option's label,
   matching TargetQueryEditor's .tqe-opt-note treatment. */
.sf-opt-note {
  color: var(--ed-text-dim);
  opacity: 0.75;
}
</style>
