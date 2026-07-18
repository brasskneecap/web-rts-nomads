<template>
  <div class="tqe">
    <template v-for="key in effectiveFields" :key="key">
      <!-- FilterableSelect has no `id`/`for` support of its own (see
           UnitTypeEditorPanel.vue's precedent) — EditorField's label is
           visual-only here; aria-label carries accessibility. -->
      <EditorField v-if="key === 'source'" label="Source">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('source')" aria-label="About Source" />
        </template>
        <FilterableSelect
          :model-value="current.source"
          :options="sourceOptions"
          aria-label="Target source"
          @update:model-value="(v) => commit({ source: v as TargetSource })"
        />
      </EditorField>

      <EditorField v-else-if="key === 'origin'" label="Origin" hint="(optional)">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('origin')" aria-label="About Origin" />
        </template>
        <FilterableSelect
          :model-value="current.origin ?? ''"
          :options="originOptions"
          aria-label="Target origin"
          @update:model-value="(v) => commit({ origin: (v || undefined) as TargetOrigin | undefined })"
        />
      </EditorField>

      <!-- originRef: a ContextRef ({key: string}) naming which named-context
           binding (ctx.Named on the server — see resolveOriginLocked,
           ability_exec_targeting.go) supplies the origin position when
           `origin` is "named_context_value" or similar. Mirrors the
           `context_ref` control's own curated-key select (SchemaField.vue's
           `position`/`owner` fields) rather than inventing a different
           widget for the same underlying shape — same TODO(phase-6)
           caveat: scope this to keys actually bound at this position instead
           of a fixed curated list. -->
      <EditorField v-else-if="key === 'originRef'" label="Origin Ref" hint="(named context key, optional)" for-id="tqe-origin-ref">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('originRef')" aria-label="About Origin Ref" />
        </template>
        <select id="tqe-origin-ref" :value="originRefKey" @change="commitOriginRef">
          <option value="">(none)</option>
          <option v-for="k in CONTEXT_REF_KEYS" :key="k" :value="k">{{ k }}</option>
        </select>
      </EditorField>

      <EditorField v-else-if="key === 'relations'" label="Relations">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('relations')" aria-label="About Relations" />
        </template>
        <div class="tqe-checkgroup">
          <label v-for="rel in relationOptions" :key="rel" class="ed-check">
            <input
              type="checkbox"
              :checked="relationSet.has(rel)"
              @change="toggleRelation(rel, ($event.target as HTMLInputElement).checked)"
            />
            {{ rel }}<span v-if="targetQueryOptionHint('relations', rel)" class="tqe-opt-note"> ({{ targetQueryOptionHint('relations', rel) }})</span>
          </label>
        </div>
      </EditorField>

      <!-- Radius is a PLAIN number here, not a sentinel_number control: Go's
           TargetQueryDef.Radius (server/internal/game/ability_program.go) is
           typed `float64`, unlike AbilityEntryDef.Range / the ability's
           castRange (both of which accept the "match_attack_range" string
           sentinel by design). Emitting that string into this field would
           fail server-side JSON decode. See the report's "deviation" note. -->
      <EditorField v-else-if="key === 'radius'" label="Radius" hint="(world units)" for-id="tqe-radius">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('radius')" aria-label="About Radius" />
        </template>
        <input
          id="tqe-radius"
          type="number"
          min="0"
          :value="radiusText"
          @input="radiusText = ($event.target as HTMLInputElement).value"
          @change="commitRadius"
        />
      </EditorField>

      <EditorField v-else-if="key === 'ordering'" label="Ordering" hint="(optional)">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('ordering')" aria-label="About Ordering" />
        </template>
        <FilterableSelect
          :model-value="current.ordering ?? ''"
          :options="orderingOptions"
          aria-label="Target ordering"
          @update:model-value="(v) => commit({ ordering: (v || undefined) as TargetOrdering | undefined })"
        />
      </EditorField>

      <EditorField v-else-if="key === 'maxCount'" label="Max Count" for-id="tqe-max-count">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('maxCount')" aria-label="About Max Count" />
        </template>
        <input
          id="tqe-max-count"
          type="number"
          min="0"
          :value="maxCountText"
          @input="maxCountText = ($event.target as HTMLInputElement).value"
          @change="commitMaxCount"
        />
      </EditorField>

      <label v-else-if="key === 'includeInitialTarget'" class="ed-check">
        <input
          type="checkbox"
          :checked="!!current.includeInitialTarget"
          :data-test="'tqe-includeInitialTarget'"
          @change="(e) => commit({ includeInitialTarget: (e.target as HTMLInputElement).checked })"
        />
        Include initial target
        <InfoTip :text="targetQueryFieldHint('includeInitialTarget')" aria-label="About Include initial target" />
      </label>

      <label v-else-if="key === 'excludeSource'" class="ed-check">
        <input
          type="checkbox"
          :checked="!!current.excludeSource"
          :data-test="'tqe-excludeSource'"
          @change="(e) => commit({ excludeSource: (e.target as HTMLInputElement).checked })"
        />
        Exclude source
        <InfoTip :text="targetQueryFieldHint('excludeSource')" aria-label="About Exclude source" />
      </label>

      <label v-else-if="key === 'excludeCurrentEvent'" class="ed-check">
        <input
          type="checkbox"
          :checked="!!current.excludeCurrentEvent"
          :data-test="'tqe-excludeCurrentEvent'"
          @change="(e) => commit({ excludeCurrentEvent: (e.target as HTMLInputElement).checked })"
        />
        Exclude current event
        <InfoTip :text="targetQueryFieldHint('excludeCurrentEvent')" aria-label="About Exclude current event" />
      </label>

      <!-- aliveState: legal values verified against
           applyTargetFiltersLocked's own switch (ability_exec_targeting.go):
           "" and "alive" are equivalent (the default — HP>0 required),
           "dead" requires HP<=0, "any" skips the HP check entirely. The
           server does NOT publish this as a ProgramEnums bundle key (unlike
           relations/targetOrderings/etc), so this curated list is sourced
           from reading that switch directly, not invented — same
           curated-fallback convention as CURATED_SOURCES/CURATED_ORIGINS
           above for when the wire doesn't (yet) carry an enum. -->
      <EditorField v-else-if="key === 'aliveState'" label="Alive State" hint="(optional)">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('aliveState')" aria-label="About Alive State" />
        </template>
        <FilterableSelect
          :model-value="current.aliveState ?? ''"
          :options="aliveStateOptions"
          aria-label="Alive state"
          @update:model-value="(v) => commit({ aliveState: (v || undefined) as string | undefined })"
        />
      </EditorField>
    </template>
  </div>
