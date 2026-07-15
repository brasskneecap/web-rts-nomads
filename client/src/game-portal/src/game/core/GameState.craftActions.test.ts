import { describe, expect, it, beforeEach } from 'vitest'
import { getBuildingActions } from './GameState'
import { initItemDefs } from '../maps/itemDefs'
import { initListDefs } from '../maps/listDefs'
import type { ItemDef } from '../maps/itemDefs'

// An item IS its own recipe. Three deliberately different prices, so a surface
// reaching for the wrong one fails rather than passing on a shared number:
//   costGold 40        — buy the finished sword
//   craftCostGold 150  — make it at an Artificer
//   recipeCostGold 300 — learn the recipe at a Recipe Shop
const FIRE_SWORD: ItemDef = {
  id: 'fire_sword', displayName: 'Fire Sword', iconKey: 'fire_sword',
  kind: 'equipment', tier: 'rare', costGold: 40,
  crafting: { inputs: ['broad_sword', 'fire_ring'], craftCostGold: 150, recipeCostGold: 300 },
}
const FROST_SWORD: ItemDef = {
  id: 'frost_sword', displayName: 'Frost Sword', iconKey: 'frost_sword',
  kind: 'equipment', tier: 'rare', costGold: 40,
  crafting: { inputs: ['broad_sword', 'ice_ring'], craftCostGold: 150, recipeCostGold: 300 },
}
const BROAD_SWORD: ItemDef = {
  id: 'broad_sword', displayName: 'Broad Sword', iconKey: 'broad_sword',
  kind: 'equipment', tier: 'common', costGold: 50,
}
const FIRE_RING: ItemDef = {
  id: 'fire_ring', displayName: 'Fire Ring', iconKey: 'fire_ring',
  kind: 'equipment', tier: 'rare', costGold: 90,
}

beforeEach(() => {
  initItemDefs([FIRE_SWORD, FROST_SWORD, BROAD_SWORD, FIRE_RING])
  initListDefs([])
})

import type { BuildingTile, VaultItemSnapshot } from '../network/protocol'

function mkArtificer(): BuildingTile {
  return {
    id: 'art-1', x: 0, y: 0, buildingType: 'artificer', width: 3, height: 3,
    occupied: true, visible: true, ownerId: 'p1', capabilities: ['crafting'],
  } as BuildingTile
}

function vault(...ids: string[]): VaultItemSnapshot[] {
  return ids.map((itemId, i) => ({ instanceId: i + 1, itemId, stacks: 1 }))
}

describe('getBuildingActions — artificer crafting', () => {
  it('enables a craft action when all ingredients are in the vault', () => {
    const actions = getBuildingActions(
      mkArtificer(), [], { vault: vault('broad_sword', 'fire_ring'), vaultCapacity: 6 },
      0, new Set(), 0, new Set(['fire_sword']),
    )
    const craft = actions.find((a) => a.id === 'craft-fire_sword')
    expect(craft).toBeTruthy()
    expect(craft?.label).toBe('Fire Sword')
    expect(craft?.disabled).toBeFalsy()
  })

  it('disables the craft action when an ingredient is missing', () => {
    const actions = getBuildingActions(
      mkArtificer(), [], { vault: vault('broad_sword'), vaultCapacity: 6 },
      0, new Set(), 0, new Set(['fire_sword']),
    )
    expect(actions.find((a) => a.id === 'craft-fire_sword')?.disabled).toBe(true)
  })

  it('shows no craft action for recipes the player has not unlocked', () => {
    const actions = getBuildingActions(
      mkArtificer(), [], { vault: vault('broad_sword', 'fire_ring'), vaultCapacity: 6 },
      0, new Set(), 0, new Set(), // empty unlocked set
    )
    expect(actions.some((a) => a.id.startsWith('craft-'))).toBe(false)
  })
})
