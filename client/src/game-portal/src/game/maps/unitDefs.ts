import type { JsonObject, UnitCapability } from '../network/protocol'
import type { UnitDirection } from '../rendering/unitSprites'

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

// Per-unit ground-shadow tuning. Every field is optional; absent fields fall
// back to defaults derived from the unit's bounds (see resolveUnitShadow in
// unitShadow.ts), so a unit with no `shadow` block still gets a proportional
// blob shadow. Passed through the catalog as-is; the server never reads it.
export type UnitShadow = {
  /** Default true. Set false to draw no shadow for this unit. */
  enabled?: boolean
  /** Horizontal radius in px. Default: bounds.halfWidth * 0.85. */
  radiusX?: number
  /** Vertical radius in px. Default: resolved radiusX * 0.4. */
  radiusY?: number
  /** Peak alpha at the center, 0..1. Default: 0.35. */
  opacity?: number
  /** Px nudge from the feet anchor. Default: 0. */
  offsetX?: number
  /** Px nudge from the feet anchor (positive = down). Default: 0. */
  offsetY?: number
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

// Screen-space offset from unit.x/unit.y (the same lift the renderer applies)
// where a unit's projectiles/spells/beams visually leave its body.
export type UnitOriginPoint = { x: number; y: number }

// Per-facing attack origin authoring. Both fields are optional so an
// unauthored unit produces an empty (or absent) block — see
// getResolvedUnitAttackOrigin, which returns null in that case so the
// renderer falls through to today's exact geometry.
export type UnitAttackOrigin = {
  default?: UnitOriginPoint
  byFacing?: Partial<Record<UnitDirection, UnitOriginPoint>>
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
  /** Per-facing point where projectiles/spells/beams leave this unit's body,
   *  as a screen-space offset from unit.x/unit.y. Absent ⇒ getResolvedUnitAttackOrigin
   *  returns null and the renderer keeps its existing geometry unchanged. */
  attackOrigin?: UnitAttackOrigin
  bounds?: UnitBounds
  /** Optional ground-shadow tuning. Absent ⇒ defaults derived from bounds. */
  shadow?: UnitShadow
  /** Airborne unit. Renders above ground units (with shadow/elevation) and is only hit by attackers whose targetableTypes include "flyer". */
  flyer?: boolean
  /** Target classes this unit's attacks can hit. When absent the server derives a default at spawn (projectile attacks → ground+flyer, otherwise ground only). */
  targetableTypes?: UnitTargetClass[]
  /** Building types the player must own fully-built before this unit
   *  can be trained. Server is authoritative; client uses this only to
   *  render the requirement tooltip on locked train actions. */
  requiresBuildings?: string[]
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

// Merge the authored bounds OVER the defaults rather than replacing them, so a
// PARTIAL bounds block (e.g. the anchors overlay authoring only ringOffsetX/Y,
// with no halfWidth/top/bottom) still yields a complete UnitBounds. Replacing
// would leave halfWidth/bottom `undefined`, which anchors the sprite at NaN and
// makes the unit render invisibly.
export function getUnitBounds(def: UnitDef | undefined | null): UnitBounds {
  return def?.bounds ? { ...DEFAULT_UNIT_BOUNDS, ...def.bounds } : DEFAULT_UNIT_BOUNDS
}

// Per-path bounds override: path id (e.g. "marksman") → bounds. Populated
// from the /catalog/units `paths` array. Path-promoted units use these
// before falling back to the base UnitDef bounds — necessary because path
// variants ship their own sprites with different pixel sizes.
export let PATH_BOUNDS_MAP: Map<string, UnitBounds> = new Map()

export function initPathBounds(entries: Array<{ path: string; bounds: UnitBounds }>): void {
  PATH_BOUNDS_MAP = new Map(entries.map((e) => [e.path, e.bounds]))
}

// Per-path ground-shadow override: path id → shadow config. Populated from the
// /catalog/units `paths` array's shadow field. Mirrors PATH_BOUNDS_MAP /
// PATH_ATTACK_ORIGIN_MAP — a path variant may want its own blob shadow, distinct
// from the base unit's (authored via UnitSpritePreview's anchors overlay).
export let PATH_SHADOW_MAP: Map<string, UnitShadow> = new Map()

export function initPathShadow(entries: Array<{ path: string; shadow?: UnitShadow | null }>): void {
  PATH_SHADOW_MAP = new Map(
    entries
      .filter((e): e is { path: string; shadow: UnitShadow } => !!e.shadow)
      .map((e) => [e.path, e.shadow]),
  )
}

// Resolves the shadow config for a rendered unit instance, checking the PATH's
// authored shadow before the base unit def's — mirrors getUnitBoundsFor's
// path-then-type precedence. Returns undefined when neither authors one, so the
// caller falls through to resolveUnitShadow's bounds-derived default blob.
export function getUnitShadowFor(args: {
  path?: string | null
  unitType?: string | null
}): UnitShadow | undefined {
  if (args.path && args.path !== 'none') {
    const s = PATH_SHADOW_MAP.get(args.path)
    if (s) return s
  }
  const def = args.unitType ? UNIT_DEF_MAP.get(args.unitType) : undefined
  return def?.shadow
}

// Catalog topology: unit type → its promotion path ids, mirroring the
// server directory layout under catalog/units/<faction>/<unit>/paths/.
// Populated from the /catalog/units `pathsByUnit` field. Consumers (e.g.
// DebugSpawnPanel) should derive their unit→path UI from this map instead
// of duplicating the layout in code.
export let PATHS_BY_UNIT_TYPE_MAP: Map<string, string[]> = new Map()

export function initPathsByUnitType(byUnit: Record<string, string[]>): void {
  PATHS_BY_UNIT_TYPE_MAP = new Map(Object.entries(byUnit))
}

// Resolves bounds for a unit instance, checking path before unitType. Mirrors
// the path-then-type lookup in getUnitSpriteSet so a path-promoted unit's
// selection ring tracks its own sprite, not the base unit's.
export function getUnitBoundsFor(args: {
  path?: string | null
  unitType?: string | null
}): UnitBounds {
  const def = args.unitType ? UNIT_DEF_MAP.get(args.unitType) : undefined
  const base = getUnitBounds(def)
  if (args.path && args.path !== 'none') {
    const pathBounds = PATH_BOUNDS_MAP.get(args.path)
    // Merge the path override OVER the base unit's bounds — a path may author a
    // COMPLETE bounds (its sprite is a different size) or just a PARTIAL tweak
    // (e.g. only the ring offset via the anchors overlay). Either way it inherits
    // any field it doesn't set from the base unit instead of nulling it out.
    if (pathBounds) return { ...base, ...pathBounds }
  }
  return base
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

// Shared "byFacing wins over default" resolution for a single authored
// attackOrigin block. Extracted so the unit-only resolver (Phase 4, below)
// and the path-aware resolver (further below) share exactly one
// implementation of that precedence instead of two copies that could drift.
function resolveOriginFromBlock(
  ao: UnitAttackOrigin | null | undefined,
  facing: UnitDirection | undefined,
): UnitOriginPoint | null {
  if (!ao) return null
  const pick = (facing && ao.byFacing?.[facing]) || ao.default
  if (!pick) return null
  return { x: Math.round(pick.x), y: Math.round(pick.y) }
}

// Resolves the authored attack origin for a unit's current facing. Returns
// null when the unit has no attackOrigin authored at all (or it authors
// neither `default` nor a matching `byFacing` entry) — callers MUST treat
// null as "fall back to today's existing geometry", never as (0, 0).
export function getResolvedUnitAttackOrigin(
  def: UnitDef | null | undefined,
  facing: UnitDirection | undefined,
): UnitOriginPoint | null {
  return resolveOriginFromBlock(def?.attackOrigin, facing)
}

// Per-path attack-origin override: path id (e.g. "marksman") → its authored
// {default?, byFacing?} block. Populated from the /catalog/units `paths`
// array's (now unioned) attackOrigin field. Mirrors PATH_BOUNDS_MAP exactly
// — same map/init/lookup shape, same reason (path variants author their own
// projectile launch points, distinct from the base unit's).
export let PATH_ATTACK_ORIGIN_MAP: Map<string, UnitAttackOrigin> = new Map()

export function initPathAttackOrigin(
  entries: Array<{ path: string; attackOrigin?: UnitAttackOrigin | null }>,
): void {
  PATH_ATTACK_ORIGIN_MAP = new Map(
    entries
      .filter((e): e is { path: string; attackOrigin: UnitAttackOrigin } => !!e.attackOrigin)
      .map((e) => [e.path, e.attackOrigin]),
  )
}

// Resolves the attack origin for a rendered unit instance, checking the
// PATH's authored origin before the base unit def's — mirrors
// getUnitBoundsFor's path-then-type precedence EXACTLY, including the
// 'none' sentinel check: if the unit is on a path AND that path authored
// ANY attackOrigin block, the path's block is used exclusively (byFacing >
// default WITHIN that block) and the base unit def's attackOrigin is never
// consulted — same "path presence switches the whole source" rule bounds
// uses, not a per-field merge. Only when the path has no attackOrigin block
// at all (or there is no path) does resolution fall through to the base
// unit def, then to null, so the renderer's existing geometric fallback is
// completely unchanged for any unit/path with no authored origin anywhere.
export function getResolvedAttackOriginFor(
  args: { path?: string | null; unitType?: string | null },
  facing: UnitDirection | undefined,
): UnitOriginPoint | null {
  if (args.path && args.path !== 'none') {
    const pathOrigin = PATH_ATTACK_ORIGIN_MAP.get(args.path)
    if (pathOrigin) return resolveOriginFromBlock(pathOrigin, facing)
  }
  const def = args.unitType ? UNIT_DEF_MAP.get(args.unitType) : undefined
  return getResolvedUnitAttackOrigin(def, facing)
}