</template>

<script setup lang="ts">
// TargetQueryEditor edits a TargetQueryDef. Which of its 10 sub-fields
// render — and in what order — is DECLARED by the caller via the `fields`
// prop (sourced from the schema field's own `targetQueryFields`, see
// programSchema.ts): a projectile flying at one target has no use for
// Radius/Ordering/MaxCount the way a scene-wide select_targets query does,
// so this no longer renders a fixed hardcoded set unconditionally. Used both
// by SchemaField (a `target_query`-control config field, e.g.
// select_targets'/launch_projectile's `target`) and, formerly, by
// InspectorBar's blanket fallback — that fallback is gone; every
// target_query field now goes through SchemaField.
//
// Every commit is IMMUTABLE: it spreads `current` (the incoming modelValue,
// defaulted) with the changed key(s) and emits the merged object — the
// caller is expected to route that through a builder op (updateAction),
// which snapshots for undo itself. Discrete controls (selects, checkboxes)
// commit immediately; the one text-ish control (radius, when not "matches
// attack range") uses the same local-copy-then-commit-on-change pattern as
// SchemaField, so typing a radius doesn't flood undo per keystroke.
import { computed, ref, watch } from 'vue'
import type {
  TargetOrdering,
  TargetOrigin,
  TargetQueryDef,
  TargetRelation,
  TargetSource,
} from '@/game/abilities/program/abilityProgram'
import EditorField from '@/components/editor/EditorField.vue'
import FilterableSelect, { type FilterableOption } from '@/components/editor/FilterableSelect.vue'
import InfoTip from '@/components/editor/InfoTip.vue'
import { targetQueryFieldHint, targetQueryOptionHint } from './targetQueryHints'

const props = defineProps<{
  modelValue: TargetQueryDef | undefined
  enums: Record<string, string[]>
  // Declared subset (and display order) of TargetQueryDef sub-fields THIS
  // action's targeting shape actually uses — sourced from the schema
  // field's own `targetQueryFields` (programSchema.ts). `undefined` falls
  // back to the full set (see ALL_TARGET_QUERY_FIELDS below) rather than
  // rendering nothing, so a target_query field the schema hasn't (yet)
  // annotated degrades to the old blanket behavior instead of going blank.
  fields?: string[]
}>()

const emit = defineEmits<{ 'update:modelValue': [value: TargetQueryDef] }>()

// Mirrors the server's targetQueryFieldsFull (ability_program_registry.go) —
// every currently-enforced TargetQueryDef sub-field, in display order. Used
// ONLY as a fallback when the caller can't supply a declared subset.
const ALL_TARGET_QUERY_FIELDS = [
  'source',
  'origin',
  'originRef',
  'relations',
  'radius',
  'ordering',
  'maxCount',
  'includeInitialTarget',
  'excludeSource',
  'excludeCurrentEvent',
  'aliveState',
]

const effectiveFields = computed(() => props.fields ?? ALL_TARGET_QUERY_FIELDS)

