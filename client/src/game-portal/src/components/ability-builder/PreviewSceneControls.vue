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
      <!-- Allies charge in and attack. The only way to preview an ability whose
           whole effect is on somebody ELSE'S damage (marker_trap's mark deals
           none of its own). With only one side ticked the other stays inert so
           the HP delta is attributable. -->
      <EditorField label="Allies attack" for-id="pv-allies-attack" inline class="pv-scene__field">
        <input
          id="pv-allies-attack"
          class="pv-scene__check"
          type="checkbox"
          :checked="alliesAttack"
          data-test="preview-allies-attack"
          @change="onAlliesAttackChange"
        />
      </EditorField>
      <!-- Enemies charge in and attack — the mirror of "Allies attack", for
           previewing an ability whose effect lands on an ENEMY'S outgoing damage
           (a Weaken debuff makes the marked enemy deal less, visible only once it
           actually swings). -->
      <EditorField label="Enemies attack" for-id="pv-enemies-attack" inline class="pv-scene__field">
        <input
          id="pv-enemies-attack"
          class="pv-scene__check"
          type="checkbox"
          :checked="enemiesAttack"
          data-test="preview-enemies-attack"
          @change="onEnemiesAttackChange"
        />
      </EditorField>
      <!-- Caster, then Path, then Rank: each choice narrows the next, and each
           one matters because an ability's damage can scale off its caster
           (deal_damage's adRatio/apRatio) and off how far that caster has been
           promoted — previewing against one hardcoded unit showed neither.
           Blank = the harness default (an adept). -->
      <EditorField label="Caster" for-id="pv-caster" inline class="pv-scene__field">
        <select
          id="pv-caster"
          class="pv-scene__select"
          :value="casterUnitType"
          data-test="preview-caster-unit"
          @change="onCasterUnitTypeChange"
        >
          <option value="">Default (adept)</option>
          <option v-for="u in casterOptions" :key="u.type" :value="u.type">{{ u.label }}</option>
        </select>
      </EditorField>
      <!-- The path is what turns a rank into real stats (pathModifierFor), so
           without it a ranked preview uses a generic curve no real unit has.
           Options are the paths belonging to the CHOSEN caster, so the pair can
           never be incoherent. -->
      <EditorField
        v-if="pathOptions.length > 0"
        label="Path"
        for-id="pv-path"
        inline
        class="pv-scene__field"
      >
        <select
          id="pv-path"
          class="pv-scene__select"
          :value="casterPath"
          data-test="preview-caster-path"
          @change="onCasterPathChange"
        >
          <option value="">None (generic curve)</option>
          <option v-for="p in pathOptions" :key="p" :value="p">{{ humanizePath(p) }}</option>
        </select>
      </EditorField>
      <!-- Base is only offered while the caster is PATHLESS. A promotion path is
           earned at bronze, so a unit on one is never at base — offering it
           would describe a unit that cannot exist. Picking a path promotes the
           rank to bronze for the same reason. -->
      <EditorField label="Rank" for-id="pv-rank" inline class="pv-scene__field">
        <select
          id="pv-rank"
          class="pv-scene__select"
          :value="casterRank"
          data-test="preview-caster-rank"
          @change="onCasterRankChange"
        >
          <option v-if="!casterPath" value="">Base</option>
          <option value="bronze">Bronze</option>
          <option value="silver">Silver</option>
          <option value="gold">Gold</option>
        </select>
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
      <!-- Perks the caster OWNS for this run: ONE dropdown per rank, because a
           unit carries at most one perk per rank. Options come from the selected
           path's own perksByRank, so a pair that cannot exist in a match cannot
           be built here either.

           Replaces the old force-a-branch checkboxes. Forcing proved the THEN
           side produced some effect but never that the CONDITION was right — a
           has_perk naming a perk that does not exist previewed identically to a
           correct one. Owning the perk runs the real evaluator. -->
      <div v-if="perkRankRows.length" class="pv-scene__perks" data-test="preview-perks">
        <EditorField
          v-for="row in perkRankRows"
          :key="row.rank"
          :label="row.label"
          :for-id="`pv-perk-${row.rank}`"
          inline
          class="pv-scene__field"
        >
          <select
            :id="`pv-perk-${row.rank}`"
            class="pv-scene__select"
            :value="perkByRank[row.rank] ?? ''"
            :data-test="`preview-perk-${row.rank}`"
            @change="onPerkRankChange(row.rank, $event)"
          >
            <option value="">None</option>
            <option v-for="p in row.options" :key="p.id" :value="p.id">{{ p.label }}</option>
          </select>
        </EditorField>
      </div>
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
  /** Which unit casts, and at what rank. Empty = the harness default (adept,
      spawn rank). See PreviewRequest.casterUnitType. */
  casterUnitType: string
  casterRank: string
  /** Promotion path — what actually determines a rank's stats. */
  casterPath: string
  /** Perks the caster owns for this run. Perk-gated branches and perk
      modifiers then behave exactly as they would in a match, which is what
      replaced the old forced-conditional map. */
  casterPerks: string[]
  /** Send the allies in to attack. See PreviewRequest.alliesAttack — the only
      way to see an ability that changes someone else's damage rather than
      dealing its own. */
  alliesAttack: boolean
  /** Send the enemies in to attack. The mirror of alliesAttack (see
      PreviewRequest.enemiesAttack) — for an ability that changes an ENEMY'S
      outgoing damage, e.g. a Weaken debuff. */
  enemiesAttack: boolean
}

