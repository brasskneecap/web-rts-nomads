import { describe, expect, it } from 'vitest'
import { applyInstanceEdit, perksForUnitType, ranksForUnitType } from './placedUnitInstance'
import type { PlacedUnit } from '@/game/network/protocol'
import { initPathsByUnitType, type UnitDef } from '@/game/maps/unitDefs'

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

  it('filters perks to a unit type by path association (generic + owned paths)', () => {
    initPathsByUnitType({ soldier: ['berserker', 'vanguard'], archer: ['marksman', 'trapper'] })
    const perkDefs = [
      { id: 'p_berserker', path: 'berserker' } as any, // eslint-disable-line @typescript-eslint/no-explicit-any
      { id: 'p_marksman', path: 'marksman' } as any, // eslint-disable-line @typescript-eslint/no-explicit-any
      { id: 'p_generic' } as any, // eslint-disable-line @typescript-eslint/no-explicit-any -- no association = any unit
    ]
    // soldier keeps its own path's perk + the generic one, drops the archer-path perk.
    expect(perksForUnitType(perkDefs, 'soldier').map((p) => p.id)).toEqual(['p_berserker', 'p_generic'])
  })

  // Items are deliberately NOT filtered by unit type: any unit can equip any
  // item (perks above are the only per-unit-type restriction).

  it('returns the global rank set for a unit type present in the catalog, empty for an unknown one', () => {
    const unitDefs = [{ type: 'soldier' } as UnitDef]
    expect(ranksForUnitType(unitDefs, 'soldier')).toEqual(['bronze', 'silver', 'gold'])
    expect(ranksForUnitType(unitDefs, 'ghost-type')).toEqual([])
  })
})
