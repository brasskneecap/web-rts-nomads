<template>
  <div class="tqe">
    <!-- Source-only delivery shape (launch_projectile / channel_beam — see
         targetQueryFieldsSourceOnly, ability_program_registry.go): the action's
         OWN field label already names the delivery ("Fire Projectile At"), and
         there is NO narrowing step — the action applies to every unit the
         source resolves. Render a bare picker + direct-delivery hint, with no
         section chrome. -->
    <div v-if="sourceOnly" class="tqe-source-row">
      <FilterableSelect
        :model-value="current.source"
        :options="sourceOptions"
        aria-label="Target source"
        @update:model-value="(v) => commit({ source: v as TargetSource })"
      />
      <InfoTip :text="SOURCE_ONLY_HINT" aria-label="About these targets" />
    </div>

    <!-- Full shape: fields grouped into fixed, human-readable sections with
         progressive disclosure. A section renders only when it has at least one
         visible field; `visibleSections` already applies both the declared
         subset (effectiveFields) and per-field visibility (fieldVisible). -->
    <section v-else v-for="sec in visibleSections" :key="sec.title" class="tqe-section">
      <div class="tqe-section__title">{{ sec.title }}</div>
      <template v-for="key in sec.fields" :key="key">
        <!-- source: bare under the "Start With" header — the header IS its
             label, so no doubled "Start With" field label. -->
        <div v-if="key === 'source'" class="tqe-source-row">
          <FilterableSelect
            :model-value="current.source"
            :options="sourceOptions"
            aria-label="Target source"
            @update:model-value="(v) => commit({ source: v as TargetSource })"
          />
          <InfoTip :text="targetQueryFieldHint('source')" aria-label="About Start With" />
        </div>

        <!-- FilterableSelect has no `id`/`for` support of its own (see
             UnitTypeEditorPanel.vue's precedent) — EditorField's label is
             visual-only here; aria-label carries accessibility. -->
        <EditorField v-else-if="key === 'origin'" label="Search Around" hint="(optional)">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('origin')" aria-label="About Search Around" />
        </template>
        <FilterableSelect
          :model-value="current.origin ?? ''"
          :options="originOptions"
          aria-label="Target origin"
          @update:model-value="(v) => commit({ origin: (v || undefined) as TargetOrigin | undefined })"
        />
      </EditorField>

      <!-- originRef: a ContextRef ({key: string}) naming which named-context
           binding (ctx.Named on the server — see resolveOriginLocked /
           resolveTargetQueryLocked, ability_exec_targeting.go) this query reads
           back. Those sites read ctx.Named[key] DIRECTLY, and only `outputs` /
           `store_targets` / `set_context` ever write it — so the picker offers
           the names THIS ability actually saves (`savedNames`, scanned by the
           inspector), with free entry for a name saved elsewhere or not yet
           created. A datalist gives both: real suggestions AND typing. -->
      <EditorField v-else-if="key === 'originRef'" label="Saved Value" hint="(saved selection name, optional)" for-id="tqe-origin-ref">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('originRef')" aria-label="About Saved Value" />
        </template>
        <input
          id="tqe-origin-ref"
          :value="originRefKey"
          :list="originRefListId"
          placeholder="(none)"
          @change="commitOriginRefInput"
        />
        <datalist :id="originRefListId">
          <option v-for="name in savedNames ?? []" :key="name" :value="name" />
        </datalist>
        <p v-if="!(savedNames && savedNames.length)" class="tqe-section__note">
          No saved selections yet — add a Save Targets action (or an action output) to create one.
        </p>
      </EditorField>

      <EditorField v-else-if="key === 'relations'" label="Relationship to Caster">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('relations')" aria-label="About Relationship to Caster" />
        </template>
        <div class="tqe-checkgroup">
          <!-- "Any relationship" makes the empty-means-no-filter state
               explicit: it's active exactly when no specific relation is
               checked. Clicking it clears the specific ones (a reset); you
               leave it by checking a specific relation. -->
          <label class="ed-check tqe-any-rel" :class="{ 'tqe-any-rel--active': noRelationFilter }">
            <input
              type="checkbox"
              :checked="noRelationFilter"
              data-test="tqe-any-relation"
              @change="selectAnyRelationship"
            />
            Any relationship
          </label>
          <label v-for="rel in relationOptions" :key="rel" class="ed-check">
            <input
              type="checkbox"
              :checked="relationSet.has(rel)"
              :data-test="`tqe-rel-${rel}`"
              @change="toggleRelation(rel, ($event.target as HTMLInputElement).checked)"
            />
            {{ targetQueryOptionLabel('relations', rel) }}<span v-if="targetQueryOptionHint('relations', rel)" class="tqe-opt-note"> ({{ targetQueryOptionHint('relations', rel) }})</span>
          </label>
        </div>
      </EditorField>

      <!-- Radius is a PLAIN number here, not a sentinel_number control: Go's
           TargetQueryDef.Radius (server/internal/game/ability_program.go) is
           typed `float64`, unlike AbilityEntryDef.Range / the ability's
           castRange (both of which accept the "match_attack_range" string
           sentinel by design). Emitting that string into this field would
           fail server-side JSON decode. See the report's "deviation" note. -->
      <EditorField v-else-if="key === 'radius'" label="Search Radius" hint="(world units)" for-id="tqe-radius">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('radius')" aria-label="About Search Radius" />
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

      <EditorField v-else-if="key === 'ordering'" label="Prioritize By" hint="(optional)">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('ordering')" aria-label="About Prioritize By" />
        </template>
        <FilterableSelect
          :model-value="current.ordering ?? ''"
          :options="orderingOptions"
          aria-label="Target ordering"
          @update:model-value="(v) => commit({ ordering: (v || undefined) as TargetOrdering | undefined })"
        />
      </EditorField>

      <EditorField v-else-if="key === 'maxCount'" label="Maximum Targets" for-id="tqe-max-count">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('maxCount')" aria-label="About Maximum Targets" />
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
        Always Include Initial Target
        <InfoTip :text="targetQueryFieldHint('includeInitialTarget')" aria-label="About Always Include Initial Target" />
      </label>

      <label v-else-if="key === 'excludeSource'" class="ed-check">
        <input
          type="checkbox"
          :checked="!!current.excludeSource"
          :data-test="'tqe-excludeSource'"
          @change="(e) => commit({ excludeSource: (e.target as HTMLInputElement).checked })"
        />
        Exclude Caster
        <InfoTip :text="targetQueryFieldHint('excludeSource')" aria-label="About Exclude Caster" />
      </label>

      <label v-else-if="key === 'excludeCurrentEvent'" class="ed-check">
        <input
          type="checkbox"
          :checked="!!current.excludeCurrentEvent"
          :data-test="'tqe-excludeCurrentEvent'"
          @change="(e) => commit({ excludeCurrentEvent: (e.target as HTMLInputElement).checked })"
        />
        Exclude Triggering Unit
        <InfoTip :text="targetQueryFieldHint('excludeCurrentEvent')" aria-label="About Exclude Triggering Unit" />
      </label>

      <!-- excludeRef: drop every unit already in a saved set (the chain
           "already hit" set). Same datalist-over-savedNames picker as Saved
           Value — both name a ctx.Named unit-set. Only shown once the ability
           saves a set (fieldVisible), so it can't dangle with nothing to pick. -->
      <EditorField v-else-if="key === 'excludeRef'" label="Exclude Saved Set" hint="(optional)" for-id="tqe-exclude-ref">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('excludeRef')" aria-label="About Exclude Saved Set" />
        </template>
        <input
          id="tqe-exclude-ref"
          :value="excludeRefKey"
          :list="excludeRefListId"
          placeholder="(none)"
          @change="commitExcludeRefInput"
        />
        <datalist :id="excludeRefListId">
          <option v-for="name in savedNames ?? []" :key="name" :value="name" />
        </datalist>
      </EditorField>

      <!-- aliveState: legal values verified against
           applyTargetFiltersLocked's own switch (ability_exec_targeting.go):
           "" and "alive" are equivalent (the default — HP>0 required),
           "dead" requires HP<=0, "any" skips the HP check entirely. The
           server does NOT publish this as a ProgramEnums bundle key (unlike
           relations/targetOrderings/etc), so this curated list is sourced
           from reading that switch directly, not invented — same
           curated-fallback convention as CURATED_SOURCES/CURATED_ORIGINS
           above for when the wire doesn't (yet) carry an enum. -->
      <EditorField v-else-if="key === 'aliveState'" label="Unit State" hint="(optional)">
        <template #label-extra>
          <InfoTip :text="targetQueryFieldHint('aliveState')" aria-label="About Unit State" />
        </template>
        <FilterableSelect
          :model-value="current.aliveState ?? ''"
          :options="aliveStateOptions"
          aria-label="Alive state"
          @update:model-value="(v) => commit({ aliveState: (v || undefined) as string | undefined })"
        />
      </EditorField>
      </template>

      <!-- Progressive disclosure: when a spatial origin IS available in this
           shape but hidden because the radius is 0, tell the author how to
           reveal it instead of leaving the section looking half-empty. -->
      <p v-if="sec.title === 'Search Area' && showSearchAreaHint" class="tqe-section__note">
        Set a Search Radius above 0 to search around a point.
      </p>
    </section>

    <!-- Plain-English confirmation of the whole query, so an author reading
         the sections can sanity-check that it means what they intend. Only for
         the sectioned (narrowing) shape — the source-only delivery shape
         already says what it does via its own hint. -->
    <p v-if="!sourceOnly" class="tqe-summary">{{ summary }}</p>
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
import { computed, getCurrentInstance, ref, watch } from 'vue'
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
import {
  ALIVE_STATE_OPTIONS,
  targetQueryFieldHint,
  targetQueryOptionHint,
  targetQueryOptionLabel,
} from './targetQueryHints'
import { summarizeTargetQuery } from './summarizeTargetQuery'

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
  // Named-context keys this ability saves to (outputs + store_targets),
  // scanned by the inspector — the suggestions for the "Saved Value" picker.
  savedNames?: string[]
}>()

