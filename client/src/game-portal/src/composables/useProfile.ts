import { ref, computed } from 'vue'
import type { GameplayTuning, PlayerBuffDef, PlayerProfile } from '@/types/profile'
import {
  fetchProfile,
  fetchTuning,
  getOrCreatePlayerId,
  updateLoadout as apiUpdateLoadout,
  unlockBuff as apiUnlockBuff,
} from '@/services/profileApi'

// Module-level singleton — one fetch for the entire app lifetime.
const profile = ref<PlayerProfile | null>(null)
const buffCatalog = ref<PlayerBuffDef[]>([])
const tuning = ref<GameplayTuning | null>(null)
const isLoading = ref(false)
const error = ref<string | null>(null)
let initialized = false

const DEFAULT_MAX_BUFF_SLOTS = 3

const maxBuffSlots = computed(() =>
  tuning.value?.buffSlots.maxActive ?? DEFAULT_MAX_BUFF_SLOTS,
)

const equippedBuffs = computed<PlayerBuffDef[]>(() => {
  if (!profile.value) return []
  const catalog = new Map(buffCatalog.value.map((d) => [d.id, d]))
  return profile.value.equippedBuffIds.flatMap((id) => {
    const def = catalog.get(id)
    return def ? [def] : []
  })
})

const unlockedBuffs = computed<PlayerBuffDef[]>(() => {
  if (!profile.value) return []
  const unlockedSet = new Set(profile.value.unlockedBuffIds)
  return buffCatalog.value.filter((d) => unlockedSet.has(d.id))
})

const lockedBuffs = computed<PlayerBuffDef[]>(() => {
  if (!profile.value) return buffCatalog.value.slice().sort((a, b) => a.unlockLegendPointCost - b.unlockLegendPointCost)
  const unlockedSet = new Set(profile.value.unlockedBuffIds)
  return buffCatalog.value
    .filter((d) => !unlockedSet.has(d.id))
    .sort((a, b) => a.unlockLegendPointCost - b.unlockLegendPointCost)
})

async function initialize(): Promise<void> {
  if (initialized) return
  initialized = true

  // Ensure UUID exists before any fetch
  getOrCreatePlayerId()

  isLoading.value = true
  error.value = null

  try {
    const [profileResult, tuningResult] = await Promise.all([
      fetchProfile(),
      fetchTuning().catch(() => null),
    ])
    profile.value = profileResult.profile
    buffCatalog.value = profileResult.buffCatalog
    if (tuningResult) tuning.value = tuningResult
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load profile'
  } finally {
    isLoading.value = false
  }
}

async function updateLoadout(buffIds: string[]): Promise<void> {
  isLoading.value = true
  error.value = null
  try {
    const updated = await apiUpdateLoadout(buffIds)
    profile.value = updated
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to save loadout'
  } finally {
    isLoading.value = false
  }
}

async function unlockBuff(buffId: string): Promise<void> {
  isLoading.value = true
  error.value = null
  try {
    const updated = await apiUnlockBuff(buffId)
    profile.value = updated
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to unlock buff'
  } finally {
    isLoading.value = false
  }
}

export function useProfile() {
  return {
    profile,
    buffCatalog,
    tuning,
    isLoading,
    error,
    maxBuffSlots,
    equippedBuffs,
    unlockedBuffs,
    lockedBuffs,
    initialize,
    updateLoadout,
    unlockBuff,
  }
}
