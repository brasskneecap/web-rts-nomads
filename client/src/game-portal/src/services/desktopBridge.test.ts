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