const emit = defineEmits<{ 'update:modelValue': [value: TargetQueryDef] }>()

// Mirrors the server's targetQueryFieldsFull (ability_program_registry.go) —
// every currently-enforced TargetQueryDef sub-field. Used ONLY for membership
// (which fields this action exposes) when the caller can't supply a declared
// subset; DISPLAY order and grouping come from SECTION_DEFS below, not from
// this list's order.
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
  'excludeRef',
  'aliveState',
]

const effectiveFields = computed(() => props.fields ?? ALL_TARGET_QUERY_FIELDS)

// A query that exposes ONLY `source` is a "deliver to whoever this resolves"
// shape (launch_projectile / channel_beam), not a "start a search then narrow"
// shape. It gets a bare picker + delivery hint instead of the sectioned
// layout — see the template comment on the source-only row.
const sourceOnly = computed(
  () => effectiveFields.value.length === 1 && effectiveFields.value[0] === 'source',
)
const SOURCE_ONLY_HINT =
  'Every unit selected here is targeted directly — there is no filtering step after this, so the action applies to each one.'

// ── Sectioned layout + progressive disclosure ──────────────────────────────
// The 11 flat fields group into four human-readable sections that mirror the
// pipeline's stages: WHERE candidates come from → WHERE distance is measured
// from → WHICH units survive → HOW the survivors are capped. Every
// TargetQueryDef field appears in exactly one section; the order here IS the
// display order (declared subset controls presence, not order).
//
// `radius` is placed before `origin`: the radius GATES the origin (a spatial
// origin is meaningless at radius 0), so a new author sees just Search Radius,
// sets it, and the origin picker reveals directly BELOW it — with the
// showSearchAreaHint nudge shown in that spot while it's hidden.
interface TqeSection {
  title: string
  fields: string[]
}
const SECTION_DEFS: TqeSection[] = [
  { title: 'Start With', fields: ['source'] },
  { title: 'Search Area', fields: ['radius', 'origin', 'originRef'] },
  {
    title: 'Filter Units',
    fields: ['relations', 'aliveState', 'includeInitialTarget', 'excludeSource', 'excludeCurrentEvent', 'excludeRef'],
  },
  { title: 'Choose Results', fields: ['ordering', 'maxCount'] },
]

