<template>
  <div class="ab-preview" data-test="ability-preview-panel">
    <!-- Preview Scene sits ABOVE the canvas (collapsible) so scene setup reads
         top-to-bottom into the canvas it configures, and can be folded away to
         give the canvas/timeline more room. -->
    <PreviewSceneControls :charge-required="chargeRequired" @update:model-value="onSceneConfigUpdate" />

    <!-- Canvas is ALWAYS mounted (Task 5) — the renderer is the top-most,
         always-visible element in the rail. It renders its own idle
         placeholder when `frames` is empty (no result yet, or a run that
         captured no frames), so it never depends on `result` being set.
         `result?.frames ?? []` is the only null-safety this reorder needed —
         every other prop below is already an independent ref/computed that
         defaults sanely with no result. -->
    <AbilityPreviewCanvas
      v-model:current-tick="currentTick"
      v-model:playing="canvasPlaying"
      :frames="framesForCanvas"
      :trace="result?.trace ?? []"
      :cast-range="overlayCastRange"
      :aoe-radius="overlayAoeRadius"
      :caster-x="canvasCasterX"
      :caster-y="canvasCasterY"
      :cast-x="canvasCastX"
      :cast-y="canvasCastY"
      :scene-units="sceneUnits"
      @update:scene-unit="onUpdateSceneUnit"
      @update:caster="onUpdateCaster"
    >
      <!-- Run / Edit live in the canvas's playback toolbar (same icon-button
           format as Play/Pause/Restart) via its leading slot. -->
      <template #controls-lead>
        <PreviewControlButton
          icon="run"
          :label="running ? 'Running…' : 'Run Preview'"
          :disabled="runDisabled"
          data-test="preview-run-button"
          @click="onRun"
        />
        <PreviewControlButton
          icon="edit"
          label="Edit Scene"
          :disabled="!result || running"
          data-test="preview-edit-scene-button"
          @click="onEditScene"
        />
      </template>
    </AbilityPreviewCanvas>

    <div class="ab-preview__body">
      <p v-if="runError" class="ab-preview__error" role="alert" data-test="preview-run-error">{{ runError }}</p>

      <div v-if="result" class="ab-preview__result">
        <div
          v-if="!result.runnable || result.warnings.length || result.error"
          class="ab-preview__banner"
          data-test="preview-warnings-banner"
        >
          <p v-if="skippedCount > 0">
            This preview ran the composable program; {{ skippedCount }} action(s) are display-only and were
            skipped — they'll run once a later phase lands.
          </p>
          <p v-else-if="!result.runnable">
            This ability isn't fully executable by the runtime yet — some of its behavior may not have run.
          </p>
          <ul v-if="result.warnings.length">
            <li v-for="(w, i) in result.warnings" :key="i">{{ w }}</li>
          </ul>
          <p v-if="result.error" class="ab-preview__banner-error" data-test="preview-result-error">{{ result.error }}</p>
        </div>

        <!-- Timeline and Event Log are two VIEWS of the same run, tabbed so
             only one claims vertical space at a time (they were stacking and
             squeezing each other on shorter screens). v-show keeps both mounted
             — the hidden one's display:none reclaims its layout space while its
             filter/scroll state survives a tab switch. -->
        <div class="ab-preview__view-tabs" role="tablist" aria-label="Run detail view">
          <button
            v-for="v in RESULT_VIEWS"
            :key="v.id"
            type="button"
            role="tab"
            class="ab-preview__view-tab"
            :class="{ 'ab-preview__view-tab--active': resultView === v.id }"
            :aria-selected="resultView === v.id"
            :data-test="`preview-view-tab-${v.id}`"
            @click="resultView = v.id"
          >{{ v.label }}</button>
        </div>

        <div class="ab-preview__views">
          <PreviewExecutionTimeline
            v-show="resultView === 'timeline'"
            :lanes="timeline.lanes"
            :axis-duration="timeline.axisDuration"
            :playhead-t="playheadT"
            :selected-path="selectedNodePath"
            @select="onTimelineSelect"
            @seek="onSeekEvent"
          />

          <PreviewEventLog
            v-show="resultView === 'log'"
            :events="result.trace"
            :selected-index="selectedIndex"
            :active-indices="activeEventIndices"
            @select="selectedIndex = $event"
            @select-node="onSelectNode"
            @seek="onSeekEvent"
          />
        </div>

        <div class="ab-preview__summary" data-test="preview-summary">
          <span class="ab-preview__summary-mana">Caster mana spent: {{ result.casterManaSpent }}</span>
          <ul class="ab-preview__unit-list">
            <li
              v-for="u in unitSummaries"
              :key="u.index"
              class="ab-preview__unit"
              :class="{ 'ab-preview__unit--healed': u.kind === 'healed', 'ab-preview__unit--damaged': u.kind === 'damaged' }"
            >
              <span class="ab-preview__unit-label">{{ u.label }}</span>
              <span class="ab-preview__unit-hp">{{ u.hpBefore }} → {{ u.hpAfter }}</span>
            </li>
          </ul>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// AbilityPreviewPanel: the Phase 6a payoff — Run Preview against a synthetic
