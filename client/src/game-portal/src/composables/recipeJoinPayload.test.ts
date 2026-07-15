import { describe, expect, it } from 'vitest'
import type { PlayerProfile } from '../types/profile'

// The mapping useGameClient performs: client.setKnownCraftableIds(profile.knownCraftableIds ?? [])
function knownCraftableIdsForJoin(profile: Pick<PlayerProfile, 'knownCraftableIds'> | null): string[] {
  return profile?.knownCraftableIds ?? []
}

describe('join_match knownCraftableIds contract', () => {
  it('defaults to [] (not undefined) when the profile is null or empty', () => {
    expect(knownCraftableIdsForJoin(null)).toEqual([])
    expect(knownCraftableIdsForJoin({ knownCraftableIds: [] })).toEqual([])
  })
  it('passes through the profile recipe ids', () => {
    expect(knownCraftableIdsForJoin({ knownCraftableIds: ['fire_sword', 'frost_sword'] }))
      .toEqual(['fire_sword', 'frost_sword'])
  })
})
