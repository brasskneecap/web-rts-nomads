import { describe, expect, it } from 'vitest'
import type { PlayerProfile } from '../types/profile'

// The mapping useGameClient performs: client.setKnownRecipeIds(profile.knownRecipeIds ?? [])
function knownRecipeIdsForJoin(profile: Pick<PlayerProfile, 'knownRecipeIds'> | null): string[] {
  return profile?.knownRecipeIds ?? []
}

describe('join_match knownRecipeIds contract', () => {
  it('defaults to [] (not undefined) when the profile is null or empty', () => {
    expect(knownRecipeIdsForJoin(null)).toEqual([])
    expect(knownRecipeIdsForJoin({ knownRecipeIds: [] })).toEqual([])
  })
  it('passes through the profile recipe ids', () => {
    expect(knownRecipeIdsForJoin({ knownRecipeIds: ['fire_sword', 'frost_sword'] }))
      .toEqual(['fire_sword', 'frost_sword'])
  })
})
