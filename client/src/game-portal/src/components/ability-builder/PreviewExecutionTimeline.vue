<template>
  <div class="pv-exec" data-test="preview-execution-timeline">
    <!-- Ruler: a gutter spacer aligning with the label column, then time ticks
         positioned inside the track column (same coordinate space as bars). -->
    <div class="pv-exec__ruler">
      <div class="pv-exec__gutter" aria-hidden="true" />
      <div
        ref="rulerTrackEl"
        class="pv-exec__track pv-exec__track--ruler"
        @pointerdown="onScrubDown($event, null)"
      >
        <span
          v-for="tick in ticks"
          :key="tick"
          class="pv-exec__tick"
          :style="{ left: `${(tick / axisDuration) * 100}%` }"
        >{{ formatTick(tick) }}</span>
      </div>
    </div>

    <div class="pv-exec__rows">
      <div
        v-for="lane in lanes"
        :key="lane.key"
        class="pv-exec__row"
        :class="{
          'pv-exec__row--selected': isSelected(lane),
          'pv-exec__row--unfired': !lane.fired,
        }"
        data-test="preview-execution-lane"
      >
        <button
          type="button"
          class="pv-exec__label"
          :style="{ paddingLeft: `${8 + lane.depth * 14}px` }"
          :title="lane.label"
          @click="onSelect(lane)"
        >
          <span class="pv-exec__dot" :class="`pv-exec__cat--${lane.category}`" />
          <span class="pv-exec__label-text">{{ lane.label }}</span>
        </button>

        <div class="pv-exec__track" @pointerdown="onScrubDown($event, lane)">
          <span
            v-if="lane.startT !== null && lane.endT !== null && lane.endT > lane.startT"
            class="pv-exec__bar"
            :class="`pv-exec__cat--${lane.category}`"
            :style="barStyle(lane)"
          />
          <span
            v-for="(m, i) in lane.markers"
            :key="i"
            class="pv-exec__marker"
            :class="`pv-exec__cat--${lane.category}`"
            :style="{ left: `${(m / axisDuration) * 100}%` }"
          />
        </div>
      </div>

      <!-- Playhead spans every row; offset past the gutter, then a fraction of
           the remaining (track) width. -->
      <div
        v-if="playheadT >= 0 && playheadT <= axisDuration"
        class="pv-exec__playhead"
        :style="{ left: `calc(var(--pv-exec-gutter) + (100% - var(--pv-exec-gutter)) * ${playheadFrac})` }"
      />
    </div>

    <div class="pv-exec__legend">
      <span v-for="item in LEGEND" :key="item.category" class="pv-exec__legend-item">
        <span class="pv-exec__dot" :class="`pv-exec__cat--${item.category}`" />
        {{ item.label }}
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
// PreviewExecutionTimeline: a Gantt-style view of a preview run — one lane per
// program node (trigger/action), a duration bar or discrete markers per lane,
// a time ruler, a playhead synced to the replay, and a category legend. All the
// timing derivation lives in the pure buildExecutionTimeline transform; this
// component only lays lanes out and handles selection + click/drag-to-scrub.
import { computed, onBeforeUnmount, ref } from 'vue'
import type { TimelineLane, LaneCategory } from './executionTimeline'
import { pathsEqual, type NodePath, type NodeRef } from './programTree'

const props = defineProps<{
  lanes: TimelineLane[]
  axisDuration: number
  /** Current replay time (seconds) for the playhead. Negative hides it. */
  playheadT: number
  /** The node currently selected in the flow/inspector, highlighted here. */
  selectedPath?: NodePath | null
}>()

const emit = defineEmits<{
  select: [ref: NodeRef]
  seek: [t: number]
}>()

const LEGEND: { category: LaneCategory; label: string }[] = [
  { category: 'trigger', label: 'Trigger' },
  { category: 'action', label: 'Action' },
  { category: 'damage', label: 'Damage' },
  { category: 'heal', label: 'Heal' },
  { category: 'targets', label: 'Targets' },
  { category: 'zone', label: 'Zone / Status' },
  { category: 'presentation', label: 'Presentation' },
]

const playheadFrac = computed(() => {
  const d = props.axisDuration > 0 ? props.axisDuration : 1
  return Math.min(1, Math.max(0, props.playheadT / d))
})

// Ruler ticks: pick a step giving ~5–8 labels for the axis length.
const ticks = computed<number[]>(() => {
  const d = props.axisDuration > 0 ? props.axisDuration : 1
  const step = d <= 2 ? 0.5 : d <= 6 ? 1 : d <= 15 ? 2 : 5
  const out: number[] = []
  for (let t = 0; t <= d + 1e-6; t += step) out.push(Number(t.toFixed(3)))
  return out
})

function formatTick(t: number): string {
  return `${t % 1 === 0 ? t.toFixed(0) : t.toFixed(1)}s`
}

function barStyle(lane: TimelineLane): Record<string, string> {
  const d = props.axisDuration > 0 ? props.axisDuration : 1
  const left = (lane.startT! / d) * 100
  const width = ((lane.endT! - lane.startT!) / d) * 100
  return { left: `${left}%`, width: `${width}%` }
}

function isSelected(lane: TimelineLane): boolean {
  return !!props.selectedPath && pathsEqual(lane.nodePath, props.selectedPath)
}

function onSelect(lane: TimelineLane) {
  emit('select', { kind: lane.kind, path: lane.nodePath } as NodeRef)
  if (lane.startT !== null) emit('seek', lane.startT)
}