// Curated fallbacks for when the schema hasn't loaded yet — mirrors
// AbilityFlow.vue's CURATED_TRIGGER_TYPES precedent.
const CURATED_SOURCES: TargetSource[] = [
  'caster',
  'initial_target',
  'previous_action_targets',
  'current_event',
  'named_context',
  'source_object',
  'all_in_scene',
]
const CURATED_ORIGINS: TargetOrigin[] = [
  'caster',
  'initial_target',
  'initial_target_position',
  'cast_point',
  'impact_position',
  'zone_center',
]
const CURATED_ORDERINGS: TargetOrdering[] = [
  'closest',
  'farthest',
  'lowest_health',
  'lowest_health_percentage',
  'highest_health',
  'random',
  'unit_id',
]
const CURATED_RELATIONS: TargetRelation[] = ['self', 'ally', 'enemy', 'neutral']

// Mirrors SchemaField.vue's own CONTEXT_REF_KEYS for its `context_ref`
// control (position/owner on play_presentation/create_zone) — originRef is
// the same ContextRef({key: string}) shape, naming a ctx.Named binding.
// TODO(phase-6): scope to keys actually bound at this position, same as
// that file's TODO.
const CONTEXT_REF_KEYS = ['caster', 'initialTarget', 'castPoint', 'impactPosition', 'zoneCenter', 'previous_action_targets']

// aliveState: see the template's doc comment on this field for why the
// options are curated from source rather than pulled off the enums bundle.
const aliveStateOptions: FilterableOption[] = [
  { id: '', label: '(default: alive)' },
  { id: 'alive', label: 'alive' },
  { id: 'dead', label: 'dead' },
  { id: 'any', label: 'any' },
]

// `field` drives the option-level suffix lookup (targetQueryHints.ts) — an
// option with no entry there renders with its plain value as the label,
// unsuffixed (the same graceful-degradation contract as the field-level
// InfoTip text: missing copy is silent, never wrong).
function toOptions(field: string, list: string[], allowBlank: boolean): FilterableOption[] {
  const opts = list.map((v) => {
    const note = targetQueryOptionHint(field, v)
    return { id: v, label: note ? `${v} — ${note}` : v }
  })
  return allowBlank ? [{ id: '', label: '(none)' }, ...opts] : opts
}

const sourceOptions = computed(() =>
  toOptions('source', props.enums.targetSources?.length ? props.enums.targetSources : CURATED_SOURCES, false),
)
const originOptions = computed(() =>
  toOptions('origin', props.enums.targetOrigins?.length ? props.enums.targetOrigins : CURATED_ORIGINS, true),
)
const orderingOptions = computed(() =>
  toOptions('ordering', props.enums.targetOrderings?.length ? props.enums.targetOrderings : CURATED_ORDERINGS, true),
)
const relationOptions = computed<string[]>(() =>
  props.enums.relations?.length ? props.enums.relations : CURATED_RELATIONS,
)

// current: the effective query, defaulted so every control has something to
// bind to even before the author has touched this action's targeting at all.
const current = computed<TargetQueryDef>(() => props.modelValue ?? { source: 'all_in_scene' })

function commit(patch: Partial<TargetQueryDef>) {
  emit('update:modelValue', { ...current.value, ...patch })
}

// relationSet/relationOptions are plain strings, not TargetRelation, because
// relationOptions is sourced from the server's enums bundle (a forward-compat
// string[] that may include values beyond TargetRelation's closed union) —
// only the final commit widens back to TargetRelation[].
const relationSet = computed<Set<string>>(() => new Set(current.value.relations ?? []))

function toggleRelation(rel: string, checked: boolean) {
  const next = new Set(relationSet.value)
  if (checked) next.add(rel)
  else next.delete(rel)
  commit({ relations: [...next] as TargetRelation[] })
}

// ── originRef ────────────────────────────────────────────────────────────
const originRefKey = computed(() => current.value.originRef?.key ?? '')

function commitOriginRef(e: Event) {
  const key = (e.target as HTMLSelectElement).value
  commit({ originRef: key ? { key } : undefined })
}

// ── radius (plain number — see the template's comment on why this is NOT a
//    sentinel_number control) ───────────────────────────────────────────
const radiusText = ref(toDisplayString(current.value.radius))

function toDisplayString(v: unknown): string {
  if (v === undefined || v === null) return ''
  return String(v)
}

watch(
  () => current.value.radius,
  (v) => {
    radiusText.value = toDisplayString(v)
  },
)

function commitRadius() {
  const n = Number(radiusText.value)
  commit({ radius: Number.isFinite(n) ? n : 0 })
}

// ── maxCount ─────────────────────────────────────────────────────────────
const maxCountText = ref(toDisplayString(current.value.maxCount))

watch(
  () => current.value.maxCount,
  (v) => {
    maxCountText.value = toDisplayString(v)
  },
)

function commitMaxCount() {
  const n = Number(maxCountText.value)
  commit({ maxCount: Number.isFinite(n) ? n : 0 })
}
</script>

<style scoped>
.tqe {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tqe-checkgroup {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
}

/* Inline note after a relation checkbox's label (e.g. "neutral — not
   implemented, never matches") — same dimmed treatment as EditorField's
   own .ed-field__hint, just scoped locally since this isn't inside one. */
.tqe-opt-note {
  color: var(--ed-text-dim);
  opacity: 0.75;
}
</style>
