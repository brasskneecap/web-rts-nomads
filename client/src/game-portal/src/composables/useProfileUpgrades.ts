import { ref, computed } from 'vue'
import type { Ref, ComputedRef } from 'vue'
import type { ProfileUpgradeDef } from '@/types/profile'
import {
  fetchProfileUpgradeCatalog,
  purchaseProfileUpgrade,
  refundProfileUpgrade,
  toggleProfileUpgrade,
} from '@/services/profileApi'
import { useProfile } from '@/composables/useProfile'

// Module-level singleton — catalog is fetched once for the app lifetime.
const catalog = ref<ProfileUpgradeDef[]>([])
const isBusy = ref(false)
const error = ref<string | null>(null)
let catalogLoaded = false

async function loadCatalog(): Promise<void> {
  if (catalogLoaded) return
  catalogLoaded = true
  try {
    const result = await fetchProfileUpgradeCatalog()
    catalog.value = result.upgrades
  } catch (err) {
    // Mark as not loaded so the caller can retry.
    catalogLoaded = false
    error.value = err instanceof Error ? err.message : 'Failed to load upgrade catalog'
  }
}

async function purchase(upgradeId: string): Promise<void> {
  isBusy.value = true
  error.value = null
  try {
    const updated = await purchaseProfileUpgrade(upgradeId)
    useProfile().profile.value = updated
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Purchase failed'
  } finally {
    isBusy.value = false
  }
}

async function refund(upgradeId: string): Promise<void> {
  isBusy.value = true
  error.value = null
  try {
    const updated = await refundProfileUpgrade(upgradeId)
    useProfile().profile.value = updated
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Refund failed'
  } finally {
    isBusy.value = false
  }
}

async function toggle(upgradeId: string, active: boolean): Promise<void> {
  isBusy.value = true
  error.value = null
  try {
    const updated = await toggleProfileUpgrade(upgradeId, active)
    useProfile().profile.value = updated
  } catch (err) {
    // The server returns {error: "unknown_upgrade" | "not_owned", message} on 400.
    // ProfileApiError sets err.code; for display purposes the message is enough.
    error.value = err instanceof Error ? err.message : 'Toggle failed'
  } finally {
    isBusy.value = false
  }
}

async function initialize(): Promise<void> {
  await loadCatalog()
}

export function useProfileUpgrades(): {
  catalog: Ref<ProfileUpgradeDef[]>
  ownedRanks: ComputedRef<Record<string, number>>
  activeUpgradeIds: ComputedRef<string[]>
  dominionPoints: ComputedRef<number>
  isBusy: Ref<boolean>
  error: Ref<string | null>
  initialize: () => Promise<void>
  purchase: (upgradeId: string) => Promise<void>
  refund: (upgradeId: string) => Promise<void>
  toggle: (upgradeId: string, active: boolean) => Promise<void>
  isActive: (upgradeId: string) => boolean
} {
  const { profile } = useProfile()

  const ownedRanks = computed<Record<string, number>>(
    () => profile.value?.ownedUpgradeRanks ?? {},
  )

  const activeUpgradeIds = computed<string[]>(
    () => profile.value?.activeUpgradeIds ?? [],
  )

  const dominionPoints = computed<number>(
    () => profile.value?.dominionPoints ?? 0,
  )

  // Expose a Set-based lookup for O(1) isActive checks in the template.
  // The Set is recomputed whenever activeUpgradeIds changes.
  const activeSet = computed(() => new Set(activeUpgradeIds.value))

  function isActive(upgradeId: string): boolean {
    return activeSet.value.has(upgradeId)
  }

  return {
    catalog,
    ownedRanks,
    activeUpgradeIds,
    dominionPoints,
    isBusy,
    error,
    initialize,
    purchase,
    refund,
    toggle,
    isActive,
  }
}
