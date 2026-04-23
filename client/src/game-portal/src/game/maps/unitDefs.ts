import type { JsonObject, UnitCapability } from '../network/protocol'

// Visual footprint of a unit, in canvas pixels relative to its (x, y) anchor.
// Used to anchor the sprite's feet, size the selection ring, place overhead UI,
// and derive hit-test rects. Sprites are centered horizontally on unit.x; top
// and bottom are signed offsets from unit.y (top is typically negative).
export type UnitBounds = {
  halfWidth: number
  top: number
  bottom: number
}

export type UnitAttackVisual = {
  kind?: 'melee' | 'projectile'
  originX?: number
  originY?: number
  effectLength?: number
}

export type ResolvedUnitAttackVisual = {
  kind: 'melee' | 'projectile'
  originX: number
  originY: number
  effectLength: number
}

export type UnitDef = {
  type: string
  name: string
  archetype?: string
  hp: number
  damage: number
  attackRange: number
  attackSpeed: number
  /** Base pixels-per-second pathing speed (pre-rank/path/perk modifiers). */
  moveSpeed: number
  goldGatherAmount?: number
  woodGatherAmount?: number
  resourceCost: Record<string, number>
  meatCost: number
  spawnSeconds: number
  capabilities: UnitCapability[]
  trainLabel: string
  metadata?: JsonObject
  attackVisual?: UnitAttackVisual
  bounds?: UnitBounds
}

export let UNIT_DEFS: UnitDef[] = []

export let UNIT_DEF_MAP = new Map<string, UnitDef>()

export function initUnitDefs(defs: UnitDef[]): void {
  UNIT_DEFS = defs
  UNIT_DEF_MAP = new Map(defs.map((def) => [def.type, def]))
}

// Fallback used when a unit def lacks `bounds` — roughly the pre-refactor
// 14px feet-radius body so hit-testing and ring sizing still work.
export const DEFAULT_UNIT_BOUNDS: UnitBounds = {
  halfWidth: 14,
  top: -26,
  bottom: 2,
}

export function getUnitBounds(def: UnitDef | undefined | null): UnitBounds {
  return def?.bounds ?? DEFAULT_UNIT_BOUNDS
}

export function getResolvedUnitAttackVisual(
  def: UnitDef | undefined | null,
): ResolvedUnitAttackVisual {
  const bounds = getUnitBounds(def)
  const inferredKind = (def?.attackRange ?? 0) > 80 ? 'projectile' : 'melee'
  const kind = def?.attackVisual?.kind ?? inferredKind

  return {
    kind,
    originX: Math.round(def?.attackVisual?.originX ?? 0),
    originY: Math.round(def?.attackVisual?.originY ?? 0),
    effectLength: Math.max(
      4,
      Math.round(
        def?.attackVisual?.effectLength
          ?? (kind === 'projectile'
            ? Math.max(18, Math.round(bounds.bottom * 1.2))
            : 10),
      ),
    ),
  }
}
