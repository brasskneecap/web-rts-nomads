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
}

/** Registered stats in the same order as the Go statRegistry (combat first,
 *  then economy/workers). */
export const STAT_DEFS: StatDef[] = [
  { id: 'healthRegen', label: 'Health Regen' },
  { id: 'manaRegen', label: 'Mana Regen' },
  { id: 'moveSpeed', label: 'Move Speed' },
  { id: 'attackSpeed', label: 'Attack Speed' },
  { id: 'damage', label: 'Damage' },
  { id: 'armor', label: 'Armor' },
  { id: 'maxHealth', label: 'Max Health' },
  { id: 'maxMana', label: 'Max Mana' },
  { id: 'goldGatherRate', label: 'Gold Gather Rate' },
  { id: 'woodGatherRate', label: 'Wood Gather Rate' },
  { id: 'gatherSpeed', label: 'Gather Speed' },
  { id: 'workerMoveSpeed', label: 'Worker Move Speed' },
  { id: 'unitProductionSpeed', label: 'Unit Production Speed' },
  { id: 'buildingConstructionSpeed', label: 'Building Construction Speed' },
]

const LABEL_BY_ID = new Map(STAT_DEFS.map((d) => [d.id, d.label]))

/** Display label for a stat id, falling back to the raw id. */
export function statLabel(id: string): string {
  return LABEL_BY_ID.get(id) ?? id
}

/** Stat operations as authored. */
export const STAT_OPERATIONS = ['add', 'multiply'] as const

/** Format a stat modifier for display, e.g. "+2 Health Regen",
 *  "+15% Gold Gather Rate", "-10% Move Speed". An `add` renders as a signed
 *  flat delta; a `multiply` renders as a signed percent delta from 1.0. */
export function formatModifier(m: StatModifier): string {
  const label = statLabel(m.stat)
  if (m.operation === 'multiply') {
    const pct = Math.round((m.value - 1) * 1000) / 10 // one decimal, trimmed below
    const sign = pct >= 0 ? '+' : ''
    return `${sign}${trimNum(pct)}% ${label}`
  }
  const sign = m.value >= 0 ? '+' : ''
  return `${sign}${trimNum(m.value)} ${label}`
}

function trimNum(n: number): string {
  // Render integers without a trailing ".0"; keep up to one decimal otherwise.
  return Number.isInteger(n) ? String(n) : String(Math.round(n * 10) / 10)
}
