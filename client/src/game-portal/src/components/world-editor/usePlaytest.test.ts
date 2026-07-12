import { describe, expect, it, vi } from 'vitest'
import { scratchMapId, resolvePlaytestMapId } from './usePlaytest'

describe('playtest map id resolution', () => {
  it('uses the working map id when saved, else the scratch id', () => {
    expect(resolvePlaytestMapId({ id: 'my_map' } as any)).toBe('my_map')
    expect(resolvePlaytestMapId({ id: 'editor-draft' } as any)).toBe(scratchMapId)
    expect(resolvePlaytestMapId({ id: '' } as any)).toBe(scratchMapId)
  })
})

vi.mock('@/game/maps/catalog', () => ({
  saveMapCatalogFile: vi.fn().mockResolvedValue(undefined),
}))

const { gameClientCtor } = vi.hoisted(() => ({
  gameClientCtor: vi.fn().mockImplementation(() => ({
    start: vi.fn().mockResolvedValue(undefined),
    stop: vi.fn(),
  })),
}))
vi.mock('@/game/core/GameClient', () => ({
  GameClient: gameClientCtor,
}))

describe('usePlaytest reentrancy guard', () => {
  it('short-circuits start() while already playing instead of constructing a second client', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { playing, start } = usePlaytest(() => ({}) as HTMLCanvasElement)
    const file = { id: 'my_map' } as any

    await start(file)
    expect(playing.value).toBe(true)
    expect(gameClientCtor).toHaveBeenCalledTimes(1)

    await start(file)
    expect(gameClientCtor).toHaveBeenCalledTimes(1)
  })

  it('rejects a concurrent start() call issued while a prior start() is still in flight', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { playing, start } = usePlaytest(() => ({}) as HTMLCanvasElement)
    const file = { id: 'my_map_concurrent' } as any

    // Isolate this test's call count from the shared mock's history.
    gameClientCtor.mockClear()

    // Fire both calls back-to-back without awaiting the first. The first
    // call runs synchronously up to its first `await` (inside
    // saveMapCatalogFile), setting the in-flight `starting` marker before
    // yielding. The second call therefore hits the reentrancy guard
    // synchronously and returns immediately, before either promise settles.
    const first = start(file)
    const second = start(file)

    await Promise.all([first, second])

    expect(playing.value).toBe(true)
    expect(gameClientCtor).toHaveBeenCalledTimes(1)
  })
})

describe('usePlaytest pause', () => {
  it('toggles pause, forwards set_pause to the client, and clears on stop', async () => {
    const sendSetPause = vi.fn()
    gameClientCtor.mockClear()
    gameClientCtor.mockImplementationOnce(() => ({
      start: vi.fn().mockResolvedValue(undefined),
      stop: vi.fn(),
      sendSetPause,
    }))
    const { usePlaytest } = await import('./usePlaytest')
    const { paused, start, stop, togglePause } = usePlaytest(() => ({}) as HTMLCanvasElement)

    await start({ id: 'pause_map' } as any)
    expect(paused.value).toBe(false) // a fresh match starts running

    togglePause()
    expect(paused.value).toBe(true)
    expect(sendSetPause).toHaveBeenLastCalledWith(true)

    togglePause()
    expect(paused.value).toBe(false)
    expect(sendSetPause).toHaveBeenLastCalledWith(false)

    togglePause() // pause again, then stop must clear it
    expect(paused.value).toBe(true)
    stop()
    expect(paused.value).toBe(false)
  })

  it('togglePause is a no-op when not playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { paused, togglePause } = usePlaytest(() => ({}) as HTMLCanvasElement)
    togglePause()
    expect(paused.value).toBe(false)
  })
})
