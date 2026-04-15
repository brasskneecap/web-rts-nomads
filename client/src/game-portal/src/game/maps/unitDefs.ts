import type { UnitCapability } from '../network/protocol'
import rawDefs from './unit-defs.json'

// A single paint layer in a unit's sprite.
// Coordinates are in canvas pixels relative to the unit's center point.
// color: 'player' means "substitute the unit's assigned color at render time".
export type UnitRenderLayer =
  | { kind: 'circle'; cx: number; cy: number; r: number; color: string }
  | { kind: 'poly'; points: readonly [number, number][]; color: string }

export type UnitRenderDef = {
  layers: UnitRenderLayer[]
}

export type UnitDef = {
  type: string
  name: string
  hp: number
  damage: number
  attackRange: number
  attackSpeed: number
  resourceCost: Record<string, number>
  meatCost: number
  spawnSeconds: number
  capabilities: UnitCapability[]
  trainLabel: string
  render: UnitRenderDef
}

export const UNIT_DEFS: UnitDef[] = rawDefs.units as UnitDef[]

export const UNIT_DEF_MAP = new Map<string, UnitDef>(
  UNIT_DEFS.map((def) => [def.type, def]),
)
