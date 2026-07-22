<template>
  <div class="rank-panel">
    <p class="rank-panel__intro unit-hint">
      <template v-if="previousRank">
        Values carry forward from <strong>{{ previousRank }}</strong> — each field starts there and
        can only go up, so a promotion never weakens the unit.
      </template>
      <template v-else>
        The first rank. Values here become the floor for the next rank.
      </template>
    </p>

    <!-- Multiplier stats: (rank multiplier) × (base stat) = what the unit
         actually has, shown resolved so the author edits against a real number
         instead of a bare coefficient. -->
    <div class="rank-panel__grid">
      <label v-for="col in MULTIPLIER_COLUMNS" :key="col.key" class="rank-panel__field">
        <span class="rank-panel__label">{{ col.label }}</span>
        <input
          type="number"
          step="0.01"
          :min="floorFor(col.key)"
          :value="stats[col.key] ?? ''"
          :data-test="`rank-${rank}-mult-${col.key}`"
          @input="onNumberInput(col.key, $event)"
          @change="onNumberCommit(col.key, $event)"
        />
        <span v-if="resolved(col) !== undefined" class="rank-panel__resolved">
          {{ baseStats[col.baseKey] }} × {{ stats[col.key] }} = {{ resolved(col) }}
        </span>
        <span v-else-if="stats[col.key] !== undefined" class="rank-panel__note">(no base value)</span>
        <span v-if="inheritedNote(col.key)" class="rank-panel__inherited">{{ inheritedNote(col.key) }}</span>
      </label>

      <label v-for="col in ADDITIVE_COLUMNS" :key="col.key" class="rank-panel__field">
        <span class="rank-panel__label">{{ col.label }}</span>
        <input
          type="number"
          :min="floorFor(col.key)"
          :value="stats[col.key] ?? ''"
          :data-test="`rank-${rank}-add-${col.key}`"
          @input="onNumberInput(col.key, $event)"
          @change="onNumberCommit(col.key, $event)"
        />
        <span v-if="inheritedNote(col.key)" class="rank-panel__inherited">{{ inheritedNote(col.key) }}</span>
      </label>
    </div>

    <!-- Unit stats with no typed rank field — ability power, crit chance,
         lifesteal. Rows MIRROR whatever the parent unit or an earlier rank
         authored, so adding ability power to the unit makes it editable at every
         rank immediately instead of being invisible until re-added by hand.
         These are ABSOLUTE per rank (a multiplier on a base of 0 is always 0). -->
    <div v-if="baseStatRows.length > 0 || addableStats.length > 0" class="rank-panel__unit-stats">
      <h4 class="rank-panel__subhead">Unit Stats</h4>
      <div class="rank-panel__grid">
        <label v-for="row in baseStatRows" :key="row.id" class="rank-panel__field">
          <span class="rank-panel__label">{{ row.label }}</span>
          <span class="rank-panel__input-row">
            <input
              type="number"
              step="0.01"
              :min="row.floor"
              :value="baseStats_[row.id] ?? ''"
              :placeholder="row.inheritedFrom !== undefined ? String(row.inheritedFrom) : ''"
              :data-test="`rank-${rank}-base-${row.id}`"
              @input="onBaseStatInput(row.id, $event)"
              @change="onBaseStatCommit(row.id, $event)"
            />
            <button
              v-if="row.canRemove"
              type="button"
              class="rank-panel__row-del"
              title="Remove"
              :data-test="`rank-${rank}-remove-${row.id}`"
              @click.prevent="removeBaseStat(row.id)"
            >✕</button>
          </span>
          <span v-if="row.note" class="rank-panel__inherited">{{ row.note }}</span>
        </label>
      </div>

      <!-- A rank can INTRODUCE a stat the unit never authored — bronze adding
           ability power makes it appear (and floored) at silver and gold. -->
      <div v-if="addableStats.length > 0" class="rank-panel__add">
        <select
          :value="''"
          :aria-label="`Add a unit stat at ${rank}`"
          :data-test="`rank-${rank}-add-stat`"
          @change="onAddStat"
        >
          <option value="">Add a stat…</option>
          <option v-for="opt in addableStats" :key="opt.id" :value="opt.id">{{ opt.label }}</option>
        </select>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// PathRankPanel: ONE rank's stat block, for the per-rank tabs that replaced the
// wide all-ranks grid.
//
// THE INHERITANCE RULE. Rank stat blocks are ABSOLUTE per rank (gold's ×1.35 is
// off the BASE unit, not off silver's ×1.2), which reads fine in a table where
// every rank is visible at once but is actively misleading one rank at a time —
// an author editing silver in isolation has no idea what bronze already gave.
// So each field shows the value carried from the previous rank and is FLOORED
// at it: a promotion can never weaken a unit.
//
// The floor comes from the PREVIOUS RANK only, never from 1.0. Flooring the
// first rank at "no change" would flag shipping data — arch_mage authors
// healthRegenMultiplier: 0 across all three ranks, which is a deliberate "no
// regen", not a regression.
import { computed } from 'vue'

