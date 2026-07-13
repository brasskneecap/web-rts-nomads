import { describe, expect, it, vi, beforeEach } from 'vitest'
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

// A single shared mock GameClient handle. useGameClient() returns it every call
// (mirrors the real module-singleton client); tests reset its spies each run.
const { gc } = vi.hoisted(() => ({
  gc: {
    ui: { value: { paused: false } },
    init: vi.fn().mockResolvedValue(undefined),
    destroy: vi.fn(),
    sendSetPause: vi.fn(),
  },
}))
vi.mock('@/composables/useGameClient', () => ({
  useGameClient: () => gc,
}))

beforeEach(() => {
  gc.init.mockClear()
  gc.destroy.mockClear()
  gc.sendSetPause.mockClear()
  gc.ui.value.paused = false
})

describe('usePlaytest lifecycle', () => {
  it('start() inits an ephemeral match and marks playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const canvas = {} as HTMLCanvasElement
    const { playing, start } = usePlaytest(() => canvas)

    await start({ id: 'my_map' } as any)

    expect(playing.value).toBe(true)
    expect(gc.init).toHaveBeenCalledTimes(1)
    expect(gc.init).toHaveBeenCalledWith(canvas, 'my_map', { ephemeral: true })
  })

  it('start() is reentrancy-guarded once playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { start } = usePlaytest(() => ({}) as HTMLCanvasElement)
    await start({ id: 'm1' } as any)
    await start({ id: 'm1' } as any)
    expect(gc.init).toHaveBeenCalledTimes(1)
  })

  it('rejects a concurrent in-flight start()', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { start } = usePlaytest(() => ({}) as HTMLCanvasElement)
    const a = start({ id: 'm2' } as any)
    const b = start({ id: 'm2' } as any)
    await Promise.all([a, b])
    expect(gc.init).toHaveBeenCalledTimes(1)
  })

  it('stop() destroys the client and clears playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { playing, start, stop } = usePlaytest(() => ({}) as HTMLCanvasElement)
    await start({ id: 'm3' } as any)
    stop()
    expect(gc.destroy).toHaveBeenCalledTimes(1)
    expect(playing.value).toBe(false)
  })

  it('togglePause forwards the negated authoritative paused state', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { start, togglePause } = usePlaytest(() => ({}) as HTMLCanvasElement)
    await start({ id: 'm4' } as any)

    gc.ui.value.paused = false
    togglePause()
    expect(gc.sendSetPause).toHaveBeenLastCalledWith(true)

    gc.ui.value.paused = true
    togglePause()
    expect(gc.sendSetPause).toHaveBeenLastCalledWith(false)
  })

  it('togglePause is a no-op when not playing', async () => {
    const { usePlaytest } = await import('./usePlaytest')
    const { togglePause } = usePlaytest(() => ({}) as HTMLCanvasElement)
    togglePause()
    expect(gc.sendSetPause).not.toHaveBeenCalled()
  })
})
