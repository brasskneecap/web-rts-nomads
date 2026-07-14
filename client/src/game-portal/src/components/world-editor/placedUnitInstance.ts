// ─────────────────────────────────────────────────────────────────────────────
// Placed-unit instance edits — pure form transforms for the world editor's
// per-placed-unit edit popup (rank / items / perks). The server applies these
// fields at spawn for BOTH player and enemy placed units (see
// server/internal/game/placed_unit_instance_test.go); this module only
// shapes the client-side edit, it never simulates anything.
// ─────────────────────────────────────────────────────────────────────────────

import type { PlacedUnit } from '@/game/network/protocol'
import type { UnitDef } from '@/game/maps/unitDefs'

export type InstancePatch = { rank: string; items: string[]; perks: string[] }

// applyInstanceEdit returns a new PlacedUnit with rank/items/perks set,
// dropping empty values so they stay off the wire (omitempty parity with the
// server's protocol.PlacedUnit).
export function applyInstanceEdit(unit: PlacedUnit, patch: InstancePatch): PlacedUnit {
  const next: PlacedUnit = { ...unit }
  if (patch.rank) next.rank = patch.rank
  else delete next.rank
  if (patch.items.length) next.items = [...patch.items]
  else delete next.items
  if (patch.perks.length) next.perks = [...patch.perks]
  else delete next.perks
  return next
}

// Global rank set — mirrors unitRankBronze/Silver/Gold in
// server/internal/game/progression.go. The catalog's UnitDef carries no
// per-unit rank-eligibility data (no rank/tier field on UnitDef), so ranks
// are not filtered per unit type; every catalog unit type can hold any of
// the three ranks. We still require the unit type to exist in the catalog so
// a stale/removed unit type on a placed unit doesn't offer rank options that
// no longer resolve to anything.
const ALL_RANKS = ['bronze', 'silver', 'gold']

export function ranksForUnitType(unitDefs: UnitDef[], unitType: string): string[] {
  if (!unitDefs.some((def) => def.type === unitType)) return []
  return ALL_RANKS
}

// perksForUnitType filters the perk catalog to perks valid for a unit type.
// PerkDef.unitType (see game/maps/perkDefs.ts) is the eligible unit type,
// e.g. "soldier" — absent means the perk applies to any unit type.
export function perksForUnitType<T extends { id: string; unitType?: string }>(
  perkDefs: T[],
  unitType: string,
): T[] {
  return perkDefs.filter((p) => !p.unitType || p.unitType === unitType)
}

// Items carry no unit-type restriction: any unit can equip any item. (Perks
// still do — see perksForUnitType above.) The former itemsForUnitType filter is
// gone with ItemDef.allowedUnitTypes; call sites list the whole item catalog.
