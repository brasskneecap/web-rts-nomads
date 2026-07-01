import { describe, expect, it } from 'vitest'
import { initRecipeDefs, RECIPE_DEF_MAP, RECIPE_DEFS } from './recipeDefs'

describe('recipeDefs store', () => {
  it('initRecipeDefs populates the list and id→def map', () => {
    initRecipeDefs([
      { id: 'fire_sword', name: 'Fire Sword', inputs: ['broad_sword', 'fire_ring'], costGold: 150, output: 'fire_sword' },
    ])
    expect(RECIPE_DEFS).toHaveLength(1)
    const def = RECIPE_DEF_MAP.get('fire_sword')
    expect(def?.output).toBe('fire_sword')
    expect(def?.inputs).toEqual(['broad_sword', 'fire_ring'])
    expect(def?.costGold).toBe(150)
  })
})
