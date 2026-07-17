<template>
  <div class="tqe">
    <!-- FilterableSelect has no `id`/`for` support of its own (see
         UnitTypeEditorPanel.vue's precedent) — EditorField's label is
         visual-only here; aria-label carries accessibility. -->
    <EditorField label="Source">
      <FilterableSelect
        :model-value="current.source"
        :options="sourceOptions"
        aria-label="Target source"
        @update:model-value="(v) => commit({ source: v as TargetSource })"
      />
    </EditorField>

    <EditorField label="Origin" hint="(optional)">
      <FilterableSelect
        :model-value="current.origin ?? ''"
        :options="originOptions"
        aria-label="Target origin"
        @update:model-value="(v) => commit({ origin: (v || undefined) as TargetOrigin | undefined })"
      />
    </EditorField>

    <EditorField label="Relations">
      <div class="tqe-checkgroup">
        <label v-for="rel in relationOptions" :key="rel" class="ed-check">
          <input
            type="checkbox"
            :checked="relationSet.has(rel)"
            @change="toggleRelation(rel, ($event.target as HTMLInputElement).checked)"
          />
          {{ rel }}
        </label>
      </div>
    </EditorField>

    <!-- Radius is a PLAIN number here, not a sentinel_number control: Go's
         TargetQueryDef.Radius (server/internal/game/ability_program.go) is
         typed `float64`, unlike AbilityEntryDef.Range / the ability's
         castRange (both of which accept the "match_attack_range" string
         sentinel by design). Emitting that string into this field would
         fail server-side JSON decode. See the report's "deviation" note. -->
    <EditorField label="Radius" hint="(world units)" for-id="tqe-radius">
      <input
        id="tqe-radius"
        type="number"
        min="0"
        :value="radiusText"
        @input="radiusText = ($event.target as HTMLInputElement).value"
        @change="commitRadius"
      />
    </EditorField>

    <EditorField label="Ordering" hint="(optional)">
      <FilterableSelect
        :model-value="current.ordering ?? ''"
        :options="orderingOptions"
        aria-label="Target ordering"
        @update:model-value="(v) => commit({ ordering: (v || undefined) as TargetOrdering | undefined })"
      />
    </EditorField>

    <EditorField label="Max Count" for-id="tqe-max-count">
      <input
        id="tqe-max-count"
        type="number"
        min="0"
        :value="maxCountText"
        @input="maxCountText = ($event.target as HTMLInputElement).value"
        @change="commitMaxCount"
      />
    </EditorField>

    <label class="ed-check">
      <input
        type="checkbox"
        :checked="!!current.includeInitialTarget"
        @change="(e) => commit({ includeInitialTarget: (e.target as HTMLInputElement).checked })"
      />
      Include initial target
    </label>
    <label class="ed-check">
      <input
        type="checkbox"
        :checked="!!current.excludeSource"
        @change="(e) => commit({ excludeSource: (e.target as HTMLInputElement).checked })"
      />
      Exclude source
    </label>
  </div>
</template>

<script setup lang="ts">
// TargetQueryEditor edits a TargetQueryDef (source/origin/relations/radius/
// ordering/maxCount/includeInitialTarget/excludeSource). Used both directly
// by InspectorBar (an action's `target`) and by SchemaField (a
// `target_query`-control config field, e.g. select_targets' `target`).
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

const props = defineProps<{
  modelValue: TargetQueryDef | undefined
  enums: Record<string, string[]>
}>()

const emit = defineEmits<{ 'update:modelValue': [value: TargetQueryDef] }>()

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

function toOptions(list: string[], allowBlank: boolean): FilterableOption[] {
  const opts = list.map((v) => ({ id: v, label: v }))
  return allowBlank ? [{ id: '', label: '(none)' }, ...opts] : opts
}

const sourceOptions = computed(() =>
  toOptions(props.enums.targetSources?.length ? props.enums.targetSources : CURATED_SOURCES, false),
)
const originOptions = computed(() =>
  toOptions(props.enums.targetOrigins?.length ? props.enums.targetOrigins : CURATED_ORIGINS, true),
)
const orderingOptions = computed(() =>
  toOptions(props.enums.targetOrderings?.length ? props.enums.targetOrderings : CURATED_ORDERINGS, true),
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
</style>