const radiusActive = computed(() => (current.value.radius ?? 0) > 0)

// Saved Value (originRef) only means something when a "named" source or origin
// needs it to say WHICH saved binding to use — hide it otherwise. Mirrors
// resolveOriginLocked's ref==nil handling server-side (ability_exec_targeting.go).
const needsSavedValue = computed(
  () => current.value.source === 'named_context' || current.value.origin === 'named_context_value',
)

function fieldVisible(key: string): boolean {
  // A spatial origin is meaningless without a radius to measure — hide it at 0.
  if (key === 'origin') return radiusActive.value
  if (key === 'originRef') return needsSavedValue.value
  // "Exclude Saved Set" only makes sense once the ability saves a set to point
  // at — hide it until then (but never hide one already configured).
  if (key === 'excludeRef') return hasSavedNames.value || !!current.value.excludeRef
  return true
}

// Sections with their currently-renderable fields (declared AND visible),
// dropping any section left with nothing to show.
const visibleSections = computed(() =>
  SECTION_DEFS.map((sec) => ({
    title: sec.title,
    fields: sec.fields.filter((k) => effectiveFields.value.includes(k) && fieldVisible(k)),
  })).filter((sec) => sec.fields.length > 0),
)

// Show the "set a radius" nudge only when this shape HAS a spatial origin to
// offer but it's currently suppressed by a 0 radius.
const showSearchAreaHint = computed(
  () => effectiveFields.value.includes('origin') && !radiusActive.value,
)

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