// scene, then render the server's authoritative execution trace as a
// timeline + filterable event log, with per-unit HP deltas. Read-only w.r.t.
// builder state: it only ever calls builder.select(...) (to focus the flow
// node behind a clicked trace event), never form/program mutations — the
// preview request is assembled fresh from a READ of form+program+scene each
// run, never cached across edits.
import { computed, ref, watch } from 'vue'
import PreviewControlButton from './PreviewControlButton.vue'
import { saveRequestFromForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import { serializeProgram } from '@/game/abilities/program/abilityProgram'
import type { PreviewRequest, PreviewResult, PreviewSceneUnit } from '@/game/abilities/program/programPreview'
import { runAbilityPreview } from '@/game/abilities/abilityEditorApi'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import { refFromPath } from './refFromPath'
import type { NodePath, NodeRef } from './programTree'
import { buildExecutionTimeline } from './executionTimeline'
import { PREVIEW_FRAME_DT_SECONDS } from './previewPlayback'
import PreviewSceneControls, { type PreviewSceneConfig } from './PreviewSceneControls.vue'
import {
  DEFAULT_ALLY_HP,
  DEFAULT_ALLY_MAX_HP,
  DEFAULT_ENEMY_HP,
  DEFAULT_ENEMY_MAX_HP,
  PREVIEW_SCENE_ORIGIN,
  defaultAllyPosition,
  defaultEnemyPosition,
} from './previewScene'
import PreviewExecutionTimeline from './PreviewExecutionTimeline.vue'
import PreviewEventLog from './PreviewEventLog.vue'
import AbilityPreviewCanvas from './AbilityPreviewCanvas.vue'

const builder = useAbilityBuilderContext()

// result: the most recent server run. Non-null ⇒ the canvas is in REPLAY mode
// (frozen, non-draggable). Null ⇒ EDIT mode (units live + draggable). Declared
// up here (before the scene watches below) so those watches can clear it to
// drop back into edit mode when the scene is edited. Cleared by: a drag
// (onUpdateSceneUnit/onUpdateCaster), a count change (the reconcile watch), and
// the explicit Edit Scene button (onEditScene).
const result = ref<PreviewResult | null>(null)

// ── editable scene (Phase 6b: drag-to-place) ────────────────────────────
// The panel — not PreviewSceneControls — now owns the actual placed
// positions: `sceneUnits`/`casterPos` are mutated by DRAGGING on
// AbilityPreviewCanvas (see onUpdateSceneUnit/onUpdateCaster below).
// PreviewSceneControls only decides enemy/ally COUNTS + how the cast is
// aimed + seed/duration (see its own module doc comment); this panel
// reconciles `sceneUnits` against those counts, preserving whatever
// positions are already there (see reconcileSceneUnitCounts).
function buildDefaultSceneUnits(): PreviewSceneUnit[] {
  return [
    { team: 'enemy', ...defaultEnemyPosition(0), hp: DEFAULT_ENEMY_HP, maxHp: DEFAULT_ENEMY_MAX_HP },
    { team: 'ally', ...defaultAllyPosition(0), hp: DEFAULT_ALLY_HP, maxHp: DEFAULT_ALLY_MAX_HP },
  ]
}

const sceneUnits = ref<PreviewSceneUnit[]>(buildDefaultSceneUnits())
const casterPos = ref({ x: PREVIEW_SCENE_ORIGIN.x, y: PREVIEW_SCENE_ORIGIN.y })

// sceneConfig mirrors PreviewSceneControls' own defaults so the panel is
// runnable immediately on mount, before the child's own `immediate: true`
// watch has flushed its first emit — that emit still lands moments later
// with the SAME values, so this is just avoiding a "no scene yet" gap
// rather than a real default mismatch risk.
const sceneConfig = ref<PreviewSceneConfig>({
  enemyCount: 1,
  allyCount: 1,
  targetSelector: 'first_enemy',
  seed: 1,
  durationSeconds: 3,
  casterCharge: 0,
})

function onSceneConfigUpdate(v: PreviewSceneConfig) {
  sceneConfig.value = v
}

// chargeRequired: if the ability under preview is a charge-fire passive
// (arcane_missiles — an on_charge_full trigger with a charge_fire_volley
// action), surface its charge threshold so PreviewSceneControls can show a
// "Charge" input prefilled to it. Null for every other ability, which hides the
// input. Reading the threshold from the live program means it tracks edits to
// the action's chargeRequired field, not a stale snapshot.
const chargeRequired = computed<number | null>(() => {
  for (const trigger of builder.program.value.triggers ?? []) {
    if (trigger.type !== 'on_charge_full') continue
    for (const action of trigger.actions ?? []) {
      if (action.type !== 'charge_fire_volley') continue
      const req = action.config?.chargeRequired
      if (typeof req === 'number' && req > 0) return req
    }
  }
  return null
})

// reconcileSceneUnitCounts adds/removes scene units to match enemyCount/
// allyCount while PRESERVING every existing unit's position (and its place
// within its own team's group) — only a genuinely NEW unit gets a fresh
// default position; a removed unit is always the LAST one in its group.
function reconcileSceneUnitCounts(enemyCount: number, allyCount: number) {
  const enemies = sceneUnits.value.filter((u) => u.team === 'enemy')
  const allies = sceneUnits.value.filter((u) => u.team !== 'enemy')

  while (enemies.length < enemyCount) {
    enemies.push({ team: 'enemy', ...defaultEnemyPosition(enemies.length), hp: DEFAULT_ENEMY_HP, maxHp: DEFAULT_ENEMY_MAX_HP })
  }
  enemies.length = Math.min(enemies.length, enemyCount)

  while (allies.length < allyCount) {
    allies.push({ team: 'ally', ...defaultAllyPosition(allies.length), hp: DEFAULT_ALLY_HP, maxHp: DEFAULT_ALLY_MAX_HP })
  }
  allies.length = Math.min(allies.length, allyCount)

  sceneUnits.value = [...enemies, ...allies]
}

watch(
  () => [sceneConfig.value.enemyCount, sceneConfig.value.allyCount] as const,
  ([enemyCount, allyCount], old) => {
    reconcileSceneUnitCounts(enemyCount, allyCount)
    // A count change is a scene edit — like dragging, it returns the canvas to
    // edit mode (clears any showing run) so the added/removed units appear
    // immediately AND stay draggable, instead of being hidden behind a stale
    // replay until the next Run. `old === undefined` on the immediate mount
    // call (result is already null then), so this only fires on real changes.
    if (old) result.value = null
  },
  { immediate: true },
)

// onUpdateSceneUnit/onUpdateCaster: AbilityPreviewCanvas's drag emits. A drag
// always returns the canvas to edit mode (clearing `result`) even if a
// result was showing — dragging IS "go back and try a different geometry",
// so the stale replay/trace/summary from the last run must not linger
// alongside newly-edited positions it no longer describes.
function onUpdateSceneUnit(payload: { index: number; x: number; y: number }) {
  const existing = sceneUnits.value[payload.index]
  if (!existing) return
  const next = sceneUnits.value.slice()
  next[payload.index] = { ...existing, x: payload.x, y: payload.y }
  sceneUnits.value = next
  result.value = null
}

function onUpdateCaster(payload: { x: number; y: number }) {
  casterPos.value = { x: payload.x, y: payload.y }
  result.value = null
}

// onEditScene: leave a finished run's frozen replay and return to edit mode so
// the caster + scene units become live and draggable again (their last-placed
// positions are preserved — sceneUnits/casterPos are untouched). This is the
// explicit escape hatch the drag/count-change auto-resets don't cover: after a
// run you may want to reposition WITHOUT first nudging a unit or changing a
// count. The button is only shown while a result is present (see template).
function onEditScene() {
  result.value = null
  runError.value = ''
  selectedIndex.value = undefined
}

// POINT_CAST_OFFSET_X: how far in front of the caster the "Point" target
// selector's ground point sits, and where "First enemy"/"First ally" fall
// back to when that team is empty. Relative to the LIVE caster position (not
// a fixed world coordinate) so dragging the caster carries its point-cast
// target along with it.
const POINT_CAST_OFFSET_X = 150

// derived: the PreviewRequest's target/castX/castY fields, computed from the
// scene controls' targetSelector against the LIVE casterPos/sceneUnits — so
// dragging a unit around immediately updates what a unit-target ability
// would hit, and what a point-target ability's ground point is, without
// waiting for Run.
const derived = computed<{ target: number; castX: number; castY: number }>(() => {
  const caster = casterPos.value
  const pointCast = { x: caster.x + POINT_CAST_OFFSET_X, y: caster.y }
  const units = sceneUnits.value
  switch (sceneConfig.value.targetSelector) {
    case 'first_enemy': {
      const idx = units.findIndex((u) => u.team === 'enemy')
      return idx >= 0
        ? { target: idx, castX: units[idx].x, castY: units[idx].y }
        : { target: -1, castX: pointCast.x, castY: pointCast.y }
    }
    case 'first_ally': {
      const idx = units.findIndex((u) => u.team === 'ally')
      return idx >= 0
        ? { target: idx, castX: units[idx].x, castY: units[idx].y }
        : { target: -1, castX: pointCast.x, castY: pointCast.y }
    }
    case 'self':
      // The caster's own ground point follows the caster when dragged.
      return { target: -1, castX: caster.x, castY: caster.y }
    case 'point':
    default:
      return { target: -1, castX: pointCast.x, castY: pointCast.y }
  }
})

const running = ref(false)
const runError = ref('')
const selectedIndex = ref<number | undefined>(undefined)

// lastRequestDuration: the duration the MOST RECENTLY RUN preview used —
// captured at run time (not read live from `scene`, which the user may have
// since changed) so the timeline's axis always matches the trace it's
// displaying.
const lastRequestDuration = ref(3)

// currentTick: the panel-owned, canvas-controlled playhead (Task 8). Reset to
// 0 whenever a NEW preview result arrives (see the watch below) so a stale
// scrub position from a previous run never survives into the next replay.
const currentTick = ref(0)
// canvasPlaying: v-model'd into AbilityPreviewCanvas's own `playing` prop so
// clicking a trace event (onSeekEvent) can pause playback from here.
const canvasPlaying = ref(true)

// lastCasterX/Y, lastCastX/Y: captured from the SAME PreviewRequest actually
// sent to the server (same capture-at-run-time pattern as
// lastRequestDuration) so the overlay rings — and the canvas's replay of a
// COMPLETED run — never drift from what was simulated, even if the user
// drags the caster/units around after the run completes (which now works:
// dragging clears `result`, see onUpdateSceneUnit/onUpdateCaster above, so
// these frozen values are only ever read while a result is actually showing).
const lastCasterX = ref(0)
const lastCasterY = ref(0)
const lastCastX = ref(0)
const lastCastY = ref(0)

// framesForCanvas/canvasCasterX/Y/canvasCastX/Y: what AbilityPreviewCanvas
// actually receives. While a result is showing (replay mode), these are the
// FROZEN values captured at run time above — a real replay is 100%
// server-authoritative and must never drift from what was actually
// simulated. Before/without a result (edit mode — frames: []), these fall
// through to the LIVE, currently-editable casterPos/derived values instead,
// so dragging on the canvas and the cast-range/AoE overlay rings both track
// the scene as it's being placed, not stale defaults.
const framesForCanvas = computed(() => result.value?.frames ?? [])
const canvasCasterX = computed(() => (framesForCanvas.value.length > 0 ? lastCasterX.value : casterPos.value.x))
const canvasCasterY = computed(() => (framesForCanvas.value.length > 0 ? lastCasterY.value : casterPos.value.y))
const canvasCastX = computed(() => (framesForCanvas.value.length > 0 ? lastCastX.value : derived.value.castX))
const canvasCastY = computed(() => (framesForCanvas.value.length > 0 ? lastCastY.value : derived.value.castY))

// overlayCastRange/overlayAoeRadius: read live off the form under preview
// (castRange/radius are the ability def's own fields — see
// abilityEditorForm.ts). castRange can carry the 'match_attack_range'
// sentinel, which resolves server-side against the caster unit's own attack
// range — this client-only preview has no such unit to resolve it against,
// so that case (and any non-numeric radius) degrades to `undefined`; the
// overlay simply omits that ring rather than showing a wrong radius.
const overlayCastRange = computed(() => {
  const v = builder.form.value.castRange
  return typeof v === 'number' ? v : undefined
})
const overlayAoeRadius = computed(() => {
  const v = builder.form.value.radius
  return typeof v === 'number' ? v : undefined
})

// EPS: a tiny fudge factor for the t -> tick bucket floor below. 7 * 0.05
// (and similar tick*DT products) don't always land exactly back on their
// "true" decimal value in IEEE-754 (e.g. 7*0.05 === 0.35000000000000003),
// which would otherwise nudge an event whose t is EXACTLY tick*DT down into
// the previous tick's bucket. Far smaller than DT (0.05) so it never merges
// two genuinely distinct ticks.
const EPS = 1e-6

// NOTE: the seek↔highlight equivalence (clicking an event lands on the frame
// where it also highlights) holds ONLY because every trace event's `t` is
// stamped on the frame grid — the harness steps the sim at previewTickDT and
// captures one frame per step at the SAME granularity as PREVIEW_FRAME_DT_SECONDS,
// so event times are always exact multiples of DT and floor((t+EPS)/DT) ===
// round(t/DT). If frames are ever decimated (see ability_preview.go's
// "decimate at the handler" note) or the sim tick decouples from DT, event
// times stop aligning to frame indices and an upper-half-window event would
// seek one frame past its highlight. Revisit both mappings if that changes.

// frameIndexForTraceEvent buckets a trace event's sim time into the tick
// whose window [tick*DT, (tick+1)*DT) contains it — a floor, not the
// round-to-nearest mapping onSeekEvent below uses (seeking wants the
// nearest CAPTURED frame; this wants which window a continuous time falls
// in for the playhead highlight, per Task 8B).
function frameIndexForTraceEvent(t: number): number {
  return Math.floor((t + EPS) / PREVIEW_FRAME_DT_SECONDS)
}

// activeEventIndices: trace events "active at the playhead" — those whose
// `t` falls in [currentTick*DT, (currentTick+1)*DT). Additive highlight only;
// does not touch `selectedIndex` (the click-to-inspect selection).
const activeEventIndices = computed<number[]>(() => {
  if (!result.value) return []
  const indices: number[] = []
  result.value.trace.forEach((e, i) => {
    if (frameIndexForTraceEvent(e.t) === currentTick.value) indices.push(i)
  })
  return indices
})

// ── Execution timeline (Gantt) ──────────────────────────────────────────────
// timeline: the lane model for PreviewExecutionTimeline, derived from the LIVE
// program + the current run's trace (buildExecutionTimeline is pure). Empty
// lanes when there's no result yet — the component simply renders nothing.
const timeline = computed(() =>
  buildExecutionTimeline(builder.program.value, result.value?.trace ?? [], lastRequestDuration.value),
)

// playheadT: the replay's current time in seconds, for the timeline's playhead.
const playheadT = computed(() => currentTick.value * PREVIEW_FRAME_DT_SECONDS)

// resultView: which of the two run detail views is showing. Tabbed rather than
// stacked so they don't fight for vertical space on shorter screens. Defaults
// to the timeline (the at-a-glance view); the log is the drill-down.
type ResultView = 'timeline' | 'log'
const RESULT_VIEWS: { id: ResultView; label: string }[] = [
  { id: 'timeline', label: 'Execution Timeline' },
  { id: 'log', label: 'Event Log' },
]
const resultView = ref<ResultView>('timeline')

// selectedNodePath: the flow/inspector selection, so the timeline highlights the
// same lane. Null unless a trigger/action is selected (the ability node has no lane).
const selectedNodePath = computed<NodePath | null>(() => {
  const s = builder.selected.value
  return s.kind === 'trigger' || s.kind === 'action' ? s.path : null
})

// onTimelineSelect: clicking a lane focuses that node in the flow + inspector,
// the same as clicking it in the flow view (the timeline's seek is handled
// separately via @seek → onSeekEvent).
function onTimelineSelect(ref: NodeRef) {
  builder.select(ref)
}

// onSeekEvent: PreviewEventLog's `seek` emits the clicked event's raw sim
// time; map it to a frame index (inverse of frameIndexAt's DT stepping) and
// pause playback so the canvas holds still on the seeked frame. Clamped to
// the current result's frame range.
function onSeekEvent(t: number) {
  const frameCount = result.value?.frames.length ?? 0
  canvasPlaying.value = false
  if (frameCount === 0) {
    currentTick.value = 0
    return
  }
  const idx = Math.round(t / PREVIEW_FRAME_DT_SECONDS)
  currentTick.value = Math.min(frameCount - 1, Math.max(0, idx))
}

// No `!scene.value` gate anymore — sceneConfig/sceneUnits/casterPos are all
// initialized with real defaults above, never null, so the button is only
// ever gated on the builder/network busy states.
const runDisabled = computed(() => builder.busy.value || running.value)

async function onRun() {
  running.value = true
  runError.value = ''
  try {
    const ability = {
      ...saveRequestFromForm(builder.form.value),
      schemaVersion: 2,
      // serializeProgram's return is the wire shape; AuthoredAbilityDef.program
      // types it as AbilityProgram — same cast buildCandidateDef uses in
      // useAbilityBuilder.ts (this object only ever travels to JSON.stringify).
      program: serializeProgram(builder.program.value) as unknown as AbilityProgram,
    }
    const d = derived.value
    // casterX/Y come from the LIVE, user-draggable casterPos (Phase 6b) —
    // defaults to PREVIEW_SCENE_ORIGIN but follows wherever the caster was
    // dragged to. units/target/castX/Y likewise come from the live scene the
    // user placed, not a snapshot taken earlier.
    const req: PreviewRequest = {
      ability,
      casterX: casterPos.value.x,
      casterY: casterPos.value.y,
      units: sceneUnits.value,
      target: d.target,
      castX: d.castX,
      castY: d.castY,
      casterCharge: sceneConfig.value.casterCharge,
      seed: sceneConfig.value.seed,
      durationSeconds: sceneConfig.value.durationSeconds,
    }
    lastRequestDuration.value = req.durationSeconds > 0 ? req.durationSeconds : 3
    lastCasterX.value = req.casterX
    lastCasterY.value = req.casterY
    lastCastX.value = req.castX
    lastCastY.value = req.castY
    const res = await runAbilityPreview(req)
    result.value = res
    selectedIndex.value = undefined
    if (res.error) runError.value = res.error
  } catch (e) {
    result.value = null
    runError.value = e instanceof Error ? e.message : String(e)
  } finally {
    running.value = false
  }
}

// onSelectNode: PreviewEventLog's `selectNode` carries the clicked row's raw
// trace path; refFromPath resolves it against the CURRENT program (not a
// snapshot from when the preview ran) since that's what the flow/inspector
// are showing. refFromPath understands both the trace's id grammar ("t1",
// "t1.actions[a1]") and validateAbilityProgram's index grammar
// ("triggers[0]", "triggers[0].actions[1]") — see its doc comment. A path
// this program no longer has a matching trigger/action for (e.g. it was
// deleted since the preview ran) is a silent no-op — the row still
// highlights via `select`, it just can't jump the flow view too. Some
// leaf trace events (e.g. damage_applied) may still carry no path at all
// until a pending server-side change adds one; those rows are likewise a
// no-op for the flow jump, highlight-only.
function onSelectNode(path: string) {
  const ref = refFromPath(builder.program.value, path)
  if (ref) builder.select(ref)
}

// A new preview result (including a re-run, or the error path's `null`)
// snaps the playhead back to the start of the replay.
watch(result, () => {
  currentTick.value = 0
})

// Switching to a DIFFERENT ability must drop any run still on screen: a replay
// belongs to the ability that produced it, so leaving (e.g.) Arcane Missiles'
// orbs replaying while Fireball is now selected is stale and wrong. Clearing
// `result` returns the canvas to edit mode for the newly-selected ability's
// scene. Keyed on the ability id (not the program) so ongoing edits to the
// SAME ability don't wipe a result mid-review — only an actual ability switch
// does. No `immediate`, so this never fires on first mount.
watch(
  () => builder.form.value.id,
  () => {
    result.value = null
    runError.value = ''
    selectedIndex.value = undefined
  },
)

const skippedCount = computed(() => result.value?.trace.filter((e) => e.type === 'action_skipped').length ?? 0)

type UnitKind = 'healed' | 'damaged' | 'unchanged'
interface UnitSummary {
  index: number
  label: string
  hpBefore: number
  hpAfter: number
  kind: UnitKind
}

const unitSummaries = computed<UnitSummary[]>(() => {
  const units = result.value?.units ?? []
  const teamSeen: Record<string, number> = {}
  return units.map((u) => {
    const n = (teamSeen[u.team] ?? 0) + 1
    teamSeen[u.team] = n
    const teamLabel = u.team.charAt(0).toUpperCase() + u.team.slice(1)
    const kind: UnitKind = u.hpAfter > u.hpBefore ? 'healed' : u.hpAfter < u.hpBefore ? 'damaged' : 'unchanged'
    return { index: u.index, label: `${teamLabel} ${n}`, hpBefore: u.hpBefore, hpAfter: u.hpAfter, kind }
  })
})
</script>

<style scoped>
/* Root fills whatever height the rail column gives it (Task 5: the panel now
   lives in a tall narrow-ish column, not a tab). The canvas below is a
   non-shrinking top item at its own fixed/aspect height (see
   AbilityPreviewCanvas's `__stage`); `.ab-preview__scroll` is the only
   flexible/scrolling region, so a long trace log never squeezes the
   renderer. Harmless when unconstrained (standalone mount, tests): with no
   parent height, `height: 100%` resolves against auto and the flex column
   just sizes to content, same as before this change. */
.ab-preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
  min-height: 0;
}

