import type { NeutralGroupSummary, NeutralGroupTierSummary } from '../network/protocol'

/**
 * Building types the server's `spawnShopGuardsLocked` actually spawns guards
 * for — the only types where a `guardGroupId` in metadata takes effect. Keep in
 * sync with `server/internal/game/state_shop.go`.
 */
export function isShopGuardableBuildingType(buildingType: string): boolean {
  return buildingType === 'neutral-shop' || buildingType === 'recipe-shop'
}

/**
 * Flattens the neutral-group tier catalog into a distinct, name-sorted list of
 * groups for the editor's guard-squad dropdown. Deduped by id (a group present
 * at several tiers appears once); the guard tier is chosen separately and the
 * server resolves the group at that tier.
 */
export function allGuardGroups(
  tiers: NeutralGroupTierSummary[] | null | undefined,
): NeutralGroupSummary[] {
  if (!tiers || tiers.length === 0) return []
  const byId = new Map<string, NeutralGroupSummary>()
  for (const t of tiers) {
    for (const g of t.groups) {
      if (!byId.has(g.id)) byId.set(g.id, g)
    }
  }
  return [...byId.values()].sort((a, b) =>
    (a.name || a.id).localeCompare(b.name || b.id),
  )
}
