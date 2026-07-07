import { describe, expect, it, beforeEach } from 'vitest'
import { getBuildingActions } from './GameState'
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
    recipeInventory: [
      { recipeId: 'fire_sword', quantity: 1 },
      { recipeId: 'frost_sword', quantity: 0 },
    ],
    ...over,
  } as BuildingTile
}

describe('getBuildingActions — recipe shop', () => {
  it('emits one buy-recipe action per stocked recipe, disabling sold-out slots', () => {
    const actions = getBuildingActions(mkRecipeShop())
    const fire = actions.find((a) => a.id === 'buy-recipe-fire_sword')
    const frost = actions.find((a) => a.id === 'buy-recipe-frost_sword')
    expect(fire).toBeTruthy()
    expect(fire?.label).toBe('Recipe: Fire Sword')
    // Recipe Shop renders a rarity-keyed recipe scroll icon, not the output item.
    expect(fire?.iconDef).toEqual({ kind: 'item', type: 'rare_recipe' })
    expect(fire?.cost).toEqual([{ resourceId: 'gold', amount: 150, accent: '#d4a84f' }])
    expect(fire?.disabled).toBeFalsy()
    expect(frost?.disabled).toBe(true) // quantity 0 → sold out
  })

  it('greys out recipes the player already knows, with an explanatory tooltip', () => {
    // 7th positional arg is unlockedRecipeIds.
    const known = new Set(['fire_sword'])
    const actions = getBuildingActions(mkRecipeShop(), [], { vault: [] }, 0, new Set(), 0, known)
    const fire = actions.find((a) => a.id === 'buy-recipe-fire_sword')
    expect(fire?.disabled).toBe(true)
    expect(fire?.tooltipBody).toContain('Recipe already known')
  })

  it('skips recipes with no catalog def', () => {
    const actions = getBuildingActions(
      mkRecipeShop({ recipeInventory: [{ recipeId: 'unknown_recipe', quantity: 1 }] }),
    )
    expect(actions.some((a) => a.id.startsWith('buy-recipe-'))).toBe(false)
  })
})
