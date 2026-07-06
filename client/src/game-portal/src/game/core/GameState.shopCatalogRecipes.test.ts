import { describe, expect, it, beforeEach } from 'vitest'
import { GameState } from './GameState'
import { initRecipeDefs } from '../maps/recipeDefs'
import type { BuildingTile } from '../network/protocol'

beforeEach(() => {
  initRecipeDefs([
    { id: 'fire_sword', name: 'Fire Sword', inputs: ['broad_sword', 'fire_ring'], costGold: 150, output: 'fire_sword', rarity: 'rare' },
    { id: 'frost_sword', name: 'Frost Sword', inputs: ['broad_sword', 'ice_ring'], costGold: 150, output: 'frost_sword', rarity: 'rare' },
  ])
})

function mkRecipeShop(over: Partial<BuildingTile> = {}): BuildingTile {
  return {
    id: 'rs-1', x: 0, y: 0, buildingType: 'recipe-shop', width: 3, height: 3,
    occupied: true, visible: true, capabilities: ['recipe-purchase'],
    ownerId: 'neutral',
    shopDiscovered: true,
    shopLocked: false,
    recipeInventory: [
      { recipeId: 'fire_sword', quantity: 1 },
      { recipeId: 'frost_sword', quantity: 1 },
    ],
    ...over,
  } as BuildingTile
}

function stateWith(buildings: BuildingTile[], knownRecipes: string[] = []): GameState {
  const s = new GameState()
  s.localPlayerId = 'p1'
  s.mapConfig.buildings = buildings
  s.localPlayerUnlockedRecipeIds = knownRecipes
  return s
}

describe('getShopCatalogSnapshot — recipe shops', () => {
  it('emits a recipe entry per stocked recipe once discovered + unlocked', () => {
    const s = stateWith([mkRecipeShop()])
    const recipes = s.getShopCatalogSnapshot().filter((e) => e.entryType === 'recipe')
    expect(recipes.map((e) => e.itemId).sort()).toEqual(['fire_sword', 'frost_sword'])
    const fire = recipes.find((e) => e.itemId === 'fire_sword')!
    expect(fire.displayName).toBe('Recipe: Fire Sword')
    expect(fire.iconKey).toBe('rare_recipe')
    expect(fire.costGold).toBe(150)
    expect(fire.purchaseBuildingId).toBe('rs-1')
  })

  it('marks recipeKnown for recipes the local player already knows', () => {
    const s = stateWith([mkRecipeShop()], ['frost_sword'])
    const recipes = s.getShopCatalogSnapshot().filter((e) => e.entryType === 'recipe')
    expect(recipes.find((e) => e.itemId === 'fire_sword')!.recipeKnown).toBe(false)
    expect(recipes.find((e) => e.itemId === 'frost_sword')!.recipeKnown).toBe(true)
  })

  it('excludes recipe shops that are undiscovered or still guard-locked', () => {
    const undiscovered = stateWith([mkRecipeShop({ id: 'rs-a', shopDiscovered: false })])
    expect(undiscovered.getShopCatalogSnapshot().some((e) => e.entryType === 'recipe')).toBe(false)

    const locked = stateWith([mkRecipeShop({ id: 'rs-b', shopLocked: true })])
    expect(locked.getShopCatalogSnapshot().some((e) => e.entryType === 'recipe')).toBe(false)
  })
})