// chargeRequired: the ability-under-preview's own charge threshold, supplied by
// the panel when (and only when) it's a charge-fire passive. Non-null unlocks
// the Charge input (prefilled to this value so one volley is ready); null hides
// it. The emitted casterCharge is still sent regardless — it's simply ignored
// server-side for any ability that isn't a charge-fire passive.
// perkOptions: every perk in the catalog, id -> display name. Supplied by the
// panel rather than read from the builder context so this control stays a pure
// props-in/config-out component like the rest of its fields. WHICH of them a
// rank offers comes from the path catalog below, not from this list.
const props = defineProps<{
  chargeRequired?: number | null
  perkOptions?: { id: string; label: string }[]
}>()

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
const casterUnitType = ref('')
const casterRank = ref('')
const casterPath = ref('')
// Off by default: an untouched preview shows the ability acting alone, which is
// the honest baseline for what it does on its own.
const alliesAttack = ref(false)
// Off by default for the same reason: the baseline preview shows the ability
// alone. Ticked, the enemy scene units swing back — the only way to see a debuff
// that reduces an enemy's OWN outgoing damage.
const enemiesAttack = ref(false)

function onAlliesAttackChange(e: Event) {
  alliesAttack.value = (e.target as HTMLInputElement).checked
}

function onEnemiesAttackChange(e: Event) {
  enemiesAttack.value = (e.target as HTMLInputElement).checked
}

// Caster options come from the unit catalog, so the picker can never offer a
// unit that no longer exists. A failed fetch leaves the list empty, which
// degrades to "Default (adept) only" rather than to a wrong list — and the
// server independently falls back for an unknown type, so a stale selection
// still previews rather than erroring.
const casterOptions = ref<{ type: string; label: string }[]>([])
const pathsByUnit = ref<Record<string, string[]>>({})

// Paths belonging to the SELECTED caster (the catalog's own pathsByUnit map), so
// the two pickers can never describe a unit/path pair that doesn't exist. The
// default caster is the adept, so its paths show before anything is chosen.
const pathOptions = computed(() => pathsByUnit.value[casterUnitType.value || 'adept'] ?? [])

