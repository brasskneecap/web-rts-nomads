import { describe, expect, it } from 'vitest'
import type { AbilityEntryDef } from '@/game/abilities/program/abilityProgram'
import { humanizeEntryType, summarizeEntry } from './summarizeEntry'

describe('humanizeEntryType', () => {
  it('labels known entry types', () => {
    expect(humanizeEntryType('ground_point')).toBe('Ground point')
    expect(humanizeEntryType('unit')).toBe('Unit')
    expect(humanizeEntryType('no_target')).toBe('No target')
  })
})

describe('summarizeEntry', () => {
  it('summarizes a ground-point entry with relations and range', () => {
    const entry: AbilityEntryDef = { type: 'ground_point', relations: ['enemy'], range: 400 }
    expect(summarizeEntry(entry)).toBe('Ground point · enemies · range 400')
  })

  it('summarizes a unit entry with the match-attack-range sentinel', () => {
    const entry: AbilityEntryDef = { type: 'unit', relations: ['self', 'ally'], range: 'match_attack_range' }
    expect(summarizeEntry(entry)).toBe('Unit · self/allies · match attack range')
  })

  it('omits the relations segment when none are authored', () => {
    const entry: AbilityEntryDef = { type: 'unit', range: 300 }
    expect(summarizeEntry(entry)).toBe('Unit · range 300')
  })

  it('omits range for no_target and passive entries', () => {
    expect(summarizeEntry({ type: 'no_target', range: 0 })).toBe('No target')
    expect(summarizeEntry({ type: 'passive', range: 0 })).toBe('Passive')
  })

  it('omits range for a self entry', () => {
    expect(summarizeEntry({ type: 'self', range: 0 })).toBe('Self')
  })

  it('returns an empty string for a missing entry', () => {
    expect(summarizeEntry(undefined)).toBe('')
    expect(summarizeEntry(null)).toBe('')
  })
})
