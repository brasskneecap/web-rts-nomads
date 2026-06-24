// Vitest harness for desktopBridge.ts. Covers:
//   - the browser-dev branch (window.__TAURI__ absent)
//   - the Tauri-present branch (window.__TAURI__ defined + invoke mocked)
//   - the settings round-trip + the localStorage migration helper
//
// Run via `npm test`. The vitest harness is configured to use the
// happy-dom environment so `window` is defined.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as bridge from './desktopBridge'

declare global {
  interface Window {
    __TAURI__?: Record<string, unknown>
  }
}

// Patchable invoke fake — installed by mock below.
const invokeMock = vi.fn()

vi.mock('@tauri-apps/api/core', () => ({
  invoke: (...args: unknown[]) => invokeMock(...args),
}))

// Patchable listen fake — captures the most recent handler so tests can
// trigger it directly. Returns an unlisten fn that increments
// unlistenCallCount on each call.
let lastListenHandler: ((evt: { payload: unknown }) => void) | undefined
let unlistenCallCount = 0
vi.mock('@tauri-apps/api/event', () => ({
  listen: (_event: string, handler: (evt: { payload: unknown }) => void) => {
    lastListenHandler = handler
    return Promise.resolve(() => {
      unlistenCallCount++
    })
  },
}))

function setTauri(present: boolean) {
  if (present) {
    ;(window as any).__TAURI__ = {}
  } else {
    delete (window as any).__TAURI__
  }
}

beforeEach(() => {
  invokeMock.mockReset()
  window.localStorage.clear()
  lastListenHandler = undefined
  unlistenCallCount = 0
})

afterEach(() => {
  setTauri(false)
})

describe('isInTauri', () => {
  it('returns false in browser dev', () => {
    setTauri(false)
    expect(bridge.isInTauri()).toBe(false)
  })

  it('returns true when window.__TAURI__ is present', () => {
    setTauri(true)
    expect(bridge.isInTauri()).toBe(true)
  })
})

describe('getSteamPlayer', () => {
  it('returns null in browser dev without invoking', async () => {
    setTauri(false)
    const result = await bridge.getSteamPlayer()
    expect(result).toBeNull()
    expect(invokeMock).not.toHaveBeenCalled()
  })

  it('forwards the invoke result when in Tauri', async () => {
    setTauri(true)
    invokeMock.mockResolvedValueOnce({ steamId64: '7656', personaName: 'gabe' })
    const result = await bridge.getSteamPlayer()
    expect(result).toEqual({ steamId64: '7656', personaName: 'gabe' })
    expect(invokeMock).toHaveBeenCalledWith('get_steam_player', undefined)
  })

  it('returns null when invoke throws (Steam unavailable degradation)', async () => {
    setTauri(true)
    invokeMock.mockRejectedValueOnce(new Error('steam_unavailable'))
    const result = await bridge.getSteamPlayer()
    expect(result).toBeNull()
  })
})

describe('settings round-trip', () => {
  it('writes to localStorage in browser dev', async () => {
    setTauri(false)
    await bridge.setSettings({ playerId: 'browser-dev-uuid' })
    expect(window.localStorage.getItem('nomads.playerId')).toBe('browser-dev-uuid')
    const snap = await bridge.getSettings()
    expect(snap.playerId).toBe('browser-dev-uuid')
    expect(invokeMock).not.toHaveBeenCalled()
  })

  it('invokes set_settings in Tauri', async () => {
    setTauri(true)
    invokeMock.mockResolvedValueOnce({ playerId: 'shell-uuid' })
    const snap = await bridge.setSettings({ playerId: 'shell-uuid' })
    expect(snap.playerId).toBe('shell-uuid')
    expect(invokeMock).toHaveBeenCalledWith('set_settings', {
      partial: { playerId: 'shell-uuid' },
    })
  })
})

