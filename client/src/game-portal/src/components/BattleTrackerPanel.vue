<!--
  BattleTrackerPanel — debug HUD for per-player damage/kill totals.

  Rendered ONLY when the active map has `debug.battleTracker: true` in its
  catalog JSON. The server streams a BattleTrackerSnapshot with every match
  snapshot when the flag is on; we display it here with live updates.

  Features:
    - Collapsible header so the panel can be minimized while keeping the live
      totals one click away.
    - Save button: captures the current tracker state + metadata and stores
      it in localStorage under `webrts.battleLogs`.
    - Review button: opens a modal listing all saved runs with expand / delete.
    - Clear button: wipes in-match accumulation (client-side only — the
      server keeps its totals; re-enabling the snapshot view shows them again).

  No styling dependencies on the rest of the HUD beyond matching the brown
  amber palette.
-->
<template>
  <div v-if="ui.debugBattleTracker" class="battle-tracker" :class="{ collapsed, dragging: drag.dragging.value }" :style="drag.style.value">
    <header class="bt-head" :class="{ 'bt-head--dragging': drag.dragging.value }" v-bind="drag.handleBindings" aria-label="Drag to move">
      <span class="bt-grip" aria-hidden="true">⋮⋮</span>
      <button
        class="bt-toggle"
        type="button"
        :aria-expanded="!collapsed"
        :title="collapsed ? 'Expand Battle Tracker' : 'Collapse Battle Tracker'"
        @click="collapsed = !collapsed"
      >
        <span class="bt-chevron" :class="{ open: !collapsed }">▾</span>
        <span class="bt-title">Battle Tracker</span>
        <span class="bt-elapsed">{{ elapsedText }}</span>
      </button>
      <div v-if="!collapsed" class="bt-actions">
        <button class="bt-btn" type="button" title="Save snapshot to local storage" @click="onSave">Save</button>
        <button class="bt-btn" type="button" title="Review saved snapshots" @click="reviewOpen = true">Review</button>
      </div>
    </header>

    <div v-if="!collapsed" class="bt-body">
      <template v-if="ui.battleTracker && ui.battleTracker.players.length > 0">
        <section
          v-for="p in ui.battleTracker.players"
          :key="p.playerId"
          class="bt-player"
        >
          <div class="bt-player-head">
            <span class="bt-player-name">{{ formatPlayerId(p.playerId) }}</span>
            <span class="bt-player-total">
              {{ p.total.damageDealt.toLocaleString() }} dmg · {{ p.total.kills }} kills
            </span>
          </div>
          <table class="bt-table">
            <thead>
              <tr>
                <th class="bt-col-kind">Kind</th>
                <th class="bt-col-subtype">Source</th>
                <th class="bt-col-num">Damage</th>
                <th class="bt-col-num">Kills</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="b in p.buckets" :key="b.kind + ':' + b.subtype">
                <td class="bt-col-kind">{{ b.kind }}</td>
                <td class="bt-col-subtype">{{ b.subtype }}</td>
                <td class="bt-col-num">{{ b.stats.damageDealt.toLocaleString() }}</td>
                <td class="bt-col-num">{{ b.stats.kills }}</td>
              </tr>
            </tbody>
          </table>
        </section>
      </template>
      <div v-else class="bt-empty">No combat data yet.</div>
    </div>

    <!-- Review modal — overlays whole screen, shows saved runs. -->
    <div v-if="reviewOpen" class="bt-modal-backdrop" role="dialog" aria-modal="true" @click.self="reviewOpen = false">
      <div class="bt-modal">
        <header class="bt-modal-head">
          <span class="bt-modal-title">Saved Battle Logs</span>
          <button class="bt-btn" type="button" @click="reviewOpen = false">Close</button>
        </header>
        <div v-if="savedLogs.length === 0" class="bt-empty">No saved logs yet. Click Save to capture the current match.</div>
        <ul v-else class="bt-log-list">
          <li v-for="log in savedLogsSorted" :key="log.id" class="bt-log-item">
            <div class="bt-log-head">
              <button class="bt-log-toggle" type="button" @click="toggleExpand(log.id)">
                <span class="bt-chevron" :class="{ open: expandedIds.has(log.id) }">▾</span>
                <span class="bt-log-name">{{ log.name }}</span>
              </button>
              <span class="bt-log-meta">
                {{ log.mapName || log.mapId }} · {{ formatElapsed(log.elapsedSeconds) }} · {{ formatSavedAt(log.savedAt) }}
              </span>
              <button class="bt-btn bt-btn-danger" type="button" title="Delete this saved log" @click="onDelete(log.id)">Delete</button>
            </div>
            <div v-if="expandedIds.has(log.id)" class="bt-log-body">
              <section v-for="p in log.data.players" :key="p.playerId" class="bt-player">
                <div class="bt-player-head">
                  <span class="bt-player-name">{{ formatPlayerId(p.playerId) }}</span>
                  <span class="bt-player-total">
                    {{ p.total.damageDealt.toLocaleString() }} dmg · {{ p.total.kills }} kills
                  </span>
                </div>
                <table class="bt-table">
                  <thead>
                    <tr>
                      <th class="bt-col-kind">Kind</th>
                      <th class="bt-col-subtype">Source</th>
                      <th class="bt-col-num">Damage</th>
                      <th class="bt-col-num">Kills</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="b in p.buckets" :key="b.kind + ':' + b.subtype">
                      <td class="bt-col-kind">{{ b.kind }}</td>
                      <td class="bt-col-subtype">{{ b.subtype }}</td>
                      <td class="bt-col-num">{{ b.stats.damageDealt.toLocaleString() }}</td>
                      <td class="bt-col-num">{{ b.stats.kills }}</td>
                    </tr>
                  </tbody>
                </table>
              </section>
            </div>
          </li>
        </ul>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { GameUiSnapshot } from '@/game/core/GameClient'
