// Shop buy actions in the building selection panel surface remaining stock
// as a corner-number badge (ActionItem.stockCount, rendered bottom-right by
// SelectionHud) instead of a "Stock remaining: N" line in the tooltip —
// matching the Match Menu shop cards' corner badge.

import { describe, expect, it, beforeEach } from 'vitest'
import { getBuildingActions } from './GameState'
import { initItemDefs, type ItemDef } from '../maps/itemDefs'
import type { BuildingTile } from '../network/protocol'

function mkItem(id: string): ItemDef {
  return {
    id,
    displayName: id,
    iconKey: id,
    kind: 'equipment',
    tier: 'common',
    slotKind: 'any',
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
  initItemDefs([mkItem('sword')])
})

describe('getBuildingActions — shop stock display', () => {
  it('exposes remaining stock as stockCount, not tooltip text', () => {
    const actions = getBuildingActions(
      mkShop({ shopInventory: [{ itemId: 'sword', quantity: 42 }] }),
    )

    const buy = actions.find((a) => a.id === 'buy-item-sword')
    expect(buy).toBeDefined()
    expect(buy!.stockCount).toBe(42)
    expect(buy!.tooltipBody ?? '').not.toContain('Stock remaining')
  })

  it('sold-out slots carry no stockCount and keep the sold-out tooltip', () => {
    const actions = getBuildingActions(
      mkShop({ shopInventory: [{ itemId: 'sword', quantity: 0 }] }),
    )

    const buy = actions.find((a) => a.id === 'buy-item-sword')
    expect(buy).toBeDefined()
    expect(buy!.disabled).toBe(true)
    expect(buy!.stockCount).toBeUndefined()
    expect(buy!.tooltipBody ?? '').toContain('Sold out')
  })
})
