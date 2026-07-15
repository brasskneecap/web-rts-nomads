<template>
  <table class="rank-grid">
    <thead>
      <tr>
        <th class="rank-grid__rank-header">Rank</th>
        <th v-for="col in MULTIPLIER_COLUMNS" :key="col.key">{{ col.label }}</th>
        <th>Attack Range</th>
        <th v-for="col in ADDITIVE_COLUMNS" :key="col.key">{{ col.label }}</th>
        <th>Vision Range</th>
      </tr>
    </thead>
    <tbody>
      <tr v-for="rank in RANK_ORDER" :key="rank">
        <th scope="row" class="rank-grid__rank-label">{{ rank }}</th>

        <!-- Multiplier columns: base × mult = result, so the author sees the
             resolved value instead of editing the multiplier blind. -->
        <td v-for="col in MULTIPLIER_COLUMNS" :key="col.key" class="rank-grid__cell">
          <input
            type="number"
            step="0.01"
            class="rank-grid__mult-input"
            :data-rank="rank"
            :data-field="col.key"
            :value="rankStats(rank)[col.key] ?? ''"
            @input="updateField(rank, col.key, ($event.target as HTMLInputElement).value)"
          />
          <span v-if="resolvedMultiplierValue(rank, col) !== undefined" class="rank-grid__resolved">
            {{ baseStats[col.baseKey] }} × {{ rankStats(rank)[col.key] }} = {{ resolvedMultiplierValue(rank, col) }}
          </span>
          <span v-else-if="rankStats(rank)[col.key] !== undefined" class="rank-grid__hint">(no base value)</span>
        </td>

        <!-- Attack Range: pick ONE mode (flat override OR multiplier) from the
             dropdown; the single input then edits that mode's field. Selecting a
             mode clears the other field, so the two can never both be set (which
             is what the old flat-wins conflict warning existed to flag). -->
        <td class="rank-grid__cell rank-grid__cell--range">
          <div class="rank-grid__range-row">
            <select
              class="rank-grid__range-mode"
              :data-rank="rank"
              :value="rangeMode(rank)"
              @change="setRangeMode(rank, ($event.target as HTMLSelectElement).value as 'flat' | 'mult')"
            >
              <option value="flat">Flat</option>
              <option value="mult">Mult</option>
            </select>
            <input
              type="number"
              :step="rangeMode(rank) === 'mult' ? '0.01' : undefined"
              :data-rank="rank"
              :data-field="rangeMode(rank) === 'flat' ? 'attackRange' : 'attackRangeMultiplier'"
              :value="rankStats(rank)[rangeMode(rank) === 'flat' ? 'attackRange' : 'attackRangeMultiplier'] ?? ''"
              @input="updateField(rank, rangeMode(rank) === 'flat' ? 'attackRange' : 'attackRangeMultiplier', ($event.target as HTMLInputElement).value)"
            />
          </div>
          <div v-if="rangeMode(rank) === 'flat' && attackRangeResolved(rank).flat !== undefined" class="rank-grid__resolved">
            Flat: {{ attackRangeResolved(rank).flat }}
          </div>
          <div v-else-if="rangeMode(rank) === 'mult' && attackRangeResolved(rank).multResult !== undefined" class="rank-grid__resolved">
            {{ baseStats.attackRange }} × {{ rankStats(rank).attackRangeMultiplier }} = {{ attackRangeResolved(rank).multResult }}
          </div>
        </td>

        <!-- Flat/additive columns: no base × mult expansion, just the value. -->
        <td v-for="col in ADDITIVE_COLUMNS" :key="col.key" class="rank-grid__cell">
          <span v-if="rankStats(rank)[col.key] !== undefined" class="rank-grid__additive-prefix">+</span>
          <input
            type="number"
            :data-rank="rank"
            :data-field="col.key"
            :value="rankStats(rank)[col.key] ?? ''"
            @input="updateField(rank, col.key, ($event.target as HTMLInputElement).value)"
          />
        </td>

        <!-- Vision Range: per-rank FLAT override (world pixels). When blank the
             unit falls back to its base/path vision, shown here for reference. -->
        <td class="rank-grid__cell">
          <input
            type="number"
            :data-rank="rank"
            data-field="visionRange"
            :value="rankStats(rank).visionRange ?? ''"
            @input="updateField(rank, 'visionRange', ($event.target as HTMLInputElement).value)"
          />
          <span
            v-if="baseStats.visionRange !== undefined && rankStats(rank).visionRange === undefined"
            class="rank-grid__hint"
          >base: {{ baseStats.visionRange }}</span>
        </td>
      </tr>
    </tbody>
  </table>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { PathRankStats } from '@/game/units/pathEditorForm'

