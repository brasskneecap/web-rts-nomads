import type { GameplayTuning, PlayerProfile, ProfileUpgradeDef } from '@/types/profile'

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

export async function fetchProfile(): Promise<{ profile: PlayerProfile; profileUpgradeCatalog: ProfileUpgradeDef[] }> {
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
