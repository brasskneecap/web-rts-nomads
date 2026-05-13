import type { JsonObject, UnitCapability } from '../network/protocol'

// Visual footprint of a unit, in canvas pixels relative to its (x, y) anchor.
// Used to anchor the sprite's feet, size the selection ring, place overhead UI,
// and derive hit-test rects. Sprites are centered horizontally on unit.x; top
// and bottom are signed offsets from unit.y (top is typically negative).
export type UnitBounds = {
  halfWidth: number
  top: number
  bottom: number
  // Per-unit nudge for the selection ring (and its hover/inspected/drag-select
  // siblings + perk auras), independent of where the sprite is anchored.
  // Positive Y pushes the ring down, negative Y pulls it up.
  ringOffsetX?: number
  ringOffsetY?: number
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

/** Default categorisation of a unit type for the map editor brushing flow.
 *  Decoupled from runtime ownership — the placed-unit `playerSlot` decides who
 *  controls the unit in-game; faction just filters which types appear under
 *  each brush category.
 *
 *  Open-ended string by design: factions are derived from the server catalog
 *  at runtime (one bucket per `catalog/units/<faction>/` directory), so adding
 *  a new faction folder + unit picks it up automatically without a type edit. */
export type UnitFaction = string

/** Target classes a unit's attacks are valid against. Drives anti-air / ground-only filtering on both server and client. */
export type UnitTargetClass = 'ground' | 'flyer'

export type UnitDef = {
  type: string
  name: string
  faction: UnitFaction
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
  /** Server-only: name of the AI combat profile to use. Ignored by the client. */
  combatProfile?: string
  attackVisual?: UnitAttackVisual
  bounds?: UnitBounds
  /** Airborne unit. Renders above ground units (with shadow/elevation) and is only hit by attackers whose targetableTypes include "flyer". */
  flyer?: boolean
  /** Target classes this unit's attacks can hit. When absent the server derives a default at spawn (projectile attacks → ground+flyer, otherwise ground only). */
  targetableTypes?: UnitTargetClass[]
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

// Per-path bounds override: path id (e.g. "marksman") → bounds. Populated
// from the /catalog/units `paths` array. Path-promoted units use these
// before falling back to the base UnitDef bounds — necessary because path
// variants ship their own sprites with different pixel sizes.
export let PATH_BOUNDS_MAP: Map<string, UnitBounds> = new Map()

export function initPathBounds(entries: Array<{ path: string; bounds: UnitBounds }>): void {
  PATH_BOUNDS_MAP = new Map(entries.map((e) => [e.path, e.bounds]))
}

// Resolves bounds for a unit instance, checking path before unitType. Mirrors
// the path-then-type lookup in getUnitSpriteSet so a path-promoted unit's
// selection ring tracks its own sprite, not the base unit's.
export function getUnitBoundsFor(args: {
  path?: string | null
  unitType?: string | null
}): UnitBounds {
  if (args.path && args.path !== 'none') {
    const pathBounds = PATH_BOUNDS_MAP.get(args.path)
    if (pathBounds) return pathBounds
  }
  const def = args.unitType ? UNIT_DEF_MAP.get(args.unitType) : undefined
  return getUnitBounds(def)
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
