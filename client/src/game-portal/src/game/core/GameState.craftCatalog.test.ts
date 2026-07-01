import { describe, expect, it, beforeEach } from 'vitest'
import { GameState } from './GameState'
import { initRecipeDefs } from '../maps/recipeDefs'
import type { BuildingTile, VaultItemSnapshot } from '../network/protocol'

beforeEach(() => {
  initRecipeDefs([
    { id: 'fire_sword', name: 'Fire Sword', inputs: ['broad_sword', 'fire_ring'], costGold: 150, output: 'fire_sword' },
  ])
})

function artificer(ownerId: string, over: Partial<BuildingTile> = {}): BuildingTile {
  return {
    id: 'art-1', x: 0, y: 0, buildingType: 'artificer', width: 3, height: 3,
    occupied: true, visible: true, ownerId, capabilities: ['crafting'],
    ...over,
  } as BuildingTile
}

function vault(...ids: string[]): VaultItemSnapshot[] {
  return ids.map((itemId, i) => ({ instanceId: i + 1, itemId, stacks: 1 }))
}

describe('GameState.localPlayerHasArtificer', () => {
  it('is true only for an own, built crafting building', () => {
    const s = new GameState()
    s.localPlayerId = 'p1'
    s.mapConfig.buildings = [artificer('p1')]
    expect(s.localPlayerHasArtificer()).toBe(true)

    s.mapConfig.buildings = [artificer('p2')] // someone else's
    expect(s.localPlayerHasArtificer()).toBe(false)

    s.mapConfig.buildings = [artificer('p1', { metadata: { underConstruction: true } })]
    expect(s.localPlayerHasArtificer()).toBe(false)

    s.mapConfig.buildings = []
    expect(s.localPlayerHasArtificer()).toBe(false)
  })
})

describe('GameState.getCraftCatalogSnapshot', () => {
  function mk(): GameState {
    const s = new GameState()
    s.localPlayerId = 'p1'
    s.localPlayerUnlockedRecipeIds = ['fire_sword']
    s.mapConfig.buildings = [artificer('p1')]
    return s
  }

  it('reports have/need per ingredient and craftable when all present + artificer owned', () => {
    const s = mk()
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    const entries = s.getCraftCatalogSnapshot()
    expect(entries).toHaveLength(1)
    const e = entries[0]
    expect(e.recipeId).toBe('fire_sword')
    expect(e.name).toBe('Fire Sword')
    expect(e.output).toBe('fire_sword')
    expect(e.costGold).toBe(150)
    expect(e.ingredients).toEqual([
      { itemId: 'broad_sword', have: 1, need: 1 },
      { itemId: 'fire_ring', have: 1, need: 1 },
    ])
    expect(e.craftable).toBe(true)
  })

  it('is not craftable when an ingredient is missing', () => {
    const s = mk()
    s.localPlayerVault = vault('broad_sword') // no fire_ring
    const e = s.getCraftCatalogSnapshot()[0]
    expect(e.ingredients.find((i) => i.itemId === 'fire_ring')?.have).toBe(0)
    expect(e.craftable).toBe(false)
  })

  it('is not craftable when the player owns no artificer', () => {
    const s = mk()
    s.mapConfig.buildings = []
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    expect(s.getCraftCatalogSnapshot()[0].craftable).toBe(false)
  })

  it('skips unlocked ids with no catalog def', () => {
    const s = mk()
    s.localPlayerUnlockedRecipeIds = ['fire_sword', 'unknown_recipe']
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    expect(s.getCraftCatalogSnapshot().map((e) => e.recipeId)).toEqual(['fire_sword'])
  })
})
