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
          <span class="pv-log__type">{{ humanizeTraceType(row.event.type) }}</span>
          <span v-if="row.summary" class="pv-log__summary">{{ row.summary }}</span>
          <span v-if="row.event.type === 'action_skipped'" class="pv-log__deferred" data-test="preview-log-deferred">
            deferred
          </span>
        </button>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
// PreviewEventLog: the full execution trace, in order, as a filterable list.
// Rows with a `path` (an action-level event) are clickable — clicking emits
// `select(index)` (highlight this row), `selectNode(path)` (the panel
// resolves path -> NodeRef via refFromPath and focuses it in the
// flow/inspector), AND `seek(t)` (Task 8: the panel maps this event's sim
// time to a frame index and scrubs the replay canvas to it, pausing
// playback). Rows without a path (most trigger/flow-level events) render as
// plain, unclickable rows — none of the three emits fire for them.
//
// `activeIndices` (Task 8) is a SEPARATE, additive highlight driven by the
// canvas's playhead (events whose `t` falls in the currently-displayed
// frame's time window) — it never touches `selectedIndex`/the click-to-
// inspect selection above.
import { computed, ref } from 'vue'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import { humanizeTraceType, summarizeTraceEvent, traceEventCategory, traceEventColor, type TraceCategory } from './traceEventDisplay'

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
}

// rows: the FULL trace, in execution order, decorated with its original
// index (needed so `select`/`selectedIndex` stay anchored to the untouched
// trace even while a tab filter is narrowing what's rendered) and its
// best-effort summary.
const rows = computed<Row[]>(() =>
  props.events.map((event, index) => ({ index, event, summary: summarizeTraceEvent(event) })),
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

.pv-log__empty {
  margin: 0;
  font-size: 0.78rem;
  color: var(--ed-text-dim);
}

.pv-log__rows {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 260px;
  overflow-y: auto;
}

.pv-log__row {
  border-radius: var(--ed-radius);
  border: 1px solid transparent;
}

.pv-log__row--selected {
  border-color: var(--ed-brass);
  background: rgba(212, 168, 71, 0.1);
}

/* pv-log__row--active: the canvas-playhead highlight (Task 8) — deliberately
   a different visual channel (left edge accent) than --selected's full
   border/fill so the two can combine on the same row without one masking
   the other. */
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
  width: 100%;
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 4px 8px;
  background: none;
  border: 0;
  text-align: left;
  font-family: var(--font-body);
}

.pv-log__row-body:disabled {
  opacity: 0.85;
}

.pv-log__time {
  flex: 0 0 auto;
  font-size: 0.72rem;
  color: var(--ed-brass-dim);
  font-variant-numeric: tabular-nums;
}

.pv-log__type {
  flex: 0 0 auto;
  font-size: 0.78rem;
  font-weight: 600;
  color: var(--ed-text);
}

.pv-log__summary {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pv-log__deferred {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 1px 7px;
  font-size: 0.62rem;
  font-weight: 700;
  letter-spacing: 0.02em;
  white-space: nowrap;
  color: #e0b258;
  background: rgba(224, 178, 88, 0.14);
  border: 1px solid rgba(224, 178, 88, 0.4);
}
</style>
