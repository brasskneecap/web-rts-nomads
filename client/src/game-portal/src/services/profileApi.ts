import type { AcquiredAdvancement, GameplayTuning, PlayerProfile, ProfileUpgradeDef, UnitAdvancementTrack } from '@/types/profile'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''
const PLAYER_ID_KEY = 'webrts.profile.id'

export function getOrCreatePlayerId(): string {
  let id = localStorage.getItem(PLAYER_ID_KEY)
  if (!id) {
    id = crypto.randomUUID()
    localStorage.setItem(PLAYER_ID_KEY, id)
  }
  return id
}

export type ProfileApiError = Error & { code: string }

function makeProfileApiError(code: string, message: string): ProfileApiError {
  const err = new Error(message) as ProfileApiError
  err.name = 'ProfileApiError'
  err.code = code
  return err
}

function playerHeaders(): Record<string, string> {
  return {
    'Content-Type': 'application/json',
    'X-Player-ID': getOrCreatePlayerId(),
  }
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let code = `HTTP_${res.status}`
    let message = res.statusText
    try {
      const body = (await res.json()) as { error?: string; code?: string }
      if (body.code) code = body.code
      if (body.error) message = body.error
    } catch {
      // leave defaults
    }
    throw makeProfileApiError(code, message)
  }
  return res.json() as Promise<T>
}

export async function fetchProfile(): Promise<{
  profile: PlayerProfile
  profileUpgradeCatalog: ProfileUpgradeDef[]
  advancementCatalog: UnitAdvancementTrack[]
}> {
  const res = await fetch(`${API_BASE}/api/profile`, { headers: playerHeaders() })
  return handleResponse(res)
}

export async function toggleProfileUpgrade(upgradeId: string, active: boolean): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/upgrades/toggle`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ upgradeId, active }),
  })
  return handleResponse<PlayerProfile>(res)
}

export async function fetchTuning(): Promise<GameplayTuning> {
  const res = await fetch(`${API_BASE}/api/catalog/tuning`, { headers: playerHeaders() })
  return handleResponse<GameplayTuning>(res)
}

export async function fetchProfileUpgradeCatalog(): Promise<{ upgrades: ProfileUpgradeDef[] }> {
  const res = await fetch(`${API_BASE}/api/catalog/profile-upgrades`, { headers: playerHeaders() })
  return handleResponse<{ upgrades: ProfileUpgradeDef[] }>(res)
}

export async function purchaseProfileUpgrade(upgradeId: string): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/upgrades/purchase`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ upgradeId }),
  })
  return handleResponse<PlayerProfile>(res)
}

export async function refundProfileUpgrade(upgradeId: string): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/upgrades/refund`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ upgradeId }),
  })
  return handleResponse<PlayerProfile>(res)
}

export type PurchaseAdvancementResponse = {
  dominionPoints: number
  conquestBadges: number
  acquiredAdvancements: AcquiredAdvancement[]
}

export async function purchaseAdvancement(advancementId: string): Promise<PurchaseAdvancementResponse> {
  const res = await fetch(`${API_BASE}/api/profile/advancements/purchase`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ advancementId }),
  })
  return handleResponse<PurchaseAdvancementResponse>(res)
}

/**
 * Refunds all acquired advancements (paid cost returned to Dominion Points) and
 * clears the acquired list. Returns the updated dominion points + (empty)
 * acquired list. Rejected while the player is in an active match.
 */
export async function resetAdvancements(): Promise<PurchaseAdvancementResponse> {
  const res = await fetch(`${API_BASE}/api/profile/advancements/reset`, {
    method: 'POST',
    headers: playerHeaders(),
  })
  return handleResponse<PurchaseAdvancementResponse>(res)
}

/**
 * Mark a campaign level as completed for the calling player. Idempotent on
 * the server — re-completing a level is a no-op. Returns the updated profile.
 */
export async function markCampaignLevelComplete(levelId: string): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/campaign/complete-level`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ levelId }),
  })
  return handleResponse<PlayerProfile>(res)
}

/**
 * Record which objectives the player completed during a specific campaign
 * level attempt. Batched at match end (§15 recap dismiss). The server merges
 * the provided objective IDs into the existing sorted set at
 * `profile.completedCampaignObjectives["<campaignId>/<levelId>"]`. Idempotent:
 * repeat calls with the same payload leave state unchanged.
 *
 * Each entry carries an optional `rewardDominionPoints` amount. The server
 * credits DP for objective IDs that are newly added to the persistent set
 * (i.e. first-ever completion). Subsequent calls with the same ID are
 * idempotent and do not re-award DP.
 *
 * Passing an empty `objectives` array is intentionally valid (the recap
 * dismiss handler always POSTs at match end, even for defeats with zero
 * completions). Returns the updated profile.
 */
export async function markCampaignObjectivesComplete(
  campaignId: string,
  levelId: string,
  objectives: { id: string; rewardDominionPoints?: number; rewardConquestBadges?: number }[],
): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/campaign/complete-objectives`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ campaignId, levelId, objectives }),
  })
  return handleResponse<PlayerProfile>(res)
}

/**
 * DEV-ONLY: grant Dominion Points to the calling player for testing. Returns the
 * updated profile. The endpoint is intentionally ungated for dev iteration —
 * callers in the UI should label it clearly as a dev affordance.
 */
export async function devGrantDominionPoints(amount: number): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/dev/grant-dominion-points`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ amount }),
  })
  return handleResponse<PlayerProfile>(res)
}

/**
 * DEV-ONLY: grant Conquest Badges to the calling player for testing. Returns
 * the updated profile. Mirrors devGrantDominionPoints — label clearly as dev.
 */
export async function devGrantConquestBadges(amount: number): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/dev/grant-conquest-badges`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ amount }),
  })
  return handleResponse<PlayerProfile>(res)
}

/**
 * DEV-ONLY: hard-reset the calling player's profile back to a fresh state —
 * wipes DP, stats, upgrades, advancements, and all campaign progress. The
 * server refuses with HTTP 409 / `player_in_match` if the caller is currently
 * in an active match. Returns the updated (now-empty) profile.
 */
export async function devResetProfile(): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/dev/reset`, {
    method: 'POST',
    headers: playerHeaders(),
  })
  return handleResponse<PlayerProfile>(res)
}

/** True when this SPA tab is a remote multiplayer client proxying to a host
 *  (Steam joiner or direct-connect joiner). Mirrors NetworkClient's WS-URL
 *  resolution. The host / single-player return false and rely on the
 *  server-side dominion-point commit. */
export function isRemoteProxyClient(): boolean {
  try {
    if (sessionStorage.getItem('webrts.steam.proxyActive') === '1') return true
    if (sessionStorage.getItem('webrts.directConnect.proxyToken')) return true
  } catch {
    // sessionStorage can throw in sandboxed contexts — treat as non-proxy.
  }
  return false
}

/** Persist end-of-match dominion points to THIS machine's local profile.
 *  Idempotent on the server by matchId. Used by a remote joiner, whose
 *  earned DP the host could only commit to the host's own disk. Returns the
 *  updated profile. */
export async function awardMatchDominionPoints(matchId: string, amount: number): Promise<PlayerProfile> {
  const res = await fetch(`${API_BASE}/api/profile/match/award-dominion-points`, {
    method: 'POST',
    headers: playerHeaders(),
    body: JSON.stringify({ matchId, amount }),
  })
  return handleResponse<PlayerProfile>(res)
}
