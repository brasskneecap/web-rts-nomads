import { ref } from 'vue'
import type { GameplayTuning, PlayerProfile } from '@/types/profile'
import {
  fetchProfile,
  fetchTuning,
  getOrCreatePlayerId,
} from '@/services/profileApi'
import { setAdvancementCatalog } from '@/composables/useAdvancements'

// Module-level singleton — one fetch for the entire app lifetime.
const profile = ref<PlayerProfile | null>(null)
const tuning = ref<GameplayTuning | null>(null)
const isLoading = ref(false)
const error = ref<string | null>(null)
let initialized = false

async function initialize(): Promise<void> {
  if (initialized) return
  initialized = true
  await refresh()
}

// refresh re-fetches the profile from the server, overwriting the
// module-level singleton's profile ref. Use this after any server-side
// mutation the client did NOT issue itself (e.g. mid-match LP drops persisted
// via the immediate commit hook). Cheap enough to call on every Profile-view
// mount; the server handler is one file read.
async function refresh(): Promise<void> {
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
    if (profileResult.advancementCatalog) setAdvancementCatalog(profileResult.advancementCatalog)
    if (tuningResult) tuning.value = tuningResult
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load profile'
  } finally {
    isLoading.value = false
  }
}

export function useProfile() {
  return {
    profile,
    tuning,
    isLoading,
    error,
    initialize,
    refresh,
  }
}
