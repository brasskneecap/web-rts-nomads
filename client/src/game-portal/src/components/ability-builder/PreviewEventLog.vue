<template>
  <div class="pv-log" data-test="preview-event-log">
    <div class="pv-log__tabs" role="tablist">
      <button
        v-for="tab in TABS"
        :key="tab.id"
        type="button"
        class="pv-log__tab"
        :class="{ 'pv-log__tab--active': tab.id === activeTab }"
        role="tab"
        :aria-selected="tab.id === activeTab"
        data-test="preview-log-tab"
        :data-tab="tab.id"
        @click="activeTab = tab.id"
      >{{ tab.label }}</button>
    </div>

    <!-- The log reads as a table: a fixed column-header row over aligned,
         scrolling rows. Time / Event / Details share one grid template so every
         row lines up under its header. -->
    <div class="pv-log__table">
      <div class="pv-log__cols" aria-hidden="true">
        <span>Time</span>
        <span>Event</span>
        <span>Details</span>
      </div>

      <p v-if="filtered.length === 0" class="pv-log__empty">No events in this filter.</p>

      <ul v-else class="pv-log__rows">
        <li
          v-for="row in filtered"
          :key="row.index"
          class="pv-log__row"
          :class="{
            'pv-log__row--selected': row.index === selectedIndex,
            'pv-log__row--skipped': row.event.type === 'action_skipped',
            'pv-log__row--danger': traceEventColor(row.event.type) === 'danger',
            'pv-log__row--active': activeIndexSet.has(row.index),
          }"
          data-test="preview-log-row"
        >
          <button
            type="button"
            class="pv-log__row-body"
            :disabled="!row.event.path"
            @click="onRowClick(row)"
          >
            <span class="pv-log__time">{{ row.event.t.toFixed(2) }}s</span>
            <span class="pv-log__event">
              <span class="pv-log__dot" :class="`pv-log__dot--${row.color}`" aria-hidden="true" />
              <span class="pv-log__type">{{ humanizeTraceType(row.event.type) }}</span>
            </span>
            <span class="pv-log__details">
              <span v-if="row.summary" class="pv-log__summary">{{ row.summary }}</span>
              <span
                v-if="row.event.type === 'action_skipped'"
                class="pv-log__deferred"
                data-test="preview-log-deferred"
              >deferred</span>
            </span>
          </button>
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
// PreviewEventLog: the full execution trace, in order, as a filterable TABLE
// (Time · Event · Details). Rows with a `path` (an action-level event) are
// clickable — clicking emits `select(index)` (highlight this row),
// `selectNode(path)` (the panel resolves path -> NodeRef via refFromPath and
// focuses it in the flow/inspector), AND `seek(t)` (the panel maps this event's
// sim time to a frame index and scrubs the replay canvas to it, pausing
// playback). Rows without a path render as plain, unclickable rows.
//
// `activeIndices` is a SEPARATE, additive highlight driven by the canvas's
// playhead (events whose `t` falls in the currently-displayed frame's time
// window) — it never touches `selectedIndex`/the click-to-inspect selection.
import { computed, ref } from 'vue'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import {
  humanizeTraceType,
  summarizeTraceEvent,
  traceEventCategory,
  traceEventColor,
  type TraceCategory,
  type TraceColor,
} from './traceEventDisplay'

const props = defineProps<{
  events: AbilityExecutionTraceEvent[]
  selectedIndex?: number
  /** Indices (into `events`) currently active at the canvas playhead. Additive highlight only. */
  activeIndices?: number[]
}>()

const emit = defineEmits<{
  select: [index: number]
  selectNode: [path: string]
  seek: [t: number]
}>()

const activeIndexSet = computed(() => new Set(props.activeIndices ?? []))

type TabId = 'all' | TraceCategory

const TABS: { id: TabId; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'damage', label: 'Damage' },
  { id: 'healing', label: 'Healing' },
  { id: 'targets', label: 'Targets' },
  { id: 'zones', label: 'Zones' },
  { id: 'skipped', label: 'Skipped' },
  { id: 'errors', label: 'Errors' },
]

const activeTab = ref<TabId>('all')

interface Row {
  index: number
  event: AbilityExecutionTraceEvent
  summary: string
  color: TraceColor
}

// rows: the FULL trace, in execution order, decorated with its original index
// (needed so `select`/`selectedIndex` stay anchored to the untouched trace even
// while a tab filter narrows what's rendered), its best-effort summary, and its
// category color (the leading dot).
const rows = computed<Row[]>(() =>
  props.events.map((event, index) => ({
    index,
    event,
    summary: summarizeTraceEvent(event),
    color: traceEventColor(event.type),
  })),
)

