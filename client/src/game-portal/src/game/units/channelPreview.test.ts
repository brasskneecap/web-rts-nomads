import { describe, expect, it } from 'vitest'
import type { AuthoredAbilityDef } from '@/game/abilities/abilityEditorForm'
import { channelTickPhase, pickChannelAbility } from './channelPreview'

function defs(list: AuthoredAbilityDef[]): Map<string, AuthoredAbilityDef> {
  return new Map(list.map((d) => [d.id, d]))
}

describe('pickChannelAbility', () => {
  const byId = defs([
    { id: 'arcane_bolt' }, // no channelType -> not a channel
    { id: 'siphon_life', channelType: 'beam', tickIntervalSeconds: 0.5 },
    { id: 'drain', channelType: 'beam' }, // channel, but no tick interval authored
  ])

  it('returns null when the unit has no abilities', () => {
    expect(pickChannelAbility(undefined, byId)).toBeNull()
    expect(pickChannelAbility([], byId)).toBeNull()
  })

  it('returns null when none of the unit\'s abilities channel', () => {
    expect(pickChannelAbility(['arcane_bolt'], byId)).toBeNull()
  })

  it('returns null when the ability id is not in the catalog', () => {
    expect(pickChannelAbility(['unknown_id'], byId)).toBeNull()
  })

  it('returns the first channeling ability with its tick interval', () => {
    expect(pickChannelAbility(['arcane_bolt', 'siphon_life'], byId)).toEqual({
      id: 'siphon_life',
      tickIntervalSeconds: 0.5,
    })
  })

  it('surfaces a channeling ability even when tickIntervalSeconds is absent', () => {
    expect(pickChannelAbility(['drain'], byId)).toEqual({
      id: 'drain',
      tickIntervalSeconds: undefined,
    })
  })
})

describe('channelTickPhase', () => {
  it('returns 0 when the tick interval is missing or non-positive', () => {
    expect(channelTickPhase(400, undefined)).toBe(0)
    expect(channelTickPhase(400, 0)).toBe(0)
    expect(channelTickPhase(400, -1)).toBe(0)
  })

  it('ramps 0 -> 1 across each tick interval and wraps', () => {
    // interval 0.5s = 500ms
    expect(channelTickPhase(0, 0.5)).toBe(0)
    expect(channelTickPhase(250, 0.5)).toBeCloseTo(0.5, 5)
    expect(channelTickPhase(500, 0.5)).toBe(0) // wraps at the tick boundary
    expect(channelTickPhase(750, 0.5)).toBeCloseTo(0.5, 5)
  })

  it('clamps a negative elapsed to phase 0', () => {
    expect(channelTickPhase(-100, 0.5)).toBe(0)
  })
})
