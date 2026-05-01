import { ref } from 'vue'
import { usePlayer } from './usePlayer'

export type Lobby = {
  id: string
  mapId: string
  mapName: string
  hostPlayerId: string
  players: string[]
  maxPlayers: number
  createdAt: number
}

function makeid(): string {
  return Math.random().toString(36).slice(2, 9)
}

const _lobbies = ref<Lobby[]>([])

export function useLobbies() {
  const { playerId } = usePlayer()

  const lobbies = _lobbies as Readonly<typeof _lobbies>

  function createLobby(opts: { mapId: string; mapName: string; hostPlayerId: string; maxPlayers?: number }): string {
    const id = makeid()
    _lobbies.value.unshift({
      id,
      mapId: opts.mapId,
      mapName: opts.mapName,
      hostPlayerId: opts.hostPlayerId,
      players: [opts.hostPlayerId],
      maxPlayers: opts.maxPlayers ?? 4,
      createdAt: Date.now(),
    })
    return id
  }

  function joinLobby(id: string): void {
    const lobby = _lobbies.value.find((l) => l.id === id)
    if (!lobby) return
    if (!lobby.players.includes(playerId.value)) {
      lobby.players.push(playerId.value)
    }
  }

  function leaveLobby(id: string): void {
    const lobby = _lobbies.value.find((l) => l.id === id)
    if (!lobby) return
    lobby.players = lobby.players.filter((p) => p !== playerId.value)
    if (lobby.players.length === 0) {
      _lobbies.value = _lobbies.value.filter((l) => l.id !== id)
    }
  }

  function getLobby(id: string): Lobby | undefined {
    return _lobbies.value.find((l) => l.id === id)
  }

  return { lobbies, createLobby, joinLobby, leaveLobby, getLobby }
}