const filtered = computed<Row[]>(() => {
  if (activeTab.value === 'all') return rows.value
  return rows.value.filter((r) => traceEventCategory(r.event.type) === activeTab.value)
})

function onRowClick(row: Row) {
  emit('select', row.index)
  if (row.event.path) emit('selectNode', row.event.path)
  emit('seek', row.event.t)
}
</script>

<style scoped>
.pv-log {
  display: flex;
  flex-direction: column;
  gap: 8px;
  min-height: 0;
}

.pv-log__tabs {
  flex: 0 0 auto;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.pv-log__tab {
  padding: 3px 10px;
  font-family: var(--font-body);
  font-size: 0.72rem;
  font-weight: 600;
  letter-spacing: 0.03em;
  color: var(--ed-text-dim);
  background: rgba(15, 23, 42, 0.25);
  border: 1px solid var(--ed-line);
  border-radius: 999px;
}

.pv-log__tab:hover {
  color: var(--ed-brass);
  border-color: var(--ed-brass);
}

.pv-log__tab--active {
  color: #17120c;
  background: var(--ed-brass);
  border-color: var(--ed-brass);
}

/* The table shell: a bordered panel so the rows read as a log, not loose text.
   Shared column template drives both the header and every row. */
.pv-log__table {
  --pv-log-cols: 48px minmax(116px, 1.3fr) minmax(0, 2fr);
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
  background: rgba(8, 14, 24, 0.4);
  overflow: hidden;
}

.pv-log__cols {
  flex: 0 0 auto;
  display: grid;
  grid-template-columns: var(--pv-log-cols);
  gap: 10px;
  padding: 5px 10px;
  border-bottom: 1px solid var(--ed-line);
  background: rgba(15, 23, 42, 0.35);
  font-size: 0.6rem;
  font-weight: 700;
  letter-spacing: 0.1em;
  text-transform: uppercase;
  color: var(--ed-text-dim);
}

.pv-log__empty {
  margin: 0;
  padding: 10px;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.pv-log__rows {
  flex: 1 1 auto;
  min-height: 0;
  margin: 0;
  padding: 0;
  list-style: none;
  overflow-y: auto;
}

.pv-log__row {
  border-bottom: 1px solid rgba(120, 130, 150, 0.1);
}

.pv-log__row:last-child {
  border-bottom: none;
}

.pv-log__row--selected {
  background: rgba(212, 168, 71, 0.12);
}

/* The canvas-playhead highlight — a left-edge accent, a different visual
   channel than --selected's fill so the two can combine on one row. */
.pv-log__row--active {
  box-shadow: inset 3px 0 0 var(--ed-ok);
}

.pv-log__row--skipped .pv-log__type,
.pv-log__row--skipped .pv-log__summary {
  color: var(--ed-text-dim);
  font-style: italic;
}

.pv-log__row--danger .pv-log__type {
  color: var(--ed-danger);
}

.pv-log__row-body {
  display: grid;
  grid-template-columns: var(--pv-log-cols);
  gap: 10px;
  align-items: center;
  width: 100%;
  padding: 5px 10px;
  background: none;
  border: 0;
  text-align: left;
  font-family: var(--font-body);
}

.pv-log__row-body:hover:not(:disabled) {
  background: rgba(120, 130, 150, 0.08);
}

.pv-log__time {
  font-size: 0.7rem;
  color: var(--ed-brass-dim);
  font-variant-numeric: tabular-nums;
}

.pv-log__event {
  display: flex;
  align-items: center;
  gap: 7px;
  min-width: 0;
}

.pv-log__dot {
  flex: 0 0 auto;
  width: 8px;
  height: 8px;
  border-radius: 2px;
  transform: rotate(45deg);
}

.pv-log__type {
  font-size: 0.76rem;
  font-weight: 600;
  color: var(--ed-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pv-log__details {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.pv-log__summary {
  font-size: 0.74rem;
  color: var(--ed-text-dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pv-log__deferred {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 1px 7px;
  font-size: 0.6rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  white-space: nowrap;
  color: #e0b258;
  background: rgba(224, 178, 88, 0.14);
  border: 1px solid rgba(224, 178, 88, 0.4);
}

/* Category dot colors — the established trace palette (shared with the timeline). */
.pv-log__dot--neutral { background: #a9b4c4; }
.pv-log__dot--blue { background: #6ea8e0; }
.pv-log__dot--red { background: var(--ed-danger); }
.pv-log__dot--green { background: var(--ed-ok); }
.pv-log__dot--amber { background: #e0b258; }
.pv-log__dot--muted { background: rgba(233, 219, 184, 0.4); }
.pv-log__dot--danger { background: var(--ed-danger); }
</style>