import type { BattleTrackerSnapshot } from '@/game/network/protocol'
import { useDraggablePanel } from '@/composables/useDraggablePanel'

const props = defineProps<{
  ui: GameUiSnapshot
}>()

const collapsed = ref(false)
const drag = useDraggablePanel('battle-tracker')
const reviewOpen = ref(false)
const expandedIds = ref<Set<string>>(new Set())

// ── Saved-logs persistence ───────────────────────────────────────────────
// localStorage is the intentional storage: matches local to this browser,
// reviewable across sessions, no server round-trips. The key is namespaced
// so other webrts tools don't collide.
const STORAGE_KEY = 'webrts.battleLogs'

type SavedLog = {
  id: string
  name: string
  savedAt: number      // epoch ms
  mapId: string
  mapName: string
  elapsedSeconds: number
  data: BattleTrackerSnapshot
}

const savedLogs = ref<SavedLog[]>([])

function loadSavedLogs() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) {
      savedLogs.value = []
      return
    }
    const parsed = JSON.parse(raw) as unknown
    if (Array.isArray(parsed)) {
      savedLogs.value = parsed as SavedLog[]
    } else {
      savedLogs.value = []
    }
  } catch {
    // Corrupted data — reset to empty rather than crashing the panel.
    savedLogs.value = []
  }
}

function persistSavedLogs() {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(savedLogs.value))
  } catch {
    // Quota exceeded or storage disabled — silently ignore so the HUD keeps
    // working. A warning could go here but debug tooling shouldn't error-log
    // into the user's console.
  }
}

onMounted(loadSavedLogs)

// Refresh from storage when the review modal opens, so saves from other tabs
// show up without a hard reload.
watch(reviewOpen, (open) => {
  if (open) loadSavedLogs()
})

const savedLogsSorted = computed(() =>
  [...savedLogs.value].sort((a, b) => b.savedAt - a.savedAt),
)

function onSave() {
  const tracker = props.ui.battleTracker
  if (!tracker) return
  const name = window.prompt(
    'Save battle log as:',
    `${props.ui.mapName || props.ui.mapId || 'match'} — ${formatElapsed(tracker.elapsedSeconds)}`,
  )
  if (name === null) return // user cancelled
  const trimmed = name.trim() || `match ${new Date().toLocaleString()}`
  savedLogs.value.push({
    id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    name: trimmed,
    savedAt: Date.now(),
    mapId: props.ui.mapId,
    mapName: props.ui.mapName,
    elapsedSeconds: tracker.elapsedSeconds,
    // Deep-copy via JSON roundtrip so future mutations to the reactive snapshot
    // do NOT leak into the saved record.
    data: JSON.parse(JSON.stringify(tracker)) as BattleTrackerSnapshot,
  })
  persistSavedLogs()
}

function onDelete(id: string) {
  if (!window.confirm('Delete this saved battle log?')) return
  savedLogs.value = savedLogs.value.filter((l) => l.id !== id)
  expandedIds.value.delete(id)
  persistSavedLogs()
}

