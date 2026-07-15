import { describe, expect, it } from 'vitest'
import type {
  PurchaseRecipeCommand,
  CraftItemCommand,
  RecipeStockEntry,
  ClientMessage,
} from './protocol'

// The wire carries ITEM ids: an item is its own recipe (ItemDef.crafting), so a
// recipe has no identity of its own to send.
describe('recipe protocol types', () => {
  it('purchase_recipe and craft_item are assignable to ClientMessage', () => {
    const buy: PurchaseRecipeCommand = { type: 'purchase_recipe', buildingId: 'rs-1', itemId: 'fire_sword' }
    const craft: CraftItemCommand = { type: 'craft_item', itemId: 'fire_sword' }
    const msgs: ClientMessage[] = [buy, craft]
    expect(msgs).toHaveLength(2)
  })

  it('RecipeStockEntry names the item the recipe makes', () => {
    const e: RecipeStockEntry = { itemId: 'fire_sword', quantity: 1 }
    expect(e.quantity).toBe(1)
  })
})