describe('migratePlayerIdFromLocalStorageIfNeeded', () => {
  it('no-op in browser dev', async () => {
    setTauri(false)
    window.localStorage.setItem('nomads.playerId', 'browser-value')
    await bridge.migratePlayerIdFromLocalStorageIfNeeded()
    expect(invokeMock).not.toHaveBeenCalled()
  })

  it('copies localStorage value into settings.json on first Tauri launch', async () => {
    setTauri(true)
    window.localStorage.setItem('nomads.playerId', 'legacy-uuid')
    // get_settings returns empty snapshot.
    invokeMock.mockResolvedValueOnce({})
    // set_settings returns the post-merge snapshot.
    invokeMock.mockResolvedValueOnce({ playerId: 'legacy-uuid' })

    await bridge.migratePlayerIdFromLocalStorageIfNeeded()

    expect(invokeMock).toHaveBeenNthCalledWith(1, 'get_settings', undefined)
    expect(invokeMock).toHaveBeenNthCalledWith(2, 'set_settings', {
      partial: { playerId: 'legacy-uuid' },
    })
  })

  it('no-op when settings.json already has a player-id', async () => {
    setTauri(true)
    window.localStorage.setItem('nomads.playerId', 'should-not-be-used')
    invokeMock.mockResolvedValueOnce({ playerId: 'already-here' })

    await bridge.migratePlayerIdFromLocalStorageIfNeeded()
    // Only the get_settings call should have happened.
    expect(invokeMock).toHaveBeenCalledTimes(1)
  })

  it('no-op when neither localStorage nor settings has a player-id', async () => {
    setTauri(true)
    invokeMock.mockResolvedValueOnce({})
    await bridge.migratePlayerIdFromLocalStorageIfNeeded()
    expect(invokeMock).toHaveBeenCalledTimes(1)
  })
})

describe('packaged-build read-from-settings.json across launches', () => {
  it('simulated second launch with different mock port still reads playerId from settings', async () => {
    setTauri(true)
    // Simulating a relaunch where localStorage is empty (different port → different
    // localStorage origin) but settings.json was persisted last time.
    invokeMock.mockResolvedValueOnce({ playerId: 'persisted-via-settings' })
    const snap = await bridge.getSettings()
    expect(snap.playerId).toBe('persisted-via-settings')
  })
})

describe('reportAchievement', () => {
  it('fire-and-forget swallows errors', async () => {
    setTauri(true)
    invokeMock.mockRejectedValueOnce(new Error('steam_channel_closed'))
    await expect(bridge.reportAchievement('ACH_FIRST_WAVE_CLEARED')).resolves.toBeUndefined()
  })

  it('is a no-op in browser dev', async () => {
    setTauri(false)
    await bridge.reportAchievement('ACH_X')
    expect(invokeMock).not.toHaveBeenCalled()
  })
})

describe('openLobby extended metadata (§14R-A)', () => {
  it('forwards mapId / localLobbyId / hostPersona when present', async () => {
    setTauri(true)
    invokeMock.mockResolvedValueOnce('12345')
    const handle = await bridge.openLobby({
      maxPlayers: 4,
      mapId: 'enemy-test-small',
      localLobbyId: 'lobby-abc',
      hostPersona: 'gabe',
    })
    expect(handle).toEqual({ lobbyId: '12345' })
    expect(invokeMock).toHaveBeenCalledWith('create_lobby', {
      maxPlayers: 4,
      mapId: 'enemy-test-small',
      localLobbyId: 'lobby-abc',
      hostPersona: 'gabe',
      mapHash: '',
      mapVersion: '',
    })
  })

  it('defaults missing optional fields to empty strings', async () => {
    setTauri(true)
    invokeMock.mockResolvedValueOnce('99')
    await bridge.openLobby({ maxPlayers: 2 })
    expect(invokeMock).toHaveBeenCalledWith('create_lobby', {
      maxPlayers: 2,
      mapId: '',
      localLobbyId: '',
      hostPersona: '',
      mapHash: '',
      mapVersion: '',
    })
  })
})

