<template>
  <SectionCard title="Preview Scene" collapsible data-test="preview-scene-controls">
    <div class="pv-scene__row">
      <EditorField label="Enemies" for-id="pv-enemy-count" inline class="pv-scene__field">
        <input
          id="pv-enemy-count"
          class="pv-scene__num"
          type="number"
          min="0"
          max="8"
          :value="enemyCount"
          data-test="preview-enemy-count"
          @input="onEnemyCountInput"
        />
      </EditorField>
      <EditorField label="Allies" for-id="pv-ally-count" inline class="pv-scene__field">
        <input
          id="pv-ally-count"
          class="pv-scene__num"
          type="number"
          min="0"
          max="8"
          :value="allyCount"
          data-test="preview-ally-count"
          @input="onAllyCountInput"
        />
      </EditorField>
      <EditorField label="Target" for-id="pv-target" inline class="pv-scene__field">
        <select
          id="pv-target"
          class="pv-scene__select"
          :value="targetSelector"
          data-test="preview-target-selector"
          @change="onTargetSelectorChange"
        >
          <option value="first_enemy">First enemy</option>
          <option value="first_ally">First ally</option>
          <option value="self">Self</option>
          <option value="point">Point</option>
        </select>
      </EditorField>
      <EditorField label="Seed" for-id="pv-seed" inline class="pv-scene__field">
        <input id="pv-seed" class="pv-scene__num" type="number" :value="seed" data-test="preview-seed" @input="onSeedInput" />
      </EditorField>
      <EditorField label="Duration" for-id="pv-duration" inline class="pv-scene__field">
        <input
          id="pv-duration"
          class="pv-scene__num"
          type="number"
          min="0.1"
          step="0.5"
          :value="durationSeconds"
          data-test="preview-duration"
          @input="onDurationInput"
        />
      </EditorField>
      <!-- Only for charge-fire passives (arcane_missiles): seed the caster's
           Arcane Charge so the passive fires. Prefilled to the ability's own
           chargeRequired so one volley is ready by default; bump it to test
           multiple volleys. Hidden for every other ability. -->
      <EditorField
        v-if="chargeRequired != null"
        :label="`Charge`"
        :hint="`(fires at ${chargeRequired})`"
        for-id="pv-charge"
        inline
        class="pv-scene__field"
      >
        <input
          id="pv-charge"
          class="pv-scene__num"
          type="number"
          min="0"
          :value="casterCharge"
          data-test="preview-caster-charge"
          @input="onCasterChargeInput"
        />
      </EditorField>
    </div>

    <!-- Force-a-branch toggles, one per `conditional` in the program. The
         preview caster owns no perks/items/advancements, so every has_perk
         branch evaluates false on its own and its THEN side would be
         unreachable here. Checking a box sends conditionalOverrides[id]=true
         (see Go's PreviewRequest.ConditionalOverrides); unchecking sends
         `false`, which is a REAL forced value, not "evaluate normally" — a
         conditional with no entry at all is what evaluates normally, and that
         only happens for a node the author has never touched (see
         conditionalOverrides below). Every conditional is independent. -->
    <div v-if="conditionals.length" class="pv-scene__conditionals" data-test="preview-conditionals">
      <span class="pv-scene__conditionals-label">Force branches</span>
      <label
        v-for="c in conditionals"
        :key="c.id"
        class="pv-scene__conditional"
        :data-test="`preview-conditional-${c.id}`"
      >
        <input
          type="checkbox"
          :checked="conditionalOverrides[c.id] === true"
          @change="onConditionalToggle(c.id, $event)"
        />
        <span class="pv-scene__conditional-summary">{{ c.summary }}</span>
      </label>
      <p class="pv-scene__hint">
        Checked runs the THEN branch, unchecked the ELSE branch — regardless of what the condition would evaluate to.
      </p>
    </div>

    <p class="pv-scene__hint">
      Drag units (and the caster) on the preview canvas above to place them. Allies start pre-damaged so heals show.
    </p>
  </SectionCard>
</template>

<script setup lang="ts">
// PreviewSceneControls: the COUNT/target/seed/duration half of the preview
// scene editor (Phase 6b). Positions are no longer this component's concern
// — it used to build a full `units[]` at fixed offsets (see the previous
// TODO this change resolves), but per-unit placement is now done by
// DRAGGING units directly on AbilityPreviewCanvas. This component only
// decides HOW MANY enemies/allies exist and how the cast is aimed; the
// parent (AbilityPreviewPanel) owns the actual `sceneUnits[]` array and its
// live positions, reconciling it against `enemyCount`/`allyCount` here
// on every change (see reconcileSceneUnitCounts in AbilityPreviewPanel.vue)
// while preserving whatever positions the user already dragged units to.
import { computed, ref, watch } from 'vue'
import EditorField from '@/components/editor/EditorField.vue'
import SectionCard from '@/components/editor/SectionCard.vue'
import { defaultPreviewRequest } from '@/game/abilities/program/programPreview'
import type { ConditionalRef } from './programTree'

export type TargetSelector = 'first_enemy' | 'first_ally' | 'self' | 'point'

// PreviewSceneConfig is everything this control owns: unit COUNTS (not
// positions — see the module doc comment above), how the cast is aimed, the
// run's seed/duration, and the caster's seeded Arcane Charge (charge-fire
// passives only). The panel derives the actual `target`/`castX`/`castY`
// PreviewRequest fields from `targetSelector` against its own live
// `sceneUnits`/`casterPos`.
export interface PreviewSceneConfig {
  enemyCount: number
  allyCount: number
  targetSelector: TargetSelector
  seed: number
  durationSeconds: number
  casterCharge: number
  /** Forced outcomes for named `conditional` actions, keyed by action id. See
      the template comment on the Force-branches block, and Go's
      PreviewRequest.ConditionalOverrides. Empty for a program with no
      conditionals, or one whose branches the author hasn't touched. */
  conditionalOverrides: Record<string, boolean>
}

// chargeRequired: the ability-under-preview's own charge threshold, supplied by
// the panel when (and only when) it's a charge-fire passive. Non-null unlocks
// the Charge input (prefilled to this value so one volley is ready); null hides
// it. The emitted casterCharge is still sent regardless — it's simply ignored
// server-side for any ability that isn't a charge-fire passive.
// conditionals: every `conditional` action in the program under preview, in
// document order (collectConditionals), each rendered as one force-the-branch
// toggle. Supplied by the panel rather than read from the builder context so
// this control stays a pure props-in/config-out component like the rest of its
// fields.
const props = defineProps<{
  chargeRequired?: number | null
  conditionals?: ConditionalRef[]
}>()

const conditionals = computed(() => props.conditionals ?? [])

const emit = defineEmits<{ 'update:modelValue': [config: PreviewSceneConfig] }>()

// Seeded from defaultPreviewRequest's own scene (1 enemy, 1 pre-damaged
// ally, seed 1, 3s) — the ability param it takes is unused by the scene
// fields, so a throwaway blank def is fine here.
const seedDefaults = defaultPreviewRequest({ id: '' })

const enemyCount = ref(seedDefaults.units.filter((u) => u.team === 'enemy').length)
const allyCount = ref(seedDefaults.units.filter((u) => u.team === 'ally').length)
const targetSelector = ref<TargetSelector>('first_enemy')
const seed = ref(seedDefaults.seed)
const durationSeconds = ref(seedDefaults.durationSeconds)
const casterCharge = ref(seedDefaults.casterCharge)

// conditionalOverrides holds ONLY the conditionals the author has actually
// toggled. An id absent from this map is sent as no override at all, so the
// server evaluates that conditional normally — which keeps an untouched
// preview behaving exactly as it did before this control existed, rather than
// silently pinning every branch to false the moment the panel mounts.
const conditionalOverrides = ref<Record<string, boolean>>({})

// Drop overrides for conditionals that no longer exist. Without this, deleting
// a branch (or switching to another ability) would leave its entry riding along
// in every subsequent request — harmless server-side (unknown ids are ignored)
// but a lie in the emitted config, and it would come back to life if the author
// ever re-created an action with the same id.
watch(
  conditionals,
  (list) => {
    const live = new Set(list.map((c) => c.id))
    const next: Record<string, boolean> = {}
    for (const [id, v] of Object.entries(conditionalOverrides.value)) {
      if (live.has(id)) next[id] = v
    }
    conditionalOverrides.value = next
  },
  { immediate: true },
)

// Keep casterCharge in lockstep with whether a charge field is even shown:
// prefill to the ability's own threshold when a charge-fire ability is under
// preview (so the first Run fires a volley without the author looking the
// number up), and reset to 0 when it isn't — otherwise the hidden field's stale
// value would keep riding along in the emitted config after switching to a
// non-charge ability.
watch(
  () => props.chargeRequired,
  (req) => {
    casterCharge.value = typeof req === 'number' && req > 0 ? req : 0
  },
  { immediate: true },
)

const config = computed<PreviewSceneConfig>(() => ({
  enemyCount: enemyCount.value,
  allyCount: allyCount.value,
  targetSelector: targetSelector.value,
  seed: seed.value,
  durationSeconds: durationSeconds.value,
  casterCharge: casterCharge.value,
  conditionalOverrides: { ...conditionalOverrides.value },
}))

watch(config, (v) => emit('update:modelValue', v), { immediate: true })

function onEnemyCountInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  enemyCount.value = Number.isFinite(n) ? Math.max(0, Math.min(8, Math.trunc(n))) : 0
}

function onAllyCountInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  allyCount.value = Number.isFinite(n) ? Math.max(0, Math.min(8, Math.trunc(n))) : 0
}

function onTargetSelectorChange(e: Event) {
  targetSelector.value = (e.target as HTMLSelectElement).value as TargetSelector
}

function onSeedInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  seed.value = Number.isFinite(n) ? Math.trunc(n) : 0
}

function onDurationInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  durationSeconds.value = Number.isFinite(n) && n > 0 ? n : 0.1
}

function onConditionalToggle(id: string, e: Event) {
  conditionalOverrides.value = {
    ...conditionalOverrides.value,
    [id]: (e.target as HTMLInputElement).checked,
  }
}

function onCasterChargeInput(e: Event) {
  const n = Number((e.target as HTMLInputElement).value)
  casterCharge.value = Number.isFinite(n) && n >= 0 ? n : 0
}
</script>

<style scoped>
/* All five controls flow in a single wrapping row, each as a compact
   label-left-of-input pair. flex-wrap keeps them on one row when the rail is
   wide enough and gracefully drops to a second row when it isn't. */
.pv-scene__conditionals {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--ed-line);
}

.pv-scene__conditionals-label {
  font-family: var(--font-title);
  font-size: 0.72rem;
  font-weight: 700;
  letter-spacing: 0.04em;
  color: var(--ed-brass);
}

.pv-scene__conditional {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.78rem;
  color: var(--ed-text);
}

.pv-scene__conditional-summary {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pv-scene__row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 14px;
}

/* Override EditorField's inline default (space-between, gap 8) so the label
   hugs its input and the whole pair sizes to content instead of stretching. */
.pv-scene__field {
  flex: 0 0 auto;
  justify-content: flex-start;
  gap: 5px;
}

/* Shrink the controls well below the base width:100%. Selectors carry the row
   class + element + control class so they out-specify editor-controls.css's
   `.ed-shell input[type='number']` / `.ed-shell select` width:100% rule. */
.pv-scene__row input.pv-scene__num {
  width: 46px;
  min-width: 0;
  padding-left: 6px;
  padding-right: 4px;
}

.pv-scene__row select.pv-scene__select {
  width: auto;
  min-width: 92px;
}

.pv-scene__hint {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
  font-style: italic;
}
</style>
