import { describe, expect, it, beforeEach } from 'vitest'
import { getBuildingActions } from './GameState'
import { initRecipeDefs } from '../maps/recipeDefs'
import type { BuildingTile, VaultItemSnapshot } from '../network/protocol'

beforeEach(() => {
  initRecipeDefs([
    { id: 'fire_sword', name: 'Fire Sword', inputs: ['broad_sword', 'fire_ring'], costGold: 150, output: 'fire_sword' },
  ])
})

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
