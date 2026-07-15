import { describe, expect, it, beforeEach } from 'vitest'
import { GameState } from './GameState'
import { initItemDefs } from '../maps/itemDefs'
import { initListDefs } from '../maps/listDefs'
import type { ItemDef } from '../maps/itemDefs'
import type { BuildingTile, VaultItemSnapshot } from '../network/protocol'

// An item IS its own recipe. The three prices are deliberately different numbers
// so a surface reaching for the wrong one fails rather than passing by accident:
//   costGold 40        — buy the finished sword off a shelf
//   craftCostGold 150  — make it at an Artificer
//   recipeCostGold 300 — learn the recipe at a Recipe Shop
const FIRE_SWORD: ItemDef = {
  id: 'fire_sword', displayName: 'Fire Sword', iconKey: 'fire_sword',
  kind: 'equipment', tier: 'rare', costGold: 40,
  crafting: { inputs: ['broad_sword', 'fire_ring'], craftCostGold: 150, recipeCostGold: 300 },
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
  initItemDefs([FIRE_SWORD, BROAD_SWORD, FIRE_RING])
  initListDefs([])
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
    s.localPlayerUnlockedCraftableIds = ['fire_sword']
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
    // The craft price (150), never the learn price (300).
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

  it('a crafting building bound to a list makes only what is ON that list', () => {
    // The list narrows what this forge can make. It never grants a recipe the
    // player has not learned — that is checked separately, server-side.
    initListDefs([{ id: 'shields_only', name: 'Shields', items: ['steel_shield'] }])
    const s = mk()
    s.mapConfig.buildings = [artificer('p1', { metadata: { list: 'shields_only' } })]
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    // fire_sword is learned and its ingredients are in hand, but this forge
    // does not make swords.
    expect(s.getCraftCatalogSnapshot()[0].craftable).toBe(false)

    // Bind a list that DOES include it and the same craft becomes available.
    initListDefs([{ id: 'swords', name: 'Swords', items: ['fire_sword'] }])
    s.mapConfig.buildings = [artificer('p1', { metadata: { list: 'swords' } })]
    expect(s.getCraftCatalogSnapshot()[0].craftable).toBe(true)
  })

  it('skips learned ids with no catalog def', () => {
    const s = mk()
    s.localPlayerUnlockedCraftableIds = ['fire_sword', 'unknown_recipe']
    s.localPlayerVault = vault('broad_sword', 'fire_ring')
    expect(s.getCraftCatalogSnapshot().map((e) => e.recipeId)).toEqual(['fire_sword'])
  })
})