/* The region under the canvas fills the remaining rail height. It no longer
   scrolls as a whole — instead the active run view (timeline / event log) grows
   to fill and scrolls internally, so the log/timeline use the freed space
   rather than being capped and pushing a page scroll. */
.ab-preview__body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1 1 auto;
  min-height: 0;
}

/* The run result is a flex column that fills the body: banner + view tabs sit
   at fixed height, the views container grows, the summary pins to the bottom. */
.ab-preview__result {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1 1 auto;
  min-height: 0;
}

/* Holds the two v-show'd views; whichever is visible grows to fill and its own
   internal rows scroll (see PreviewEventLog/PreviewExecutionTimeline). */
.ab-preview__views {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-height: 0;
}

.ab-preview__views > :deep(*) {
  flex: 1 1 auto;
  min-height: 0;
}

/* Segmented view switcher for Timeline vs Event Log — sits on a hairline that
   reads as the top edge of the view below it, matching the editor's tab strips. */
.ab-preview__view-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--ed-line);
}

.ab-preview__view-tab {
  padding: 5px 12px;
  font-family: var(--font-title);
  font-size: 0.7rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid transparent;
  border-bottom: none;
  border-radius: var(--ed-radius) var(--ed-radius) 0 0;
  margin-bottom: -1px;
}

