import { describe, expect, it } from 'vitest'
import { formatPerkTooltip } from './perkTooltip'
import type { PerkDef } from '../maps/perkDefs'
import type { Unit } from './GameState'

// Minimal unit stub — formatPerkTooltip only reads effectiveTrap and
// perkIds off it, both of which are irrelevant to the fallback-order tests
// below (no trap/owned-perk branch templates involved).
function stubUnit(): Unit {
  return { perkIds: [] } as unknown as Unit
}

function basePerk(overrides: Partial<PerkDef>): PerkDef {
  return {
    id: 'test_perk',
    displayName: 'Test Perk',
    config: {},
    ...overrides,
  } as PerkDef
}

describe('formatPerkTooltip fallback order', () => {
  it('uses tooltipTemplate when non-empty, even if generatedDescription is also set', () => {
    const def = basePerk({
      tooltipTemplate: 'Deals {damage} damage.',
      config: { damage: 10 },
      generatedDescription: '+999% Damage.',
      description: 'fallback text',
    })
    expect(formatPerkTooltip(def, stubUnit())).toBe('Deals 10 damage.')
  })

  it('falls back to generatedDescription when tooltipTemplate is absent', () => {
    const def = basePerk({
      generatedDescription: '+90 Max Health.',
      description: 'fallback text',
    })
    expect(formatPerkTooltip(def, stubUnit())).toBe('+90 Max Health.')
  })

  it('falls back to description when neither tooltipTemplate nor generatedDescription is set', () => {
    const def = basePerk({ description: 'plain description' })
    expect(formatPerkTooltip(def, stubUnit())).toBe('plain description')
  })

  it('returns empty string when template, generatedDescription, and description are all absent', () => {
    const def = basePerk({})
    expect(formatPerkTooltip(def, stubUnit())).toBe('')
  })
})
