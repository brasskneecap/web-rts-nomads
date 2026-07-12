import { describe, expect, it } from 'vitest'
import { applyInstanceEdit, itemsForUnitType, perksForUnitType, ranksForUnitType } from './placedUnitInstance'
import type { PlacedUnit } from '@/game/network/protocol'
import type { UnitDef } from '@/game/maps/unitDefs'

const base: PlacedUnit = { id: 'u1', x: 1, y: 1, playerSlot: 'player1', unitType: 'soldier' }

describe('placed unit instance edits', () => {
  it('applies rank/items/perks onto the placed unit, dropping empties', () => {
    const next = applyInstanceEdit(base, { rank: 'silver', items: ['fire_sword'], perks: ['p_a'] })
    expect(next.rank).toBe('silver')
    expect(next.items).toEqual(['fire_sword'])
    expect(next.perks).toEqual(['p_a'])
    // Clearing rank drops the field (kept out of the wire).
    const cleared = applyInstanceEdit(next, { rank: '', items: [], perks: [] })
    expect(cleared.rank).toBeUndefined()
    expect(cleared.items).toBeUndefined()
    expect(cleared.perks).toBeUndefined()
  })

  it('filters perks to the unit type using the real PerkDef.unitType field', () => {
    const perkDefs = [
      { id: 'p_a', unitType: 'soldier' } as any, // eslint-disable-line @typescript-eslint/no-explicit-any
      { id: 'p_b', unitType: 'archer' } as any, // eslint-disable-line @typescript-eslint/no-explicit-any
      { id: 'p_c' } as any, // eslint-disable-line @typescript-eslint/no-explicit-any -- unitType absent = any unit
    ]
    expect(perksForUnitType(perkDefs, 'soldier').map((p) => p.id)).toEqual(['p_a', 'p_c'])
  })

  it('filters items to the unit type using the real ItemDef.allowedUnitTypes field', () => {
    const itemDefs = [
      { id: 'i_a', allowedUnitTypes: ['knight'] } as any, // eslint-disable-line @typescript-eslint/no-explicit-any
      { id: 'i_b', allowedUnitTypes: ['archer'] } as any, // eslint-disable-line @typescript-eslint/no-explicit-any
      { id: 'i_c' } as any, // eslint-disable-line @typescript-eslint/no-explicit-any -- allowedUnitTypes absent = any unit
    ]
    expect(itemsForUnitType(itemDefs, 'knight').map((i) => i.id)).toEqual(['i_a', 'i_c'])
    expect(itemsForUnitType(itemDefs, 'archer').map((i) => i.id)).toEqual(['i_b', 'i_c'])
  })

  it('returns the global rank set for a unit type present in the catalog, empty for an unknown one', () => {
    const unitDefs = [{ type: 'soldier' } as UnitDef]
    expect(ranksForUnitType(unitDefs, 'soldier')).toEqual(['bronze', 'silver', 'gold'])
    expect(ranksForUnitType(unitDefs, 'ghost-type')).toEqual([])
  })
})
