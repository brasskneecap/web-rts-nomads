<!--
  DebugSpawnPanel — dev-only HUD for spawning an enemy unit with a chosen
  perk loadout. Rendered only when the active map has `debug.debugSpawn:
  true`. The Spawn button arms the 'debug-spawn-unit' building-targeting
  mode so clicking on the map fires a debug_spawn_unit command; the mode
  persists across clicks so multiple copies can be dropped in a row.

  The panel is completely hidden on non-debug maps — no visible affordance
  on production gameplay.
-->
<template>
  <div v-if="debugEnabled" class="debug-spawn" :class="{ collapsed, dragging: drag.dragging.value }" :style="drag.style.value">
    <header class="ds-head" :class="{ 'ds-head--dragging': drag.dragging.value }" v-bind="drag.handleBindings" aria-label="Drag to move">
      <span class="ds-grip" aria-hidden="true">⋮⋮</span>
      <button
        class="ds-toggle"
        type="button"
        :aria-expanded="!collapsed"
        :title="collapsed ? 'Expand Debug Spawn' : 'Collapse Debug Spawn'"
        @click="collapsed = !collapsed"
      >
        <span class="ds-chevron" :class="{ open: !collapsed }">▾</span>
        <span class="ds-title">Debug Spawn</span>
        <span v-if="targetingActive" class="ds-placing">placing…</span>
      </button>
    </header>

    <div v-if="!collapsed" class="ds-body">
      <label class="ds-row">
        <span class="ds-label">Team</span>
        <select v-model="team" class="ds-select">
          <option value="mine">Mine</option>
          <option value="enemy">Enemy</option>
        </select>
      </label>

      <label class="ds-row">
        <span class="ds-label">Unit Type</span>
        <select v-model="unitType" class="ds-select">
          <option v-for="u in unitTypeOptions" :key="u.value" :value="u.value">{{ u.label }}</option>
        </select>
      </label>

      <label class="ds-row">
        <span class="ds-label">Path</span>
        <select v-model="path" class="ds-select">
          <option value="none">(none)</option>
          <option value="trapper">Trapper</option>
          <option value="marksman">Marksman</option>
          <option value="vanguard">Vanguard</option>
          <option value="berserker">Berserker</option>
        </select>
      </label>

      <label class="ds-row">
        <span class="ds-label">Rank</span>
        <select v-model="rank" class="ds-select">
          <option value="base">Base</option>
          <option value="bronze">Bronze</option>
          <option value="silver">Silver</option>
          <option value="gold">Gold</option>
        </select>
      </label>

      <!-- Perk rows, one per rank slot the chosen rank unlocks. Selecting a
           perk for a slot is optional (empty = no perk in that slot). Perks
           are filtered by matching unit type + path + rank (eligibility), but
           the server accepts any ID — a mis-matched pick still lands so you
           can intentionally test perk/unit combinations the rank-up pool
           would never have produced. -->
      <div v-for="slot in perkSlots" :key="slot.rank" class="ds-row">
        <span class="ds-label">{{ slot.label }}</span>
        <select v-model="perkSelections[slot.rank]" class="ds-select">
          <option value="">(none)</option>
          <option v-for="p in slot.options" :key="p.id" :value="p.id">
            {{ p.displayName }}{{ gatedWarning(p) }}
          </option>
        </select>
      </div>

      <label class="ds-row">
        <span class="ds-label">HP (0 = default)</span>
        <input v-model.number="customHp" type="number" min="0" step="1" class="ds-input" />
      </label>

      <div class="ds-actions">
        <button v-if="!targetingActive" class="ds-btn ds-btn-primary" type="button" @click="beginPlacement">
          Place on Map
        </button>
        <button v-else class="ds-btn ds-btn-danger" type="button" @click="cancelPlacement">
          Cancel Placement
        </button>
      </div>

      <div class="ds-hint">
        {{ targetingActive
          ? 'Click anywhere on the map to spawn. Right-click to stop.'
          : 'Pick a loadout, then click "Place on Map" to arm placement.' }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { GameUiSnapshot } from '@/game/core/GameClient'
import type { DebugSpawnConfig } from '@/game/core/GameState'
import { UNIT_DEFS } from '@/game/maps/unitDefs'
import { PERK_DEFS, type PerkDef } from '@/game/maps/perkDefs'
import { useDraggablePanel } from '@/composables/useDraggablePanel'

const drag = useDraggablePanel('debug-spawn')

const props = defineProps<{
  ui: GameUiSnapshot
  // Handle to GameState begin/cancel methods. Passed in as callbacks rather
  // than reaching into GameState directly so this component stays a pure
  // view: the parent wires up the targeting lifecycle.
  beginDebugSpawn: (cfg: DebugSpawnConfig) => void
  cancelDebugSpawn: () => void
  // Whether the 'debug-spawn-unit' targeting mode is currently active —
  // drives the primary button's label between Place and Cancel.
  targetingActive: boolean
}>()

const debugEnabled = computed(() => props.ui.debugSpawn)
const collapsed = ref(false)

// ── Selection state ───────────────────────────────────────────────────────
// Defaults match a common test case: a Gold archer on the trapper path with
// no perks pre-selected. Team defaults to "Mine" so the debug unit joins the
// caller's army and responds to their commands — the common case when
// iterating on your own perk loadouts. Toggle to "Enemy" to spawn a hostile
// target dummy for combat-matchup testing.
const unitType = ref('archer')
const team = ref<'mine' | 'enemy'>('mine')
const path = ref('trapper')
const rank = ref<'base' | 'bronze' | 'silver' | 'gold'>('gold')
const customHp = ref(0)
// Per-slot picks, keyed by rank name. Empty string = no perk in that slot.
const perkSelections = reactive<Record<string, string>>({
  bronze: '',
  silver: '',
  gold: '',
})

// ── Dropdown options ──────────────────────────────────────────────────────

const unitTypeOptions = computed(() => {
  // Use the fetched unit catalog for the main list so only trainable types
  // show up. Raider is hardcoded because it lives outside the catalog on
  // the server side but the debug tool supports spawning it.
  const fromCatalog = UNIT_DEFS.map((u) => ({ value: u.type, label: u.name || u.type }))
  fromCatalog.push({ value: 'raider', label: 'Raider (NPC)' })
  return fromCatalog.sort((a, b) => a.label.localeCompare(b.label))
})

// perkSlots determines which rank slots are visible given the selected rank.
// A Silver-ranked unit gets Bronze + Silver slots; a Gold unit gets all three.
const perkSlots = computed(() => {
  const slots: { rank: 'bronze' | 'silver' | 'gold'; label: string; options: PerkDef[] }[] = []
  if (rank.value === 'bronze' || rank.value === 'silver' || rank.value === 'gold') {
    slots.push({ rank: 'bronze', label: 'Bronze Perk', options: filterPerks('bronze') })
  }
  if (rank.value === 'silver' || rank.value === 'gold') {
    slots.push({ rank: 'silver', label: 'Silver Perk', options: filterPerks('silver') })
  }
  if (rank.value === 'gold') {
    slots.push({ rank: 'gold', label: 'Gold Perk', options: filterPerks('gold') })
  }
  return slots
})

// filterPerks returns all perks with matching (unitType, path, rank). Wildcard
// matches (empty string on the perk side) are treated as "applies to any".
function filterPerks(slotRank: string): PerkDef[] {
  return PERK_DEFS.filter((p) => {
    if (p.rank && p.rank !== slotRank) return false
    if (p.unitType && p.unitType !== unitType.value) return false
    if (p.path && p.path !== path.value) return false
    return true
  }).sort((a, b) => a.displayName.localeCompare(b.displayName))
}

// gatedWarning appends an "(!)" marker to perks that are selectable but whose
// `requiresPerk` prerequisite isn't currently satisfied by the picks in
// earlier slots. The debug tool still lets you spawn with that perk (server
// does not enforce eligibility) — the marker is just a heads-up that the
// combo wouldn't normally occur in-match.
function gatedWarning(perk: PerkDef): string {
  const req = (perk as unknown as { requiresPerk?: string }).requiresPerk
  if (!req) return ''
  const picked = [perkSelections.bronze, perkSelections.silver, perkSelections.gold]
  return picked.includes(req) ? '' : ' (!)'
}

// Clear slot picks when the unit type or path changes, since the eligible
// pool shifts completely and the old picks are almost never still valid.
watch([unitType, path], () => {
  perkSelections.bronze = ''
  perkSelections.silver = ''
  perkSelections.gold = ''
})

// ── Placement actions ─────────────────────────────────────────────────────

function beginPlacement() {
  const perkIds: string[] = []
  if (perkSelections.bronze) perkIds.push(perkSelections.bronze)
  if (perkSelections.silver) perkIds.push(perkSelections.silver)
  if (perkSelections.gold) perkIds.push(perkSelections.gold)
  props.beginDebugSpawn({
    unitType: unitType.value,
    team: team.value,
    path: path.value,
    rank: rank.value,
    perkIds,
    customHp: customHp.value > 0 ? customHp.value : undefined,
  })
}

function cancelPlacement() {
  props.cancelDebugSpawn()
}
</script>

<style scoped>
.debug-spawn {
  position: fixed;
  /* Anchored to the top-LEFT so we don't collide with the minimap (top-right)
     or the Battle Tracker (bottom-right). MatchHud occupies the very top so
     we offset 90px to clear the resource tray. */
  top: 90px;
  left: 18px;
  z-index: 10;
  width: 300px;
  max-height: calc(100vh - 130px);
  overflow: hidden;
  display: flex;
  flex-direction: column;
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

.debug-spawn.collapsed {
  max-height: none;
}

.ds-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  border-bottom: 1px solid rgba(200, 164, 106, 0.2);
  cursor: grab;
  user-select: none;
  touch-action: none;
}

.ds-head--dragging {
  cursor: grabbing;
}

.ds-grip {
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

.debug-spawn.dragging {
  opacity: 0.92;
}

.ds-toggle {
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

.ds-chevron {
  display: inline-block;
  font-size: 12px;
  color: #d7bb84;
  transition: transform 120ms ease;
}
.ds-chevron.open { transform: rotate(0deg); }
.ds-chevron:not(.open) { transform: rotate(-90deg); }

.ds-title {
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.18em;
  text-transform: uppercase;
  color: #f0d88e;
}

.ds-placing {
  margin-left: auto;
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.12em;
  text-transform: uppercase;
  color: #ffaf4a;
}

.ds-body {
  padding: 10px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ds-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ds-label {
  flex: 0 0 105px;
  font-size: 11px;
  color: #d7bb84;
}

.ds-select, .ds-input {
  flex: 1;
  padding: 4px 6px;
  border-radius: 6px;
  border: 1px solid rgba(200, 164, 106, 0.3);
  background: rgba(20, 12, 7, 0.7);
  color: #f5ead2;
  font-size: 12px;
}

.ds-select:focus, .ds-input:focus {
  outline: none;
  border-color: rgba(247, 216, 142, 0.7);
}

.ds-actions {
  display: flex;
  gap: 6px;
  margin-top: 6px;
}

.ds-btn {
  flex: 1;
  padding: 8px 10px;
  border-radius: 8px;
  border: 1px solid rgba(200, 164, 106, 0.3);
  background: linear-gradient(180deg, rgba(113, 75, 39, 0.7), rgba(61, 39, 22, 0.85));
  color: #f5ead2;
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  cursor: pointer;
}
.ds-btn:hover {
  background: linear-gradient(180deg, rgba(145, 96, 48, 0.9), rgba(83, 53, 28, 0.96));
  border-color: rgba(220, 180, 110, 0.55);
}

.ds-btn-primary {
  background: linear-gradient(180deg, rgba(180, 130, 52, 0.85), rgba(120, 80, 28, 0.95));
}

.ds-btn-danger {
  border-color: rgba(220, 90, 90, 0.45);
}
.ds-btn-danger:hover {
  background: linear-gradient(180deg, rgba(180, 60, 60, 0.9), rgba(100, 30, 30, 0.96));
}

.ds-hint {
  margin-top: 2px;
  font-size: 10px;
  color: #b89a6a;
  line-height: 1.35;
}
</style>
