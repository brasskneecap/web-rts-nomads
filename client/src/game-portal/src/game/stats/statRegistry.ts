// Shared stat-modifier registry — the TypeScript mirror of the Go
// statRegistry (server/internal/game/stat_modifiers.go). Single source of truth
// for the editor's stat dropdown and the zone inspection UI's bonus labels, so
// both speak the same stat ids and display the same names the server validates.
//
// Keep IN SYNC with the Go registry: adding a stat there means adding the same
// id + label here.

import type { StatModifier } from '../network/protocol'

export type StatDef = {
  id: string
  label: string
  /** Mirrors Go's statDef.IsFraction (stat_modifiers.go): true when the
   *  stat's value is itself a dimensionless 0-1-ish fraction (a probability,
   *  or a ratio measured against a fixed baseline of 1.0), so an `add` delta
   *  is a percentage-point amount; false when it's a raw rate/value with a
   *  per-unit base that varies, where an `add` delta must render as a bare
   *  number. See the Go registry's doc comment for the full per-stat
   *  reasoning — this mirror must stay in sync with it by hand. */
  isFraction: boolean
}

/** Registered stats in the same order as the Go statRegistry (combat first,
 *  then economy/workers). */
export const STAT_DEFS: StatDef[] = [
  { id: 'healthRegen', label: 'Health Regen', isFraction: false },
  { id: 'manaRegen', label: 'Mana Regen', isFraction: false },
  { id: 'moveSpeed', label: 'Move Speed', isFraction: false },
  { id: 'attackSpeed', label: 'Attack Speed', isFraction: false },
  { id: 'damage', label: 'Damage', isFraction: false },
  { id: 'armor', label: 'Armor', isFraction: false },
  { id: 'maxHp', label: 'Max Health', isFraction: false },
  { id: 'maxMana', label: 'Max Mana', isFraction: false },
  { id: 'attackRange', label: 'Attack Range', isFraction: false },
  { id: 'critChance', label: 'Crit Chance', isFraction: true },
  { id: 'critMultiplier', label: 'Crit Multiplier', isFraction: false },
  { id: 'goldGatherRate', label: 'Gold Gather Rate', isFraction: false },
  { id: 'woodGatherRate', label: 'Wood Gather Rate', isFraction: false },
  { id: 'gatherSpeed', label: 'Gather Speed', isFraction: true },
  { id: 'workerMoveSpeed', label: 'Worker Move Speed', isFraction: false },
  { id: 'unitProductionSpeed', label: 'Unit Production Speed', isFraction: true },
  { id: 'buildingConstructionSpeed', label: 'Building Construction Speed', isFraction: true },
]

const LABEL_BY_ID = new Map(STAT_DEFS.map((d) => [d.id, d.label]))
const FRACTION_BY_ID = new Map(STAT_DEFS.map((d) => [d.id, d.isFraction]))

/** Display label for a stat id, falling back to the raw id. */
export function statLabel(id: string): string {
  return LABEL_BY_ID.get(id) ?? id
}

/** Mirrors Go's isFractionStat: true when the stat's value is itself a 0-1
 *  fraction, so an `add` delta is a percentage-point amount. Unknown ids fall
 *  back to false — a bare number is honest, a fabricated percentage is not. */
export function isFractionStat(id: string): boolean {
  return FRACTION_BY_ID.get(id) ?? false
}

/** Stat operations as authored. */
export const STAT_OPERATIONS = ['add', 'multiply'] as const

/** Format a stat modifier for display, e.g. "+2 Health Regen",
 *  "+15% Gold Gather Rate", "-10% Move Speed", "+10% Crit Chance".
 *
 *  A `multiply` renders as a signed percent delta from 1.0. An `add` depends on
 *  the stat: for a FRACTION stat (isFraction — a probability, or a ratio against
 *  a fixed 1.0 baseline) the delta IS a percentage-point amount, so +0.1 shows
 *  as "+10%". For a raw per-unit-base stat (attackSpeed, damage, maxHp…) the
 *  percentage cannot be derived from the delta alone — it depends on that unit's
 *  base — so it renders as a bare number. Rendering those as a percentage is
 *  exactly the bug that made hawk_spirit advertise "+30% attack speed" when
 *  +0.3 on an archer's 1.5 base is really +20%. */
export function formatModifier(m: StatModifier): string {
  const label = statLabel(m.stat)
  if (m.operation === 'multiply') {
    const pct = Math.round((m.value - 1) * 1000) / 10 // one decimal, trimmed below
    const sign = pct >= 0 ? '+' : ''
    return `${sign}${trimNum(pct)}% ${label}`
  }
  const sign = m.value >= 0 ? '+' : ''
  if (isFractionStat(m.stat)) {
    return `${sign}${trimNum(m.value * 100)}% ${label}`
  }
  return `${sign}${trimNum(m.value)} ${label}`
}

function trimNum(n: number): string {
  // Render integers without a trailing ".0"; keep up to one decimal otherwise.
  return Number.isInteger(n) ? String(n) : String(Math.round(n * 10) / 10)
}
