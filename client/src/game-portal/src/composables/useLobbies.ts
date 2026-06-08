import { ref } from 'vue'
import type { Lobby } from '@/game/network/protocol'

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? ''

const lobbies = ref<readonly Lobby[]>([])

async function apiRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...init,
  })
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(text || `Server error ${res.status}`)
  }
  return res.json() as Promise<T>
}

async function refreshList(): Promise<void> {
  const data = await apiRequest<{ lobbies: Lobby[] }>('/lobbies')
  lobbies.value = data.lobbies
}

async function fetchLobby(id: string): Promise<Lobby | null> {
  const res = await fetch(`${API_BASE}/lobbies/${encodeURIComponent(id)}`, {
    headers: { 'Content-Type': 'application/json' },
  })
  if (res.status === 404) return null
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(text || `Server error ${res.status}`)
  }
  const data = (await res.json()) as { lobby: Lobby }
  return data.lobby
}

async function createLobby(opts: {
  mapId: string
  hostPlayerId: string
  /** Optional campaign-level identifier. When provided, the server installs
   *  that level's authored objectives on the GameState at match start.
   *  Custom Game callers omit this. */
  campaignLevelId?: string
}): Promise<Lobby> {
  const body: Record<string, unknown> = {
    mapId: opts.mapId,
    hostPlayerId: opts.hostPlayerId,
  }
  if (opts.campaignLevelId) body.campaignLevelId = opts.campaignLevelId
  const data = await apiRequest<{ lobby: Lobby }>('/lobbies', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return data.lobby
}

async function joinLobby(opts: { id: string; playerId: string }): Promise<Lobby> {
  const data = await apiRequest<{ lobby: Lobby }>(`/lobbies/${encodeURIComponent(opts.id)}/join`, {
    method: 'POST',
    body: JSON.stringify({ playerId: opts.playerId }),
  })
  return data.lobby
}

async function leaveLobby(opts: { id: string; playerId: string }): Promise<Lobby | null> {
  const res = await fetch(`${API_BASE}/lobbies/${encodeURIComponent(opts.id)}/leave`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ playerId: opts.playerId }),
  })
  if (res.status === 404) return null
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText)
    throw new Error(text || `Server error ${res.status}`)
  }
  const data = (await res.json()) as { lobby: Lobby }
  return data.lobby
}

async function startLobby(opts: { id: string; playerId: string }): Promise<Lobby> {
  const data = await apiRequest<{ lobby: Lobby }>(`/lobbies/${encodeURIComponent(opts.id)}/start`, {
    method: 'POST',
    body: JSON.stringify({ playerId: opts.playerId }),
  })
  return data.lobby
}

export function useLobbies() {
  return {
    lobbies,
    refreshList,
    fetchLobby,
    createLobby,
    joinLobby,
    leaveLobby,
    startLobby,
  }
}
