<template>
  <div class="ab-preview" data-test="ability-preview-panel">
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
      :frames="result?.frames ?? []"
      :cast-range="overlayCastRange"
      :aoe-radius="overlayAoeRadius"
      :caster-x="lastCasterX"
      :caster-y="lastCasterY"
      :cast-x="lastCastX"
      :cast-y="lastCastY"
    />

    <div class="ab-preview__scroll">
      <div class="ab-preview__run-row">
        <UiButton
          size="sm"
          variant="active"
          data-test="preview-run-button"
          :disabled="runDisabled"
          @click="onRun"
        >{{ running ? 'Running…' : 'Run Preview' }}</UiButton>
        <span v-if="running" class="ab-preview__running-hint">Simulating on the server…</span>
      </div>

      <PreviewSceneControls @update:model-value="onSceneUpdate" />

      <p v-if="runError" class="ab-preview__error" role="alert" data-test="preview-run-error">{{ runError }}</p>

      <template v-if="result">
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

        <PreviewTimeline
          :events="result.trace"
          :duration="lastRequestDuration"
          :selected-index="selectedIndex"
          :active-indices="activeEventIndices"
          @select="selectedIndex = $event"
        />

        <PreviewEventLog
          :events="result.trace"
          :selected-index="selectedIndex"
          :active-indices="activeEventIndices"
          @select="selectedIndex = $event"
          @select-node="onSelectNode"
          @seek="onSeekEvent"
        />

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
      </template>
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
import UiButton from '@/components/ui/UiButton.vue'
import { saveRequestFromForm } from '@/game/abilities/abilityEditorForm'
import type { AbilityProgram } from '@/game/abilities/program/abilityProgram'
import { serializeProgram } from '@/game/abilities/program/abilityProgram'
import type { PreviewRequest, PreviewResult } from '@/game/abilities/program/programPreview'
import { runAbilityPreview } from '@/game/abilities/abilityEditorApi'
import { useAbilityBuilderContext } from './AbilityBuilderContext'
import { refFromPath } from './refFromPath'
import { PREVIEW_FRAME_DT_SECONDS } from './previewPlayback'
import PreviewSceneControls, { type PreviewScene } from './PreviewSceneControls.vue'
import { PREVIEW_SCENE_ORIGIN } from './previewScene'
import PreviewTimeline from './PreviewTimeline.vue'
import PreviewEventLog from './PreviewEventLog.vue'
import AbilityPreviewCanvas from './AbilityPreviewCanvas.vue'

const builder = useAbilityBuilderContext()

// scene starts undefined-safe: PreviewSceneControls emits its own default
// scene synchronously during its own setup (a `watch(..., {immediate:true})`
// on its internal state), so by the time onRun can actually be invoked (a
// user click, always after mount) `scene.value` is populated. The `?? null`
// guard in runDisabled/onRun covers the theoretical gap regardless.
const scene = ref<PreviewScene | null>(null)

const result = ref<PreviewResult | null>(null)
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
// lastRequestDuration) so the overlay rings never drift from what was
// simulated, even if the user tweaks scene controls after the run completes.
const lastCasterX = ref(0)
const lastCasterY = ref(0)
const lastCastX = ref(0)
const lastCastY = ref(0)

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

const runDisabled = computed(() => builder.busy.value || running.value || !scene.value)

function onSceneUpdate(v: PreviewScene) {
  scene.value = v
}

async function onRun() {
  if (!scene.value) return
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
    // The caster spawns at the scene origin, not the world origin — the map's
    // terrain starts at (0,0) and the scene is laid out around the caster, so
    // (0,0) would put the caster on the map's corner and its allies off-map.
    // PreviewSceneControls lays its units out around this same constant.
    const req: PreviewRequest = {
      ability,
      casterX: PREVIEW_SCENE_ORIGIN.x,
      casterY: PREVIEW_SCENE_ORIGIN.y,
      ...scene.value,
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

.ab-preview__scroll {
  display: flex;
  flex-direction: column;
  gap: 12px;
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
}

.ab-preview__run-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ab-preview__running-hint {
  font-size: 0.78rem;
  color: var(--ed-text-dim);
  font-style: italic;
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