describe('listSteamLobbies / getSteamLobbyData (§14R-A)', () => {
  it('listSteamLobbies returns [] in browser dev', async () => {
    setTauri(false)
    const result = await bridge.listSteamLobbies()
    expect(result).toEqual([])
    expect(invokeMock).not.toHaveBeenCalled()
  })

  it('listSteamLobbies forwards the Tauri result', async () => {
    setTauri(true)
    const entry: bridge.SteamLobbyListEntry = {
      steamLobbyId: '12345',
      hostSteamId: '76561197960287930',
      hostPersona: 'gabe',
      mapId: 'enemy-test-small',
      localLobbyId: 'lobby-abc',
      status: 'waiting',
      playerCount: 1,
      maxPlayers: 4,
      mapHash: '',
      mapVersion: '',
    }
    invokeMock.mockResolvedValueOnce([entry])
    const result = await bridge.listSteamLobbies()
    expect(result).toEqual([entry])
    expect(invokeMock).toHaveBeenCalledWith('list_steam_lobbies', undefined)
  })

  it('listSteamLobbies returns [] when Tauri throws (degraded mode)', async () => {
    setTauri(true)
    invokeMock.mockRejectedValueOnce(new Error('steam_unavailable'))
    const result = await bridge.listSteamLobbies()
    expect(result).toEqual([])
  })

  it('getSteamLobbyData returns null in browser dev', async () => {
    setTauri(false)
    const result = await bridge.getSteamLobbyData('12345')
    expect(result).toBeNull()
    expect(invokeMock).not.toHaveBeenCalled()
  })

  it('getSteamLobbyData passes the lobby id and returns the snapshot', async () => {
    setTauri(true)
    const snapshot: bridge.SteamLobbyData = {
      steamLobbyId: '12345',
      hostSteamId: '76561197960287930',
      hostPersona: 'gabe',
      mapId: 'enemy-test-small',
      localLobbyId: 'lobby-abc',
      status: 'waiting',
      matchId: '',
      maxPlayers: 4,
      members: [
        { steamId64: '76561197960287930', personaName: 'gabe' },
      ],
      mapHash: '',
      mapVersion: '',
    }
    invokeMock.mockResolvedValueOnce(snapshot)
    const result = await bridge.getSteamLobbyData('12345')
    expect(result).toEqual(snapshot)
    expect(invokeMock).toHaveBeenCalledWith('get_steam_lobby_data', {
      steamLobbyId: '12345',
    })
  })
})

describe('startSteamGame', () => {
  it('invokes start_steam_game with lobbyId + matchId in Tauri', async () => {
    setTauri(true)
    invokeMock.mockResolvedValueOnce(undefined)
    await bridge.startSteamGame('12345', 'match-abc')
    expect(invokeMock).toHaveBeenCalledWith('start_steam_game', {
      lobbyId: '12345',
      matchId: 'match-abc',
    })
  })

  it('is a no-op in browser dev', async () => {
    setTauri(false)
    await bridge.startSteamGame('12345', 'match-abc')
    expect(invokeMock).not.toHaveBeenCalled()
  })

  it('propagates Tauri errors so the SPA can surface them', async () => {
    setTauri(true)
    invokeMock.mockRejectedValueOnce(new Error('steam_unavailable'))
    await expect(bridge.startSteamGame('12345', 'match-abc')).rejects.toThrow('steam_unavailable')
  })
})

describe('onSteamLobbyStarted', () => {
  it('subscribes via tauri listen and routes the payload to the handler', async () => {
    setTauri(true)
    const handler = vi.fn()
    const unlisten = await bridge.onSteamLobbyStarted(handler)

    // Simulate the shell emitting the event.
    expect(lastListenHandler).toBeDefined()
    lastListenHandler!({ payload: { lobbyId: '12345', matchId: 'match-abc' } })
    expect(handler).toHaveBeenCalledWith({ lobbyId: '12345', matchId: 'match-abc' })

    unlisten()
    expect(unlistenCallCount).toBe(1)
  })

  it('returns a no-op unlisten in browser dev (does not subscribe)', async () => {
    setTauri(false)
    const handler = vi.fn()
    const unlisten = await bridge.onSteamLobbyStarted(handler)
    expect(lastListenHandler).toBeUndefined()
    unlisten() // should not throw
    expect(handler).not.toHaveBeenCalled()
  })
})
