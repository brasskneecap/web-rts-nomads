import { describe, expect, it } from 'vitest'
import { isShopGuardableBuildingType, allGuardGroups } from './shopGuardEditor'
import type { NeutralGroupTierSummary } from '../network/protocol'

describe('isShopGuardableBuildingType', () => {
  it('is true only for the shop types the server spawns guards for', () => {
    expect(isShopGuardableBuildingType('neutral-shop')).toBe(true)
    expect(isShopGuardableBuildingType('recipe-shop')).toBe(true)
    expect(isShopGuardableBuildingType('marketplace')).toBe(false)
    expect(isShopGuardableBuildingType('artificer')).toBe(false)
    expect(isShopGuardableBuildingType('enemy-spawnpoint')).toBe(false)
  })
})

const tiers: NeutralGroupTierSummary[] = [
  { tier: 1, groups: [{ id: 'small_raider_group', name: 'Small Raiders' }] },
  {
    tier: 3,
    groups: [
      { id: 'big_raider_group', name: 'Big Raiders' },
      { id: 'small_raider_group', name: 'Small Raiders' },
    ],
  },
]

describe('allGuardGroups', () => {
  it('returns the distinct groups across all tiers, sorted by display name', () => {
    // small_raider_group appears in both tiers → deduped; sorted by name.
    expect(allGuardGroups(tiers).map((g) => g.id)).toEqual(['big_raider_group', 'small_raider_group'])
  })
  it('returns [] for null/empty input', () => {
    expect(allGuardGroups(null)).toEqual([])
    expect(allGuardGroups([])).toEqual([])
  })
})
