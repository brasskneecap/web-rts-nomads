import type { JsonObject, UnitCapability } from '../network/protocol'

// A single paint layer in a unit's sprite.
// Coordinates are in canvas pixels relative to the unit's center point.
// color: 'player' means "substitute the unit's assigned color at render time".
export type UnitRenderLayer =
  | { kind: 'circle'; cx: number; cy: number; r: number; color: string }
  | { kind: 'poly'; points: readonly [number, number][]; color: string }

export type UnitRenderDef = {
  layers: UnitRenderLayer[]
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
  hp: number
  damage: number
  attackRange: number
  attackSpeed: number
  goldGatherAmount?: number
  woodGatherAmount?: number
  resourceCost: Record<string, number>
  meatCost: number
  spawnSeconds: number
  capabilities: UnitCapability[]
  trainLabel: string
  metadata?: JsonObject
  attackVisual?: UnitAttackVisual
  render: UnitRenderDef
}

export let UNIT_DEFS: UnitDef[] = []

export let UNIT_DEF_MAP = new Map<string, UnitDef>()

export function initUnitDefs(defs: UnitDef[]): void {
  UNIT_DEFS = defs
  UNIT_DEF_MAP = new Map(defs.map((def) => [def.type, def]))
}

export function getUnitRenderBounds(def: UnitDef | undefined | null): {
  minX: number
  minY: number
  maxX: number
  maxY: number
} | null {
  const layers = def?.render?.layers
  if (!layers?.length) return null

  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity

  for (const layer of layers) {
    if (layer.kind === 'circle') {
      minX = Math.min(minX, layer.cx - layer.r)
      minY = Math.min(minY, layer.cy - layer.r)
      maxX = Math.max(maxX, layer.cx + layer.r)
      maxY = Math.max(maxY, layer.cy + layer.r)
      continue
    }

    for (const [px, py] of layer.points) {
      minX = Math.min(minX, px)
      minY = Math.min(minY, py)
      maxX = Math.max(maxX, px)
      maxY = Math.max(maxY, py)
    }
  }

  if (!isFinite(minX) || !isFinite(minY) || !isFinite(maxX) || !isFinite(maxY)) {
    return null
  }

  return { minX, minY, maxX, maxY }
}

export function getResolvedUnitAttackVisual(
  def: UnitDef | undefined | null,
): ResolvedUnitAttackVisual {
  const bounds = getUnitRenderBounds(def)
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
            ? Math.max(18, Math.round((bounds?.maxY ?? 12) * 1.2))
            : 10),
      ),
    ),
  }
}
