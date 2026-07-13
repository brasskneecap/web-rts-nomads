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

// itemsForUnitType filters the item catalog to items valid for a unit type.
// ItemDef.allowedUnitTypes (see game/maps/itemDefs.ts) is the list of
// eligible unit types — absent/empty means the item applies to any unit
// type. Mirrors the itemTypeAllowsUnit semantics in VaultPanel.vue: an item
// authored onto a placed unit of a disallowed type is silently
// force-equipped by the server (equipItemDirectLocked) with no drop and no
// warning, unlike perks (which the server drops at hydrate), so this filter
// must be applied client-side.
export function itemsForUnitType<T extends { id: string; allowedUnitTypes?: string[] }>(
  items: T[],
  unitType: string,
): T[] {
  return items.filter((i) => !i.allowedUnitTypes || i.allowedUnitTypes.length === 0 || i.allowedUnitTypes.includes(unitType))
}