export interface RankStats {
  [key: string]: number | undefined
}

const props = defineProps<{
  rank: string
  /** The rank before this one, or '' for the first. Used for the floor. */
  previousRank?: string
  /** The previous rank's authored stats — the floor and the "inherited" note. */
  previousStats?: RankStats
  /** The parent unit's base stats, for the resolved `base × mult` readout. */
  baseStats: Record<string, number | undefined>
  stats: RankStats
  /** The parent unit's OWN fieldless base stats (UnitDef.baseStats), keyed by
   *  stat id. Seeds a row per stat so a unit-level ability power is editable at
   *  every rank without re-adding it. */
  unitBaseStats?: Record<string, number>
  /** The previous rank's fieldless base stats — the floor for this rank's. */
  previousBaseStats?: Record<string, number>
  /** Display labels for stat ids, from the shared stat registry. */
  statLabels?: Record<string, string>
  /** Every stat a unit may carry a base for, so a rank can INTRODUCE one the
   *  unit never authored (the server rejects anything outside this set). */
  addableStatIds?: string[]
}>()

const emit = defineEmits<{
  'update:stats': [stats: RankStats]
  'update:baseStats': [stats: Record<string, number>]
}>()

// This rank's own fieldless base stats.
const baseStats_ = computed<Record<string, number>>(() => (props.stats.baseStats as unknown as Record<string, number>) ?? {})

interface BaseStatRow {
  id: string
  label: string
  floor: number | undefined
  inheritedFrom: number | undefined
  note: string
  /** True when THIS rank set a value — the only thing a rank can take back. */
  canRemove: boolean
}

// The row set is the UNION of every stat the unit authored, every stat an
// earlier rank authored, and this rank's own — so a stat can never disappear
// from a later rank just because that rank has not touched it yet.
const baseStatRows = computed<BaseStatRow[]>(() => {
  const ids = new Set([
    ...Object.keys(props.unitBaseStats ?? {}),
    ...Object.keys(props.previousBaseStats ?? {}),
    ...Object.keys(baseStats_.value),
  ])
  return [...ids].sort().map((id) => {
    // The floor is the nearest authored value behind this rank: the previous
    // rank if it set one, else the unit's own base.
    const floor = props.previousBaseStats?.[id] ?? props.unitBaseStats?.[id]
    const mine = baseStats_.value[id]
    let note = ''
    if (floor !== undefined) {
      const source = props.previousBaseStats?.[id] !== undefined ? props.previousRank : 'the unit'
      if (mine === undefined) note = `inherits ${floor} from ${source}`
      else if (mine === floor) note = `same as ${source}`
      else note = `${source} had ${floor}`
    }
    return {
      id,
      label: props.statLabels?.[id] ?? id,
      floor,
      inheritedFrom: mine === undefined ? floor : undefined,
      note,
      // Removable when THIS rank set a value and no LOWER RANK has one.
      //
      // The base unit deliberately does NOT count as a lower value: a stat
      // bronze introduced is bronze's to take back even though the unit itself
      // never authored it. What blocks removal is a lower RANK depending on it —
      // clearing silver while bronze still sets the stat would leave silver
      // silently inheriting a number it appears not to have.
      canRemove: mine !== undefined && props.previousBaseStats?.[id] === undefined,
    }
  })
})

// Stats not already on a row here — the ones this rank could introduce.
const addableStats = computed(() => {
  const present = new Set(baseStatRows.value.map((r) => r.id))
  return (props.addableStatIds ?? [])
    .filter((id) => !present.has(id))
    .map((id) => ({ id, label: props.statLabels?.[id] ?? id }))
    .sort((a, b) => a.label.localeCompare(b.label))
})

// Adding seeds the value at 0 so the row appears immediately with something to
// edit. A bare key with no value would round-trip as "authored nothing".
function onAddStat(e: Event) {
  const id = (e.target as HTMLSelectElement).value
  if (!id) return
  ;(e.target as HTMLSelectElement).value = ''
  emit('update:baseStats', { ...baseStats_.value, [id]: 0 })
}

function removeBaseStat(id: string) {
  const next = { ...baseStats_.value }
  delete next[id]
  emit('update:baseStats', next)
}

function onBaseStatInput(id: string, e: Event) {
  writeBaseStat(id, (e.target as HTMLInputElement).value, false)
}

function onBaseStatCommit(id: string, e: Event) {
  writeBaseStat(id, (e.target as HTMLInputElement).value, true)
}

