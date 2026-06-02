import { computed, ref } from 'vue'
import type { UnitAdvancementTrack } from '@/types/profile'
import { useProfile } from '@/composables/useProfile'
import { purchaseAdvancement } from '@/services/profileApi'

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

  const legendPoints = computed<number>(() => profile.value?.legendPoints ?? 0)

  function isAcquired(nodeId: string): boolean {
    return acquiredIds.value.has(nodeId)
  }

  function canAfford(cost: number): boolean {
    return legendPoints.value >= cost
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
      // updates (legendPoints display in ProfileView, balance in Advancements).
      const p = profile.value
      if (p) {
        p.legendPoints = updated.legendPoints
        p.acquiredAdvancements = updated.acquiredAdvancements
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'Purchase failed'
    } finally {
      isBusy.value = false
    }
  }

  return {
    catalog,
    acquiredIds,
    legendPoints,
    isBusy,
    error,
    isAcquired,
    canAfford,
    nextNodeFor,
    purchase,
  }
}
