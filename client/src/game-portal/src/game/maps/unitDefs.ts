import type { UnitCapability } from '../network/protocol'

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
  // Client-only display properties
  trainLabel: string
}

export const UNIT_DEFS: UnitDef[] = [
  {
    type: 'worker',
    name: 'Worker',
    hp: 100,
    damage: 3,
    attackRange: 60,
    attackSpeed: 1.0,
    resourceCost: { gold: 150 },
    meatCost: 1,
    spawnSeconds: 5,
    capabilities: ['move', 'gather', 'build', 'attack'],
    trainLabel: 'Train Worker',
  },
  {
    type: 'soldier',
    name: 'Soldier',
    hp: 150,
    damage: 10,
    attackRange: 60,
    attackSpeed: 1.0,
    resourceCost: { gold: 100, wood: 25 },
    meatCost: 2,
    spawnSeconds: 10,
    capabilities: ['move', 'attack'],
    trainLabel: 'Train Soldier',
  },
]

export const UNIT_DEF_MAP = new Map<string, UnitDef>(
  UNIT_DEFS.map((def) => [def.type, def]),
)
