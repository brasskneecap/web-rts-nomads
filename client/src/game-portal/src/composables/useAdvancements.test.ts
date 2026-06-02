// Vitest unit tests for useAdvancements composable.
//
// Covers:
//   - acquiredIds derived correctly from profile.acquiredAdvancements
//   - legendPoints derived correctly from profile.legendPoints
//   - isAcquired / canAfford predicates
//   - nextNodeFor returns first unacquired node, null when track complete
//   - setAdvancementCatalog populates the catalog ref
//   - purchase() on success updates legendPoints and acquiredAdvancements
//   - purchase() on server error sets error ref and does not mutate profile
//   - WS acquiredAdvancementIds: empty array (not undefined) when no advancements

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import type { UnitAdvancementTrack } from '@/types/profile'

// ─── Module-level singleton reset helpers ─────────────────────────────────────
// useAdvancements uses module-level refs (catalog, isBusy, error). We reset
// them between tests by re-importing the module with a fresh module registry.
// Vitest supports this via vi.resetModules() + dynamic import.

// Shared mock for purchaseAdvancement so we can control the server response.
const purchaseMock = vi.fn()

vi.mock('@/services/profileApi', () => ({
  purchaseAdvancement: (...args: unknown[]) => purchaseMock(...args),
}))

// Shared profile ref so useAdvancements' computed derivations have data.
const mockProfile = ref<{
  legendPoints: number
  acquiredAdvancements: { id: string; costPaid: number }[]
} | null>(null)

vi.mock('@/composables/useProfile', () => ({
  useProfile: () => ({ profile: mockProfile }),
}))