// aliveState options are shared with SchemaField's generic enum path (see
// ALIVE_STATE_OPTIONS' doc comment) so filter_targets renders the same
// dropdown as this control.
const aliveStateOptions: FilterableOption[] = ALIVE_STATE_OPTIONS

// `field` drives BOTH the human-readable option label (targetQueryOptionLabel)
// and the inline suffix (targetQueryOptionHint) — an option with no mapped
// label renders with its raw wire value, and one with no note renders
// unsuffixed (the same graceful-degradation contract as the field-level
// InfoTip text: missing copy is silent, never wrong). `blankLabel`, when
// given, prepends a "no selection" entry (id '') with a per-field human label
// (e.g. "No Spatial Origin", "No Priority") instead of a bare "(none)".
function toOptions(field: string, list: string[], blankLabel?: string): FilterableOption[] {
  const opts = list.map((v) => {
    const label = targetQueryOptionLabel(field, v)
    const note = targetQueryOptionHint(field, v)
    return { id: v, label: note ? `${label} — ${note}` : label }
  })
  return blankLabel !== undefined ? [{ id: '', label: blankLabel }, ...opts] : opts
}

const sourceOptions = computed(() =>
  toOptions('source', props.enums.targetSources?.length ? props.enums.targetSources : CURATED_SOURCES),
)
const originOptions = computed(() =>
  toOptions('origin', props.enums.targetOrigins?.length ? props.enums.targetOrigins : CURATED_ORIGINS, 'No Spatial Origin'),
)
const orderingOptions = computed(() =>
  toOptions('ordering', props.enums.targetOrderings?.length ? props.enums.targetOrderings : CURATED_ORDERINGS, 'No Priority'),
)
const relationOptions = computed<string[]>(() =>
  props.enums.relations?.length ? props.enums.relations : CURATED_RELATIONS,
)

// current: the effective query, defaulted so every control has something to
// bind to even before the author has touched this action's targeting at all.
const current = computed<TargetQueryDef>(() => props.modelValue ?? { source: 'all_in_scene' })

// Plain-English description of the current query, shown beneath the sections.
const summary = computed(() => summarizeTargetQuery(current.value))

function commit(patch: Partial<TargetQueryDef>) {
  emit('update:modelValue', { ...current.value, ...patch })
}

// relationSet/relationOptions are plain strings, not TargetRelation, because
// relationOptions is sourced from the server's enums bundle (a forward-compat
// string[] that may include values beyond TargetRelation's closed union) —
// only the final commit widens back to TargetRelation[].
const relationSet = computed<Set<string>>(() => new Set(current.value.relations ?? []))