function humanizePath(id: string): string {
  return id
    .split('_')
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

async function loadCasterOptions() {
  try {
    const res = await fetch('/catalog/units')
    if (!res.ok) return
    const body = (await res.json()) as {
      units?: { type: string; name?: string }[]
      pathsByUnit?: Record<string, string[]>
    }
    casterOptions.value = (body.units ?? [])
      .map((u) => ({ type: u.type, label: u.name || u.type }))
      .sort((a, b) => a.label.localeCompare(b.label))
    pathsByUnit.value = body.pathsByUnit ?? {}
  } catch {
    // Offline / server down — see the comment above.
  }
}
void loadCasterOptions()

function onCasterUnitTypeChange(e: Event) {
  casterUnitType.value = (e.target as HTMLSelectElement).value
  // A path belongs to ONE unit, so a leftover selection would describe a pair
  // that doesn't exist. Clear it rather than send an incoherent request.
  casterPath.value = ''
  perkByRank.value = {}
}

function onCasterPathChange(e: Event) {
  casterPath.value = (e.target as HTMLSelectElement).value
  // Perks belong to a path, so a selection from the previous one is nonsense.
  perkByRank.value = {}
  // A path is EARNED at bronze — there is no such thing as a pathed unit at
  // base rank. Promote rather than leaving a pairing no real unit has.
  if (casterPath.value && !casterRank.value) casterRank.value = 'bronze'
}

function onCasterRankChange(e: Event) {
  casterRank.value = (e.target as HTMLSelectElement).value
  // Dropping to a lower rank retires the perks above it. selectedPerks() is
  // derived from the visible rows and would ignore them anyway, but leaving
  // them in the state means they silently reappear if the user goes back up —
  // a selection they never re-made.
  const next = { ...perkByRank.value }
  for (const { rank } of PERK_RANKS.slice(rankCap() + 1)) delete next[rank]
  perkByRank.value = next
}

// rankCap is the highest rank index the caster can carry perks from. A unit
// holds every perk it earned on the way up, so gold offers all three rows and
// bronze offers only its own.
//
// -1 means base rank, which carries no perks at all. That case never coincides
// with a visible perk row in practice — perk rows require a path, and choosing
// a path promotes to bronze (onCasterPathChange) — but the ordering still
// answers it correctly rather than relying on that.
function rankCap(): number {
  return PERK_RANKS.findIndex((r) => r.rank === casterRank.value)
}

// One perk per rank, which is what a unit can actually carry. Empty by default:
// an untouched preview shows what the ability does for a unit that owns
// nothing, the honest baseline.
const perkByRank = ref<Record<string, string>>({})

// perksByRankByPath is the catalog's own rank -> perk-ids map per promotion
// path. It is the SOLE source of which perks exist at which rank (a PerkDef
// carries no rank of its own — a path's perksByRank assigns it), so these
// dropdowns cannot drift from what a real promotion would offer.
const perksByRankByPath = ref<Record<string, Record<string, string[]>>>({})

async function loadPathPerks() {
  try {
    const res = await fetch('/catalog/paths')
    if (!res.ok) return
    const body = (await res.json()) as {
      paths?: { path: string; def?: { perksByRank?: Record<string, string[]> } }[]
    }
    const out: Record<string, Record<string, string[]>> = {}
    for (const p of body.paths ?? []) {
      if (p.def?.perksByRank) out[p.path] = p.def.perksByRank
    }
    perksByRankByPath.value = out
  } catch {
    // Offline / server down: the rank rows simply don't render.
  }
}
void loadPathPerks()

const PERK_RANKS = [
  { rank: 'bronze', label: 'Bronze Perk' },
  { rank: 'silver', label: 'Silver Perk' },
  { rank: 'gold', label: 'Gold Perk' },
] as const

// One row per rank the selected path actually grants perks at. A path with no
// silver bucket shows no Silver dropdown rather than an empty one.
const perkRankRows = computed(() => {
  const byRank = perksByRankByPath.value[casterPath.value]
  if (!byRank) return []
  const labelFor = new Map((props.perkOptions ?? []).map((p) => [p.id, p.label]))
  const cap = rankCap()
  return PERK_RANKS.flatMap(({ rank, label }, i) => {
    if (i > cap) return []
    const ids = byRank[rank] ?? []
    if (ids.length === 0) return []
    return [{
      rank,
      label,
      options: ids
        .map((id) => ({ id, label: labelFor.get(id) ?? id }))
        .sort((a, b) => a.label.localeCompare(b.label)),
    }]
  })
})

// The selected perks, in rank order, skipping ranks left at "None". Derived
// from the VISIBLE rows rather than from the raw per-rank state, so a perk that
// the chosen rank cannot carry can never ride along in the request — the rank
// filter is enforced by what gets emitted, not only by what gets drawn.
function selectedPerks(): string[] {
  return perkRankRows.value
    .map((row) => perkByRank.value[row.rank])
    .filter((id) => !!id)
}

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
  casterUnitType: casterUnitType.value,
  casterRank: casterRank.value,
  casterPath: casterPath.value,
  casterPerks: selectedPerks(),
  alliesAttack: alliesAttack.value,
  enemiesAttack: enemiesAttack.value,
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

function onPerkRankChange(rank: string, e: Event) {
  const id = (e.target as HTMLSelectElement).value
  const next = { ...perkByRank.value }
  if (id) next[rank] = id
  else delete next[rank]
  perkByRank.value = next
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

/* Same specificity reason as the number inputs above: without the row+element
   prefix the shell's width:100% control rule stretches the box. */
.pv-scene__row input.pv-scene__check {
  width: auto;
  min-width: 0;
  margin: 0;
}

.pv-scene__row select.pv-scene__select {
  width: auto;
  min-width: 92px;
}

/* The perk fields belong to the SAME wrapping row as caster/rank/path — they
   are more of the same kind of choice, not a section of their own. The wrapper
   survives only as a v-if + test hook, so display:contents lets its children
   participate in .pv-scene__row's flex flow directly instead of forming a
   nested box that always breaks to its own line. */
.pv-scene__perks {
  display: contents;
}

.pv-scene__hint {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
  font-style: italic;
}
</style>
