import { describe, expect, it } from 'vitest'
import type {
  PurchaseRecipeCommand,
  CraftItemCommand,
  RecipeStockEntry,
  ClientMessage,
} from './protocol'

describe('recipe protocol types', () => {
  it('purchase_recipe and craft_item are assignable to ClientMessage', () => {
    const buy: PurchaseRecipeCommand = { type: 'purchase_recipe', buildingId: 'rs-1', recipeId: 'fire_sword' }
    const craft: CraftItemCommand = { type: 'craft_item', recipeId: 'fire_sword' }
    const msgs: ClientMessage[] = [buy, craft]
    expect(msgs).toHaveLength(2)
  })

  it('RecipeStockEntry has recipeId + quantity', () => {
    const e: RecipeStockEntry = { recipeId: 'fire_sword', quantity: 1 }
    expect(e.quantity).toBe(1)
  })
})