// No specific relation checked ⇒ no relationship filter (every unit passes).
// This is the wire semantics of an empty `relations`; the "Any relationship"
// checkbox just surfaces it instead of leaving it implied by empty boxes.
const noRelationFilter = computed(() => relationSet.value.size === 0)

function toggleRelation(rel: string, checked: boolean) {
  const next = new Set(relationSet.value)
  if (checked) next.add(rel)
  else next.delete(rel)
  commit({ relations: [...next] as TargetRelation[] })
}

// "Any relationship" is a reset: clear every specific relation so the query
// filters by none. Idempotent (clicking it while already active is a no-op, so
// it can't be unchecked directly) — you leave it by checking a specific
// relation, which drops it out of the "no filter" state automatically.
function selectAnyRelationship() {
  if (!noRelationFilter.value) commit({ relations: [] })
}

// ── saved-value pickers (originRef, excludeRef) ────────────────────────────
// Both name a ctx.Named unit-set, so both draw suggestions from the same
// scanned `savedNames`. A per-instance uid keeps their datalist ids unique.
const hasSavedNames = computed(() => (props.savedNames?.length ?? 0) > 0)
const instanceUid = getCurrentInstance()?.uid ?? 0

const originRefKey = computed(() => current.value.originRef?.key ?? '')
const originRefListId = `tqe-origin-ref-list-${instanceUid}`

function commitOriginRefInput(e: Event) {
  const key = (e.target as HTMLInputElement).value.trim()
  commit({ originRef: key ? { key } : undefined })
}

const excludeRefKey = computed(() => current.value.excludeRef?.key ?? '')
const excludeRefListId = `tqe-exclude-ref-list-${instanceUid}`

function commitExcludeRefInput(e: Event) {
  const key = (e.target as HTMLInputElement).value.trim()
  commit({ excludeRef: key ? { key } : undefined })
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
  gap: 12px;
}

/* A subsection group. These sit INSIDE the inspector's brass SectionCard
   ("Targeting"), so they read as a lighter, subordinate grouping — a dim
   uppercase caption over a hairline, not a second brass card. */
.tqe-section {
  display: flex;
  flex-direction: column;
  gap: var(--ed-gap, 8px);
}

.tqe-section__title {
  font-family: var(--font-body);
  font-size: 0.66rem;
  font-weight: 700;
  letter-spacing: 0.09em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
  padding-bottom: 3px;
  border-bottom: 1px solid var(--ed-line);
}

/* The progressive-disclosure nudge shown in place of a hidden field (e.g.
   Search Around while the radius is 0). */
.tqe-section__note {
  margin: 0;
  font-size: 0.7rem;
  font-style: italic;
  color: var(--ed-text-dim);
  opacity: 0.8;
}

/* The generated plain-English summary of the whole query — a quiet callout
   set off by a brass left-rule so it reads as commentary, not another field. */
.tqe-summary {
  margin: 2px 0 0;
  padding: 6px 10px;
  border-left: 2px solid var(--ed-brass-dim);
  background: rgba(212, 168, 71, 0.06);
  border-radius: 0 4px 4px 0;
  font-size: 0.74rem;
  font-style: italic;
  line-height: 1.4;
  color: var(--ed-text);
}

.tqe-checkgroup {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
}

/* "Any relationship" reads as the "no filter" reset, set off from the specific
   relations by a trailing divider. It dims when inactive so the specific
   checkboxes are the visual focus once the author picks any. */
.tqe-any-rel {
  padding-right: 14px;
  border-right: 1px solid var(--ed-line);
  opacity: 0.7;
}

.tqe-any-rel--active {
  opacity: 1;
}

/* Source row (both the source-only delivery shape and the bare source under
   the "Start With" header): the picker sits inline with its InfoTip, with no
   field label of its own — the section header / action label names it. */
.tqe-source-row {
  display: flex;
  align-items: center;
  gap: 6px;
}

/* Inline note after a relation checkbox's label (e.g. "neutral — not
   implemented, never matches") — same dimmed treatment as EditorField's
   own .ed-field__hint, just scoped locally since this isn't inside one. */
.tqe-opt-note {
  color: var(--ed-text-dim);
  opacity: 0.75;
}
</style>