const props = defineProps<{
  // The parent unit's base stats (AuthoredUnitDef-shaped: hp, damage,
  // attackSpeed, moveSpeed, attackRange, maxMana, healthRegenRate, …).
  // Loosely typed (not AuthoredUnitDef itself) since this component only
  // ever reads a handful of numeric fields out of it.
  baseStats: Record<string, number | undefined>
  ranks: Record<string, PathRankStats>
}>()

const emit = defineEmits<{ 'update:ranks': [Record<string, PathRankStats>] }>()

// Fixed row order regardless of which rank keys are actually present on
// `ranks` — a path missing e.g. `gold` still gets an editable blank row.
const RANK_ORDER = ['bronze', 'silver', 'gold'] as const

// Multiplier columns: (rank field) × (base stat) = resolved value. Field
// names confirmed against pathEditorForm.ts's PathRankStats and
// unitEditorForm.ts's AuthoredUnitDef (maxMana, healthRegenRate are the
// server's actual field names — not "mana"/"regenRate").
interface MultiplierColumn {
  // `string`, not `keyof PathRankStats` — PathRankStats' index signature
  // widens `keyof` to `string | number`, which doesn't match updateField's
  // `string` key parameter (and every column key here is a plain string
  // literal anyway).
  key: string
  label: string
  baseKey: string
}
const MULTIPLIER_COLUMNS: MultiplierColumn[] = [
  { key: 'maxHPMultiplier', label: 'Max HP', baseKey: 'hp' },
  { key: 'maxMPMultiplier', label: 'Max MP', baseKey: 'maxMana' },
  { key: 'healthRegenMultiplier', label: 'HP Regen', baseKey: 'healthRegenRate' },
  { key: 'damageMultiplier', label: 'Damage', baseKey: 'damage' },
  { key: 'attackSpeedMultiplier', label: 'Attack Speed', baseKey: 'attackSpeed' },
  { key: 'moveSpeedMultiplier', label: 'Move Speed', baseKey: 'moveSpeed' },
]

// Flat/additive columns: authored verbatim, no base-stat expansion.
// attackRange is deliberately NOT here — it gets its own combined cell
// alongside attackRangeMultiplier (see the conflict handling below).
const ADDITIVE_COLUMNS: { key: string; label: string }[] = [
  { key: 'armor', label: 'Armor' },
  { key: 'dodgeChance', label: 'Dodge' },
  { key: 'blockChance', label: 'Block' },
]

function rankStats(rank: string): PathRankStats {
  return props.ranks[rank] ?? {}
}

function round1(n: number): number {
  return Math.round(n * 10) / 10
}

function resolvedMultiplierValue(rank: string, col: MultiplierColumn): number | undefined {
  const mult = rankStats(rank)[col.key] as number | undefined
  const base = props.baseStats[col.baseKey]
  if (mult === undefined || base === undefined) return undefined
  return round1(base * mult)
}

