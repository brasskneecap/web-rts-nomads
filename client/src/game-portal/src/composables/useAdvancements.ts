import { computed, ref } from 'vue'
import type { UnitAdvancementTrack } from '@/types/profile'
import { useProfile } from '@/composables/useProfile'
import { purchaseAdvancement, resetAdvancements } from '@/services/profileApi'

// Module-level singleton — catalog is populated by setAdvancementCatalog(),
// which useProfile's refresh() calls immediately after profile resolves.
const catalog = ref<UnitAdvancementTrack[]>([])
const isBusy = ref(false)
const error = ref<string | null>(null)

export function setAdvancementCatalog(tracks: UnitAdvancementTrack[]): void {
  catalog.value = tracks
}

export function useAdvancements() {
  const { profile } = useProfile()

  const acquiredIds = computed<Set<string>>(
    () => new Set((profile.value?.acquiredAdvancements ?? []).map((a) => a.id)),
  )

  const dominionPoints = computed<number>(() => profile.value?.dominionPoints ?? 0)

  const conquestBadges = computed<number>(() => profile.value?.conquestBadges ?? 0)

  function isAcquired(nodeId: string): boolean {
    return acquiredIds.value.has(nodeId)
  }

  function canAfford(cost: number): boolean {
    return dominionPoints.value >= cost
  }

  function canAcquire(node: UnitAdvancementTrack['nodes'][number]): boolean {
    if (!canAfford(node.cost)) return false
    if (node.kind === 'major' && conquestBadges.value < 1) return false
    return true
  }

  // nextNodeFor returns the first unacquired node on a track, or null if the
  // track is complete.
  function nextNodeFor(track: UnitAdvancementTrack): UnitAdvancementTrack['nodes'][number] | null {
    return track.nodes.find((n) => !acquiredIds.value.has(n.id)) ?? null
  }

  async function purchase(advancementId: string): Promise<void> {
    isBusy.value = true
    error.value = null
    try {
      const updated = await purchaseAdvancement(advancementId)
      // Mutate the profile singleton in place so every consumer reactively
      // updates (dominionPoints display in ProfileView, balance in Advancements).
      const p = profile.value
      if (p) {
        p.dominionPoints = updated.dominionPoints
        p.conquestBadges = updated.conquestBadges
        p.acquiredAdvancements = updated.acquiredAdvancements
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Purchase failed'
    } finally {
      isBusy.value = false
    }
  }

  // Refund all acquired advancements and clear them — a dev/testing affordance
  // for comparing unit behavior with vs without advancements. The refund lets
  // the player immediately re-buy.
  async function reset(): Promise<void> {
    isBusy.value = true
    error.value = null
    try {
      const updated = await resetAdvancements()
      const p = profile.value
      if (p) {
        p.dominionPoints = updated.dominionPoints
        p.conquestBadges = updated.conquestBadges
        p.acquiredAdvancements = updated.acquiredAdvancements
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Reset failed'
    } finally {
      isBusy.value = false
    }
  }

  return {
    catalog,
    acquiredIds,
    dominionPoints,
    conquestBadges,
    isBusy,
    error,
    isAcquired,
    canAfford,
    canAcquire,
    nextNodeFor,
    purchase,
    reset,
  }
}