.ab-preview__view-tab:hover {
  color: var(--ed-brass);
}

.ab-preview__view-tab--active {
  color: var(--ed-brass);
  border-color: var(--ed-line);
  border-bottom-color: transparent;
  background: rgba(212, 168, 71, 0.08);
}

.ab-preview__error {
  margin: 0;
  color: var(--ed-danger);
  font-size: 0.82rem;
}

.ab-preview__banner {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  color: #e0b258;
  background: rgba(224, 178, 88, 0.1);
  border: 1px solid rgba(224, 178, 88, 0.3);
  border-radius: var(--ed-radius);
  font-size: 0.8rem;
}

.ab-preview__banner p {
  margin: 0;
}

.ab-preview__banner ul {
  margin: 0;
  padding-left: 18px;
}

.ab-preview__banner-error {
  color: var(--ed-danger);
  font-weight: 600;
}

.ab-preview__summary {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(15, 23, 42, 0.2);
}

.ab-preview__summary-mana {
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.ab-preview__unit-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-wrap: wrap;
  gap: 8px 16px;
}

.ab-preview__unit {
  display: flex;
  align-items: baseline;
  gap: 6px;
  font-size: 0.8rem;
}

.ab-preview__unit-label {
  color: var(--ed-text-dim);
}

.ab-preview__unit-hp {
  font-weight: 600;
  color: var(--ed-text);
}

.ab-preview__unit--healed .ab-preview__unit-hp {
  color: var(--ed-ok);
}

.ab-preview__unit--damaged .ab-preview__unit-hp {
  color: var(--ed-danger);
}
</style>