function attackRangeResolved(rank: string): { flat: number | undefined; multResult: number | undefined } {
  const stats = rankStats(rank)
  const flat = stats.attackRange as number | undefined
  const mult = stats.attackRangeMultiplier as number | undefined
  const base = props.baseStats.attackRange
  const multResult = (mult !== undefined && base !== undefined) ? round1(base * mult) : undefined
  return { flat, multResult }
}

// Attack Range is authored as ONE of two mutually-exclusive fields (flat
// override OR multiplier). The active mode is derived from whichever field is
// present; when neither is (a blank cell) it falls back to a per-rank override
// that remembers the author's dropdown choice.
const rangeModeOverride = ref<Record<string, 'flat' | 'mult'>>({})

function rangeMode(rank: string): 'flat' | 'mult' {
  const stats = rankStats(rank)
  if (stats.attackRange !== undefined) return 'flat'
  if (stats.attackRangeMultiplier !== undefined) return 'mult'
  return rangeModeOverride.value[rank] ?? 'flat'
}

// Switching mode records the choice AND clears the other field, so a rank can
// never carry both a flat override and a multiplier at once.
function setRangeMode(rank: string, mode: 'flat' | 'mult') {
  rangeModeOverride.value = { ...rangeModeOverride.value, [rank]: mode }
  const nextRankStats: PathRankStats = { ...rankStats(rank) }
  delete nextRankStats[mode === 'flat' ? 'attackRangeMultiplier' : 'attackRange']
  emit('update:ranks', { ...props.ranks, [rank]: nextRankStats })
}

// Writes an update:ranks emission for a single field. Preserves the
// undefined-vs-0 distinction: an empty input DELETES the key (so it reads as
// absent, matching pathEditorForm's round-trip contract) rather than writing
// 0 or NaN — mirrors UnitTypeEditorPanel's healthRegenRate @input handling.
function updateField(rank: string, key: string, raw: string) {
  const nextRankStats: PathRankStats = { ...rankStats(rank) }
  if (raw === '') {
    delete nextRankStats[key]
  } else {
    nextRankStats[key] = Number(raw)
  }
  emit('update:ranks', { ...props.ranks, [rank]: nextRankStats })
}
</script>

<style scoped>
.rank-grid {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.76rem;
  color: #f8fafc;
}

.rank-grid th,
.rank-grid td {
  border: 1px solid rgba(148, 163, 184, 0.18);
  padding: 6px 8px;
  vertical-align: top;
  text-align: left;
}

.rank-grid thead th {
  background: linear-gradient(180deg, rgba(25, 35, 52, 0.92), rgba(14, 22, 36, 0.94));
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  font-size: 0.68rem;
}

.rank-grid__rank-header {
  width: 64px;
}

.rank-grid__rank-label {
  text-transform: capitalize;
  font-weight: 700;
  background: rgba(8, 14, 24, 0.55);
}

.rank-grid__cell {
  display: table-cell;
}

.rank-grid input {
  width: 68px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 4px 6px;
  font-size: 0.76rem;
}

.rank-grid__resolved {
  display: block;
  margin-top: 4px;
  color: rgba(226, 232, 240, 0.72);
  font-size: 0.7rem;
}

.rank-grid__hint {
  display: block;
  margin-top: 4px;
  color: rgba(226, 232, 240, 0.5);
  font-size: 0.7rem;
  font-style: italic;
}

.rank-grid__cell--range {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

/* Mode dropdown + its single value input, side by side. */
.rank-grid__range-row {
  display: flex;
  align-items: center;
  gap: 4px;
}

.rank-grid__range-mode {
  flex: 0 0 auto;
  width: 52px;
  min-width: 0;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 8px;
  background: rgba(15, 23, 42, 0.92);
  color: #f8fafc;
  padding: 4px 2px;
  font-size: 0.72rem;
}

/* Keep the value input from being shoved out of the cell by the dropdown. */
.rank-grid__range-row input {
  min-width: 0;
}

.rank-grid__additive-prefix {
  color: rgba(226, 232, 240, 0.6);
  margin-right: 2px;
}
</style>