function writeBaseStat(id: string, raw: string, clamp: boolean) {
  const next = { ...baseStats_.value }
  if (raw === '') {
    delete next[id]
  } else {
    const n = Number(raw)
    if (!Number.isFinite(n)) return
    const floor = clamp ? (props.previousBaseStats?.[id] ?? props.unitBaseStats?.[id]) : undefined
    next[id] = floor !== undefined && n < floor ? floor : n
  }
  emit('update:baseStats', next)
}

interface MultiplierColumn {
  key: string
  label: string
  baseKey: string
}

// Field names are the server's pathRankStatsJSON keys. This panel is the only
// place they are listed — the old all-ranks PathRankGrid it replaced is deleted.
const MULTIPLIER_COLUMNS: MultiplierColumn[] = [
  { key: 'maxHPMultiplier', label: 'Max HP', baseKey: 'hp' },
  { key: 'maxMPMultiplier', label: 'Max MP', baseKey: 'maxMana' },
  { key: 'healthRegenMultiplier', label: 'HP Regen', baseKey: 'healthRegenRate' },
  { key: 'damageMultiplier', label: 'Damage', baseKey: 'damage' },
  { key: 'attackSpeedMultiplier', label: 'Attack Speed', baseKey: 'attackSpeed' },
  { key: 'moveSpeedMultiplier', label: 'Move Speed', baseKey: 'moveSpeed' },
  { key: 'attackRangeMultiplier', label: 'Attack Range ×', baseKey: 'attackRange' },
]

const ADDITIVE_COLUMNS = [
  { key: 'attackRange', label: 'Attack Range +' },
  { key: 'armor', label: 'Armor +' },
  { key: 'dodgeChance', label: 'Dodge +' },
  { key: 'blockChance', label: 'Block +' },
  { key: 'visionRange', label: 'Vision Range +' },
]

const previousStats = computed<RankStats>(() => props.previousStats ?? {})

/** The floor for a field: the previous rank's value, or undefined for the first rank. */
function floorFor(key: string): number | undefined {
  return previousStats.value[key]
}

function inheritedNote(key: string): string {
  const prev = floorFor(key)
  if (prev === undefined) return ''
  const mine = props.stats[key]
  if (mine === undefined) return `inherits ${prev} from ${props.previousRank}`
  if (mine === prev) return `same as ${props.previousRank}`
  return `${props.previousRank} was ${prev}`
}

function resolved(col: MultiplierColumn): number | undefined {
  const base = props.baseStats[col.baseKey]
  const mult = props.stats[col.key]
  if (base === undefined || mult === undefined) return undefined
  return Math.round(base * mult * 100) / 100
}

// Typing is not clamped — that would fight the user mid-keystroke (typing "1"
// on the way to "1.5" under a floor of 1.2 would snap instantly). The clamp
// lands on `change`, once the value is settled.
function onNumberInput(key: string, e: Event) {
  write(key, (e.target as HTMLInputElement).value, false)
}

function onNumberCommit(key: string, e: Event) {
  write(key, (e.target as HTMLInputElement).value, true)
}

function write(key: string, raw: string, clamp: boolean) {
  const next: RankStats = { ...props.stats }
  if (raw === '') {
    delete next[key]
  } else {
    const n = Number(raw)
    if (!Number.isFinite(n)) return
    const floor = clamp ? floorFor(key) : undefined
    next[key] = floor !== undefined && n < floor ? floor : n
  }
  emit('update:stats', next)
}
</script>

<style scoped>
.rank-panel__intro {
  margin: 0 0 0.75rem;
}

.rank-panel__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(13rem, 1fr));
  gap: 0.75rem 1rem;
}

.rank-panel__field {
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  font-size: 0.9em;
}

.rank-panel__label {
  opacity: 0.85;
}

.rank-panel__resolved,
.rank-panel__note,
.rank-panel__inherited {
  font-size: 0.78em;
  opacity: 0.65;
}

.rank-panel__inherited {
  font-style: italic;
}

.rank-panel__unit-stats {
  margin-top: 1rem;
}

.rank-panel__subhead {
  margin: 0 0 0.5rem;
  font-size: 0.9em;
  opacity: 0.85;
}

.rank-panel__add {
  margin-top: 0.6rem;
}

/* Matches the editor's other row-remove buttons (unit-editor__row-del): a bare
   ✕ beside the field, not a worded link — every other removable row in this
   editor looks like this, and a one-off treatment reads as something else. */
.rank-panel__input-row {
  display: flex;
  align-items: center;
  gap: 0.35rem;
}

.rank-panel__input-row input {
  flex: 1;
  min-width: 0;
}

.rank-panel__row-del {
  padding: 4px 8px;
  font-size: 0.76rem;
  color: var(--ed-text-dim);
  background: none;
  border: 1px solid var(--ed-line);
  border-radius: var(--ed-radius);
}

.rank-panel__row-del:hover {
  color: var(--ed-danger);
  border-color: rgba(240, 132, 108, 0.4);
}
</style>