function toggleExpand(id: string) {
  const next = new Set(expandedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  expandedIds.value = next
}

// ── Formatters ───────────────────────────────────────────────────────────

function formatElapsed(secs: number): string {
  const total = Math.max(0, Math.floor(secs))
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${s.toString().padStart(2, '0')}`
}

function formatSavedAt(ms: number): string {
  return new Date(ms).toLocaleString()
}

function formatPlayerId(id: string): string {
  if (id === '__enemy__') return 'NPC Enemies'
  if (id === '__neutral__') return 'Neutral'
  // UUID identities (the canonical X-Player-ID format) are 36 chars with
  // hyphens — truncate to the first 6 hex chars so the panel stays readable.
  if (/^[0-9a-f-]{36}$/.test(id)) return `Player ${id.slice(0, 6)}`
  return id
}

const elapsedText = computed(() => {
  const e = props.ui.battleTracker?.elapsedSeconds ?? 0
  return formatElapsed(e)
})
</script>

<style scoped>
.battle-tracker {
  position: fixed;
  /* Anchored to the bottom-right so the collapsed header hugs the corner and
     the expanded body grows upward. Bottom offset clears the SelectionHud
     (which sits at the bottom-center) without overlapping it. */
  bottom: 18px;
  right: 18px;
  z-index: 10;
  width: 360px;
  max-height: calc(100vh - 160px);
  overflow: hidden;
  display: flex;
  /* column-reverse keeps the header pinned to the bottom so the body grows up. */
  flex-direction: column-reverse;
  border-radius: 14px;
  border: 1px solid rgba(200, 164, 106, 0.32);
  background:
    radial-gradient(circle at top, rgba(196, 140, 62, 0.14), transparent 45%),
    linear-gradient(180deg, rgba(46, 29, 16, 0.96), rgba(22, 14, 9, 0.96));
  box-shadow:
    inset 0 1px 0 rgba(246, 225, 183, 0.1),
    0 10px 24px rgba(0, 0, 0, 0.4);
  font-family: inherit;
  color: #f5ead2;
}

.battle-tracker.collapsed {
  max-height: none;
}

/* Bottom-anchored layout: header border flips to the top so it still
   separates the header from the body above it. Rule appears second so it
   extends the earlier .bt-head block without needing !important. */
.bt-head {
  border-top: 1px solid rgba(200, 164, 106, 0.2);
}

.bt-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  cursor: grab;
  user-select: none;
  touch-action: none;
}

.bt-head--dragging {
  cursor: grabbing;
}

.bt-grip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 18px;
  color: rgba(200, 164, 106, 0.6);
  font-size: 12px;
  letter-spacing: -2px;
  line-height: 1;
  transform: rotate(90deg);
}

.battle-tracker.dragging {
  opacity: 0.92;
}

.bt-toggle {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 6px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  text-align: left;
}

.bt-chevron {
  display: inline-block;
  font-size: 12px;
  color: #d7bb84;
  transition: transform 120ms ease;
}
.bt-chevron.open {
  transform: rotate(0deg);
}
.bt-chevron:not(.open) {
  transform: rotate(-90deg);
}

.bt-title {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #f0d88e;
}

.bt-elapsed {
  margin-left: auto;
  font-size: 12px;
  color: #d7bb84;
  font-variant-numeric: tabular-nums;
}

.bt-actions {
  display: flex;
  gap: 6px;
}

.bt-btn {
  padding: 4px 10px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.3);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.7), rgba(61, 39, 22, 0.85));
  color: #f5ead2;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  cursor: pointer;
}
.bt-btn:hover {
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.9), rgba(83, 53, 28, 0.96));
  border-color: rgba(220, 180, 110, 0.55);
}

.bt-btn-danger {
  border-color: rgba(220, 90, 90, 0.45);
}
.bt-btn-danger:hover {
  background: linear-gradient(180deg, rgba(180, 60, 60, 0.9), rgba(100, 30, 30, 0.96));
}

.bt-body {
  padding: 8px 10px 10px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.bt-player {
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.16);
  padding: 6px 8px;
  background: rgba(20, 12, 7, 0.5);
}

.bt-player-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 10px;
  margin-bottom: 4px;
}

.bt-player-name {
  font-size: 12px;
  font-weight: 700;
  color: #f5ead2;
}

.bt-player-total {
  font-size: 11px;
  color: #d7bb84;
  font-variant-numeric: tabular-nums;
}

.bt-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 11px;
}

.bt-table th,
.bt-table td {
  padding: 2px 4px;
  text-align: left;
}

.bt-table th {
  font-weight: 600;
  color: #d7bb84;
  border-bottom: 1px solid rgba(200, 164, 106, 0.2);
}

.bt-table td {
  color: #f5ead2;
  border-bottom: 1px solid rgba(200, 164, 106, 0.08);
}

.bt-col-kind {
  width: 64px;
  color: #d7bb84;
}
.bt-col-subtype {
  color: #f5ead2;
}
.bt-col-num {
  width: 68px;
  text-align: right !important;
  font-variant-numeric: tabular-nums;
}

.bt-empty {
  padding: 12px;
  color: #b89a6a;
  font-size: 12px;
  text-align: center;
}

/* ── Review modal ───────────────────────────────────────────────────── */

.bt-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 50;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.bt-modal {
  width: 640px;
  max-width: 100%;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  border-radius: 14px;
  border: 1px solid rgba(200, 164, 106, 0.32);
  background: linear-gradient(180deg, rgba(46, 29, 16, 0.98), rgba(22, 14, 9, 0.98));
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.5);
  overflow: hidden;
}

.bt-modal-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-bottom: 1px solid rgba(200, 164, 106, 0.2);
}

.bt-modal-title {
  flex: 1;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #f0d88e;
}

.bt-log-list {
  list-style: none;
  margin: 0;
  padding: 8px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.bt-log-item {
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.18);
  background: rgba(20, 12, 7, 0.55);
  padding: 6px 8px;
}

.bt-log-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.bt-log-toggle {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
  text-align: left;
  font-size: 12px;
}

.bt-log-name {
  font-weight: 700;
  color: #f5ead2;
}

.bt-log-meta {
  font-size: 10px;
  color: #d7bb84;
  font-variant-numeric: tabular-nums;
}

.bt-log-body {
  margin-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
</style>