// ── click / drag to scrub the playhead ──────────────────────────────────────
// pointerdown on the ruler or any lane track begins a scrub: seek to the time
// under the cursor and keep seeking while the pointer drags. Move/up are tracked
// at the window level so the drag survives leaving the element (no pointer
// capture needed). A pointerdown that DOESN'T drag is treated as a click and
// also selects that lane's node (a null lane = the ruler, which selects
// nothing). Every track column shares the ruler track's left/width, so its rect
// is the one x→time reference for all rows.
const rulerTrackEl = ref<HTMLElement | null>(null)
let scrubbing = false
let scrubMoved = false
let scrubStartX = 0
let scrubLane: TimelineLane | null = null

function seekAtClientX(clientX: number) {
  const el = rulerTrackEl.value
  if (!el) return
  const rect = el.getBoundingClientRect()
  if (rect.width <= 0) return
  const frac = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
  emit('seek', frac * props.axisDuration)
}

function onScrubMove(e: PointerEvent) {
  if (!scrubbing) return
  if (Math.abs(e.clientX - scrubStartX) > 3) scrubMoved = true
  seekAtClientX(e.clientX)
}

function detachScrubListeners() {
  window.removeEventListener('pointermove', onScrubMove)
  window.removeEventListener('pointerup', onScrubUp)
}

function onScrubUp() {
  if (!scrubbing) return
  scrubbing = false
  detachScrubListeners()
  // A pure click (no drag) on a lane track also selects its node.
  if (!scrubMoved && scrubLane) {
    emit('select', { kind: scrubLane.kind, path: scrubLane.nodePath } as NodeRef)
  }
  scrubLane = null
}

function onScrubDown(e: PointerEvent, lane: TimelineLane | null) {
  if (e.button !== 0) return // left button only
  scrubbing = true
  scrubMoved = false
  scrubStartX = e.clientX
  scrubLane = lane
  seekAtClientX(e.clientX)
  window.addEventListener('pointermove', onScrubMove)
  window.addEventListener('pointerup', onScrubUp)
  e.preventDefault() // suppress text selection while dragging
}

onBeforeUnmount(detachScrubListeners)
</script>

<style scoped>
.pv-exec {
  --pv-exec-gutter: 150px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-height: 0;
  font-family: var(--font-body);
}

/* Ruler + every row share this two-column shape so ticks, bars, markers and the
   playhead all live in the same track coordinate space. */
.pv-exec__ruler,
.pv-exec__row {
  display: grid;
  grid-template-columns: var(--pv-exec-gutter) 1fr;
  align-items: center;
}

.pv-exec__gutter {
  height: 1px;
}

.pv-exec__track {
  position: relative;
  height: 22px;
  border-left: 1px solid var(--ed-line);
  /* Horizontal drag scrubs the playhead — don't let touch turn it into a scroll. */
  touch-action: none;
}

.pv-exec__track--ruler {
  height: 16px;
  border-left: none;
}

.pv-exec__tick {
  position: absolute;
  top: 0;
  transform: translateX(-50%);
  font-size: 0.62rem;
  font-variant-numeric: tabular-nums;
  color: var(--ed-text-dim);
  white-space: nowrap;
}
/* Keep the 0s label from clipping off the left edge. */
.pv-exec__tick:first-child {
  transform: translateX(0);
}

.pv-exec__rows {
  position: relative;
  flex: 1 1 auto;
  min-height: 0;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--ed-line);
}

.pv-exec__row {
  border-bottom: 1px solid rgba(120, 130, 150, 0.12);
}

.pv-exec__row--selected {
  background: rgba(212, 168, 71, 0.1);
}

.pv-exec__row--unfired {
  opacity: 0.5;
}

.pv-exec__label {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 0 8px;
  background: none;
  border: 0;
  text-align: left;
  color: var(--ed-text);
  font: inherit;
  font-size: 0.74rem;
  overflow: hidden;
}

.pv-exec__label-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pv-exec__dot {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  border-radius: 2px;
  transform: rotate(45deg);
}

.pv-exec__bar {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  height: 10px;
  min-width: 3px;
  border-radius: 3px;
  opacity: 0.85;
}

.pv-exec__marker {
  position: absolute;
  top: 50%;
  width: 9px;
  height: 9px;
  margin-left: -4.5px;
  transform: translateY(-50%) rotate(45deg);
  border: 1px solid rgba(0, 0, 0, 0.45);
}

.pv-exec__playhead {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  background: #6ea8e0;
  box-shadow: 0 0 4px rgba(110, 168, 224, 0.7);
  pointer-events: none;
}

.pv-exec__legend {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 14px;
  padding-top: 2px;
  font-size: 0.66rem;
  color: var(--ed-text-dim);
}

.pv-exec__legend-item {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

/* Category colors — the established trace palette (kept within the current
   theme, not the mockup's warmer tones). Shared by dots, bars, and markers. */
.pv-exec__cat--trigger { background: #a9b4c4; }
.pv-exec__cat--action { background: #6b7a91; }
.pv-exec__cat--damage { background: var(--ed-danger); }
.pv-exec__cat--heal { background: var(--ed-ok); }
.pv-exec__cat--targets { background: #6ea8e0; }
.pv-exec__cat--zone { background: #e0b258; }
.pv-exec__cat--status { background: #e0b258; }
.pv-exec__cat--presentation { background: #9887c0; }
</style>
