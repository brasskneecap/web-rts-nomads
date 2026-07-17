<template>
  <div class="pv-timeline" data-test="preview-timeline">
    <div class="pv-timeline__axis">
      <button
        v-for="(m, i) in markers"
        :key="i"
        type="button"
        class="pv-timeline__marker"
        :class="[
          `pv-timeline__marker--${m.color}`,
          { 'pv-timeline__marker--selected': i === selectedIndex, 'pv-timeline__marker--active': activeIndexSet.has(i) },
        ]"
        :style="{ left: `${m.leftPct}%`, top: `${m.rowPx}px` }"
        :title="`${humanizeTraceType(m.event.type)} @ ${m.event.t.toFixed(2)}s`"
        data-test="preview-timeline-marker"
        @click="emit('select', i)"
      />
    </div>
    <div class="pv-timeline__scale">
      <span>0s</span>
      <span>{{ duration.toFixed(1) }}s</span>
    </div>
  </div>
</template>

<script setup lang="ts">
// PreviewTimeline: a compact horizontal time axis with one marker per trace
// event, positioned at event.t / duration. Events sharing (approximately)
// the same t are stacked into different rows so their markers stay visually
// distinguishable instead of drawing exactly on top of one another.
//
// `activeIndices` (Task 8) is the canvas-playhead highlight — additive,
// independent of `selectedIndex` (the click-to-inspect selection), same
// contract as PreviewEventLog's identically-named prop.
import { computed } from 'vue'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import { humanizeTraceType, traceEventColor } from './traceEventDisplay'

const props = defineProps<{
  events: AbilityExecutionTraceEvent[]
  duration: number
  selectedIndex?: number
  activeIndices?: number[]
}>()

const emit = defineEmits<{ select: [index: number] }>()

const activeIndexSet = computed(() => new Set(props.activeIndices ?? []))

const ROW_HEIGHT = 7
// STACK_EPSILON_PCT: events land in the same "column" (and therefore stack
// into different rows) when their time-axis positions are within this many
// percentage points of each other — small enough that visually-distinct
// times never stack, large enough that near-simultaneous events (the common
// "several actions fire in the same instant" case) don't paint one marker
// directly on top of another.
const STACK_EPSILON_PCT = 1.5

interface Marker {
  event: AbilityExecutionTraceEvent
  leftPct: number
  rowPx: number
  color: string
}

const markers = computed<Marker[]>(() => {
  const duration = props.duration > 0 ? props.duration : 1
  const lastLeftPctByRow: number[] = []
  return props.events.map((event) => {
    const leftPct = Math.max(0, Math.min(100, (event.t / duration) * 100))
    // Find the first row whose last-placed marker is far enough away on the
    // axis that this one won't visually overlap it; otherwise open a new row.
    let row = lastLeftPctByRow.findIndex((prevPct) => Math.abs(prevPct - leftPct) >= STACK_EPSILON_PCT)
    if (row === -1) row = lastLeftPctByRow.length
    lastLeftPctByRow[row] = leftPct
    return { event, leftPct, rowPx: row * ROW_HEIGHT, color: traceEventColor(event.type) }
  })
})
</script>

<style scoped>
.pv-timeline {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.pv-timeline__axis {
  position: relative;
  height: 34px;
  border-bottom: 1px solid var(--ed-line);
  background: rgba(15, 23, 42, 0.2);
  border-radius: var(--ed-radius) var(--ed-radius) 0 0;
}

.pv-timeline__marker {
  position: absolute;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  transform: translateX(-50%);
  border: 1px solid rgba(0, 0, 0, 0.5);
  padding: 0;
}

.pv-timeline__marker--selected {
  outline: 2px solid var(--ed-brass);
  outline-offset: 1px;
}

.pv-timeline__marker--active {
  box-shadow: 0 0 0 2px var(--ed-ok);
}

.pv-timeline__marker--neutral { background: #a9b4c4; }
.pv-timeline__marker--blue { background: #6ea8e0; }
.pv-timeline__marker--red { background: var(--ed-danger); }
.pv-timeline__marker--green { background: var(--ed-ok); }
.pv-timeline__marker--amber { background: #e0b258; }
.pv-timeline__marker--muted { background: rgba(233, 219, 184, 0.35); }
.pv-timeline__marker--danger { background: var(--ed-danger); }

.pv-timeline__scale {
  display: flex;
  justify-content: space-between;
  font-size: 0.68rem;
  color: var(--ed-text-dim);
}
</style>
