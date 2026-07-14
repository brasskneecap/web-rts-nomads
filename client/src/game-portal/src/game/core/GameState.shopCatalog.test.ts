// Regression test for the Shop menu / merchant disconnect.
//
// Bug: getShopCatalogSnapshot deduped item ids GLOBALLY across every eligible
// shop (player-owned shops processed first). With the per-shop-card Shop menu,
// an item stocked by both a player shop and a neutral merchant was attributed
// only to the player shop, so the merchant's card under-reported its stock —
// e.g. a merchant selling [sword, potion, greater_potion] showed only
// greater_potion in the menu while its in-world selection panel showed all 3.
//
// Fix: dedup PER BUILDING, so each shop card reflects that building's own
// inventory (matching getBuildingActions, which lists raw shopInventory).

import { describe, expect, it, beforeEach } from 'vitest'
import { GameState } from './GameState'
import { initItemDefs, type ItemDef } from '../maps/itemDefs'
import type { BuildingTile } from '../network/protocol'

function mkItem(id: string): ItemDef {
  return {
    id,
    displayName: id,
    iconKey: id,
    kind: 'equipment',
    tier: 'common',
   
    costGold: 10,
  }
}

function mkShop(over: Partial<BuildingTile>): BuildingTile {
  return {
    id: 'shop',
    x: 0,
    y: 0,
    buildingType: 'marketplace',
    width: 2,
    height: 2,
    occupied: true,
    visible: true,
    capabilities: ['item-purchase'],
    ...over,
  } as BuildingTile
}

beforeEach(() => {
  initItemDefs([mkItem('sword'), mkItem('potion'), mkItem('greater_potion')])
})

describe('getShopCatalogSnapshot — per-building inventory', () => {
  it('shows a neutral merchant its full stock even when items overlap a player shop', () => {
    const state = new GameState()
    state.localPlayerId = 'p1'
    state.mapConfig.buildings = [
      // Player-owned marketplace (processed first) shares two items.
      mkShop({
        id: 'market',
        buildingType: 'marketplace',
        ownerId: 'p1',
        shopInventory: [
          { itemId: 'sword', quantity: 1 },
          { itemId: 'potion', quantity: 1 },
        ],
      }),
      // Neutral merchant: discovered + unlocked, stocks the same two plus one.
      mkShop({
        id: 'merchant',
        buildingType: 'neutral-shop',
        ownerId: 'neutral',
        shopDiscovered: true,
        shopLocked: false,
        shopInventory: [
          { itemId: 'sword', quantity: 1 },
          { itemId: 'potion', quantity: 1 },
          { itemId: 'greater_potion', quantity: 1 },
        ],
      }),
    ]

    const catalog = state.getShopCatalogSnapshot()
    const merchantItems = catalog
      .filter((e) => e.purchaseBuildingId === 'merchant')
      .map((e) => e.itemId)
      .sort()

    // The merchant's card must reflect its own inventory, not just the items
    // no other shop happened to claim first.
    expect(merchantItems).toEqual(['greater_potion', 'potion', 'sword'])
  })
})