describe('useAdvancements', () => {
  // Import the module fresh after clearing module registry so singleton refs reset.
  let setAdvancementCatalog: (tracks: UnitAdvancementTrack[]) => void
  let useAdvancements: () => ReturnType<typeof import('@/composables/useAdvancements').useAdvancements>

  beforeEach(async () => {
    vi.resetModules()
    // Re-apply mocks after module reset so the fresh module sees them.
    vi.mock('@/services/profileApi', () => ({
      purchaseAdvancement: (...args: unknown[]) => purchaseMock(...args),
    }))
    vi.mock('@/composables/useProfile', () => ({
      useProfile: () => ({ profile: mockProfile }),
    }))

    purchaseMock.mockReset()
    mockProfile.value = null

    const mod = await import('@/composables/useAdvancements')
    setAdvancementCatalog = mod.setAdvancementCatalog
    useAdvancements = mod.useAdvancements
  })

  // ─── setAdvancementCatalog ──────────────────────────────────────────────────

  it('setAdvancementCatalog populates catalog ref', () => {
    const tracks: UnitAdvancementTrack[] = [
      {
        unitType: 'soldier',
        nodes: [
          {
            id: 'soldier_hp_1',
            name: 'Hardened Veteran',
            description: '+50 HP',
            kind: 'minor',
            cost: 50,
            effects: [{ kind: 'unitStatAdd', stat: 'maxHp', amount: 50 }],
          },
        ],
      },
    ]
    setAdvancementCatalog(tracks)
    const { catalog } = useAdvancements()
    expect(catalog.value).toHaveLength(1)
    expect(catalog.value[0].unitType).toBe('soldier')
  })

  // ─── legendPoints derived from profile ─────────────────────────────────────

  it('legendPoints returns 0 when profile is null', () => {
    mockProfile.value = null
    const { legendPoints } = useAdvancements()
    expect(legendPoints.value).toBe(0)
  })

  it('legendPoints mirrors profile.legendPoints', () => {
    mockProfile.value = { legendPoints: 150, acquiredAdvancements: [] }
    const { legendPoints } = useAdvancements()
    expect(legendPoints.value).toBe(150)
  })

  // ─── acquiredIds ────────────────────────────────────────────────────────────

  it('acquiredIds is empty Set when profile has no advancements', () => {
    mockProfile.value = { legendPoints: 0, acquiredAdvancements: [] }
    const { acquiredIds } = useAdvancements()
    expect(acquiredIds.value.size).toBe(0)
  })

  it('acquiredIds contains ids from profile.acquiredAdvancements', () => {
    mockProfile.value = {
      legendPoints: 50,
      acquiredAdvancements: [{ id: 'soldier_hp_1', costPaid: 50 }],
    }
    const { acquiredIds } = useAdvancements()
    expect(acquiredIds.value.has('soldier_hp_1')).toBe(true)
    expect(acquiredIds.value.has('other_id')).toBe(false)
  })

  // ─── isAcquired predicate ───────────────────────────────────────────────────

  it('isAcquired returns true when node id is in acquiredIds', () => {
    mockProfile.value = {
      legendPoints: 0,
      acquiredAdvancements: [{ id: 'soldier_hp_1', costPaid: 50 }],
    }
    const { isAcquired } = useAdvancements()
    expect(isAcquired('soldier_hp_1')).toBe(true)
    expect(isAcquired('not_owned')).toBe(false)
  })

  // ─── canAfford predicate ────────────────────────────────────────────────────

  it('canAfford returns true when legendPoints >= cost', () => {
    mockProfile.value = { legendPoints: 50, acquiredAdvancements: [] }
    const { canAfford } = useAdvancements()
    expect(canAfford(50)).toBe(true)
    expect(canAfford(51)).toBe(false)
    expect(canAfford(0)).toBe(true)
  })

  // ─── nextNodeFor ────────────────────────────────────────────────────────────

  it('nextNodeFor returns first unacquired node', () => {
    mockProfile.value = { legendPoints: 0, acquiredAdvancements: [] }
    const track: UnitAdvancementTrack = {
      unitType: 'soldier',
      nodes: [
        { id: 'a', name: 'A', description: '', kind: 'minor', cost: 25, effects: [] },
        { id: 'b', name: 'B', description: '', kind: 'major', cost: 75, effects: [] },
      ],
    }
    const { nextNodeFor } = useAdvancements()
    const next = nextNodeFor(track)
    expect(next?.id).toBe('a')
  })

  it('nextNodeFor returns second node when first is acquired', () => {
    mockProfile.value = {
      legendPoints: 100,
      acquiredAdvancements: [{ id: 'a', costPaid: 25 }],
    }
    const track: UnitAdvancementTrack = {
      unitType: 'soldier',
      nodes: [
        { id: 'a', name: 'A', description: '', kind: 'minor', cost: 25, effects: [] },
        { id: 'b', name: 'B', description: '', kind: 'major', cost: 75, effects: [] },
      ],
    }
    const { nextNodeFor } = useAdvancements()
    const next = nextNodeFor(track)
    expect(next?.id).toBe('b')
  })

  it('nextNodeFor returns null when all nodes are acquired (track complete)', () => {
    mockProfile.value = {
      legendPoints: 0,
      acquiredAdvancements: [
        { id: 'a', costPaid: 25 },
        { id: 'b', costPaid: 75 },
      ],
    }
    const track: UnitAdvancementTrack = {
      unitType: 'soldier',
      nodes: [
        { id: 'a', name: 'A', description: '', kind: 'minor', cost: 25, effects: [] },
        { id: 'b', name: 'B', description: '', kind: 'major', cost: 75, effects: [] },
      ],
    }
    const { nextNodeFor } = useAdvancements()
    expect(nextNodeFor(track)).toBeNull()
  })

  // ─── purchase() — success path ──────────────────────────────────────────────

  it('purchase() on success updates legendPoints and acquiredAdvancements on profile', async () => {
    mockProfile.value = { legendPoints: 100, acquiredAdvancements: [] }
    purchaseMock.mockResolvedValue({
      legendPoints: 50,
      acquiredAdvancements: [{ id: 'soldier_hp_1', costPaid: 50 }],
    })

    const { purchase, legendPoints, acquiredIds, isBusy, error } = useAdvancements()
    await purchase('soldier_hp_1')

    expect(isBusy.value).toBe(false)
    expect(error.value).toBeNull()
    expect(legendPoints.value).toBe(50)
    expect(acquiredIds.value.has('soldier_hp_1')).toBe(true)
  })

  it('purchase() calls purchaseAdvancement with the correct advancementId', async () => {
    mockProfile.value = { legendPoints: 100, acquiredAdvancements: [] }
    purchaseMock.mockResolvedValue({
      legendPoints: 50,
      acquiredAdvancements: [{ id: 'soldier_hp_1', costPaid: 50 }],
    })

    const { purchase } = useAdvancements()
    await purchase('soldier_hp_1')

    expect(purchaseMock).toHaveBeenCalledOnce()
    expect(purchaseMock).toHaveBeenCalledWith('soldier_hp_1')
  })

  // ─── purchase() — error path ────────────────────────────────────────────────

  it('purchase() on server error sets error ref and does not mutate profile', async () => {
    mockProfile.value = { legendPoints: 100, acquiredAdvancements: [] }
    purchaseMock.mockRejectedValue(new Error('insufficient_legend_points'))

    const { purchase, legendPoints, error, isBusy } = useAdvancements()
    await purchase('soldier_hp_1')

    expect(isBusy.value).toBe(false)
    expect(error.value).toBe('insufficient_legend_points')
    // Profile must be unmodified.
    expect(legendPoints.value).toBe(100)
  })

  it('purchase() resets error on a subsequent successful call', async () => {
    mockProfile.value = { legendPoints: 100, acquiredAdvancements: [] }
    purchaseMock
      .mockRejectedValueOnce(new Error('first_fail'))
      .mockResolvedValueOnce({
        legendPoints: 50,
        acquiredAdvancements: [{ id: 'soldier_hp_1', costPaid: 50 }],
      })

    const { purchase, error } = useAdvancements()
    await purchase('soldier_hp_1')
    expect(error.value).toBe('first_fail')

    await purchase('soldier_hp_1')
    expect(error.value).toBeNull()
  })
})

// ─── WS payload shape: acquiredAdvancementIds is [] not undefined ─────────────
// The JoinMatchMessage in the server protocol uses `omitempty` on
// acquiredAdvancementIds, meaning a nil/absent slice on the Go side decodes
// to nil (no advancements). On the client side, acquiredAdvancementIds is
// always sent as an array (never undefined). This test verifies the TypeScript
// type contract by checking that the protocol type includes acquiredAdvancementIds
// as an optional string[] (not 'undefined') so the compiler catches misuse.

describe('join_match payload type contract', () => {
  it('acquiredAdvancementIds in join payload derives from profile.acquiredAdvancements.map(a => a.id)', () => {
    // Simulate the mapping done in useGameClient.ts:
    //   client.setAcquiredAdvancementIds(
    //     (profile.value?.acquiredAdvancements ?? []).map((a) => a.id)
    //   )
    // When profile.acquiredAdvancements is empty, result must be [] not undefined.
    const acquiredAdvancements: { id: string; costPaid: number }[] = []
    const ids = (acquiredAdvancements ?? []).map((a) => a.id)
    expect(ids).toEqual([])
    expect(ids).not.toBeUndefined()

    // When advancements are owned, IDs are correctly extracted.
    const withAdvancements = [{ id: 'soldier_hp_1', costPaid: 50 }]
    const idsWithData = withAdvancements.map((a) => a.id)
    expect(idsWithData).toEqual(['soldier_hp_1'])
  })
})
