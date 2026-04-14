import type { BuildingCapability } from '../network/protocol'

export type BuildingDef = {
  type: string
  width: number
  height: number
  maxHp: number
  buildSeconds: number
  resourceCost: Record<string, number>
  capabilities: BuildingCapability[]
  spawnUnitTypes: string[]
  metadata: Record<string, string | number | boolean>
  // Client-only display properties
  color: string
  label: string
  hotkey: string
}

export const BUILDABLE_BUILDING_DEFS: BuildingDef[] = [
  {
    type: 'barracks',
    width: 2,
    height: 2,
    maxHp: 500,
    buildSeconds: 15,
    resourceCost: { gold: 200, wood: 150 },
    capabilities: ['unit-spawner'],
    spawnUnitTypes: ['soldier'],
    metadata: {},
    color: '#1e40af',
    label: '(B)arracks',
    hotkey: 'b',
  },
  {
    type: 'farm',
    width: 2,
    height: 2,
    maxHp: 300,
    buildSeconds: 10,
    resourceCost: { gold: 100, wood: 50 },
    capabilities: [],
    spawnUnitTypes: [],
    metadata: {},
    color: '#4a7c3f',
    label: '(F)arm',
    hotkey: 'f',
  },
  {
    type: 'townhall',
    width: 3,
    height: 3,
    maxHp: 1000,
    buildSeconds: 30,
    resourceCost: { gold: 400, wood: 200 },
    capabilities: ['unit-spawner', 'occupiable', 'deposit-point'],
    spawnUnitTypes: ['worker'],
    metadata: { foodSupply: 15 },
    color: '#b45309',
    label: '(T)ownhall',
    hotkey: 't',
  },
]

export const BUILDING_DEF_MAP = new Map<string, BuildingDef>(
  BUILDABLE_BUILDING_DEFS.map((def) => [def.type, def]),
)
