// The train action's displayed cost must reflect the local player's effective
// (advancement-adjusted) unit cost, not the static catalog cost. The server
// sends per-player overrides on PlayerSnapshot.unitCostOverrides; getBuildingActions
// overlays them so the build-menu price matches what the server charges.

import { describe, expect, it, beforeEach } from 'vitest'
import { getBuildingActions } from './GameState'
import { initUnitDefs, type UnitDef } from '../maps/unitDefs'
import type { BuildingTile, UnitCostOverride } from '../network/protocol'

function mkWorkerDef(): UnitDef {
  return {
    type: 'worker',
    name: 'Worker',
    faction: 'human',
    hp: 40,
    damage: 2,
    attackRange: 0,
    attackSpeed: 1,
    moveSpeed: 100,
    resourceCost: { gold: 100, wood: 50 },
    meatCost: 1,
    spawnSeconds: 10,
    capabilities: ['gather'],
    trainLabel: 'Train Worker',
  }
}

function mkBarracks(): BuildingTile {
  return {
    id: 'b1',
    x: 0,
    y: 0,
    buildingType: 'townhall',
    width: 2,
    height: 2,
    occupied: true,
    visible: true,
    capabilities: ['unit-spawner'],
    spawnUnitTypes: ['worker'],
  } as BuildingTile
}

function goldAmount(action: { cost?: Array<{ resourceId: string; amount: number }> } | undefined): number | undefined {
  return action?.cost?.find((c) => c.resourceId === 'gold')?.amount
}

beforeEach(() => {
  initUnitDefs([mkWorkerDef()])
})

describe('getBuildingActions — train cost override', () => {
  it('uses the catalog cost when the player has no override', () => {
    const actions = getBuildingActions(mkBarracks())
    const train = actions.find((a) => a.id === 'train-worker')
    expect(goldAmount(train)).toBe(100)
  })

  it('uses the effective cost when the player has a goldCost advancement', () => {
    const overrides = new Map<string, UnitCostOverride>([
      ['worker', { unitType: 'worker', resourceCost: { gold: 85, wood: 50 }, meatCost: 1 }],
    ])
    const actions = getBuildingActions(
      mkBarracks(),
      undefined,
      undefined,
      0,
      new Set<string>(),
      0,
      new Set<string>(),
      overrides,
    )
    const train = actions.find((a) => a.id === 'train-worker')
    expect(goldAmount(train)).toBe(85)
    // Untouched resources still render from the override map.
    expect(train?.cost?.find((c) => c.resourceId === 'wood')?.amount).toBe(50)
  })

  it('drops a resource row the override reduces to zero', () => {
    const overrides = new Map<string, UnitCostOverride>([
      ['worker', { unitType: 'worker', resourceCost: { gold: 100, wood: 0 }, meatCost: 1 }],
    ])
    const actions = getBuildingActions(
      mkBarracks(),
      undefined,
      undefined,
      0,
      new Set<string>(),
      0,
      new Set<string>(),
      overrides,
    )
    const train = actions.find((a) => a.id === 'train-worker')
    expect(train?.cost?.some((c) => c.resourceId === 'wood')).toBe(false)
  })
})
