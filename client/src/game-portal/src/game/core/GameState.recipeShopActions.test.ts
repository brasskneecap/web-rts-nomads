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

import type { BuildingTile } from '../network/protocol'

function mkRecipeShop(over: Partial<BuildingTile> = {}): BuildingTile {
  return {
    id: 'rs-1', x: 0, y: 0, buildingType: 'recipe-shop', width: 3, height: 3,
    occupied: true, visible: true, capabilities: ['recipe-purchase'],
    recipeInventory: [
      { itemId: 'fire_sword', quantity: 1 },
      { itemId: 'frost_sword', quantity: 0 },
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
    // The price to LEARN the recipe (300), NOT the craft cost the Artificer will
    // charge later (150). This button used to show the craft cost — it lied.
    expect(fire?.cost).toEqual([{ resourceId: 'gold', amount: 300, accent: '#d4a84f' }])
    expect(fire?.disabled).toBeFalsy()
    expect(frost?.disabled).toBe(true) // quantity 0 → sold out
  })

  it('greys out recipes the player already knows, with an explanatory tooltip', () => {
    // 7th positional arg is unlockedCraftableIds.
    const known = new Set(['fire_sword'])
    const actions = getBuildingActions(mkRecipeShop(), [], { vault: [] }, 0, new Set(), 0, known)
    const fire = actions.find((a) => a.id === 'buy-recipe-fire_sword')
    expect(fire?.disabled).toBe(true)
    expect(fire?.tooltipBody).toContain('Recipe already known')
  })

  it('skips recipes with no catalog def', () => {
    const actions = getBuildingActions(
      mkRecipeShop({ recipeInventory: [{ itemId: 'unknown_recipe', quantity: 1 }] }),
    )
    expect(actions.some((a) => a.id.startsWith('buy-recipe-'))).toBe(false)
  })
})
