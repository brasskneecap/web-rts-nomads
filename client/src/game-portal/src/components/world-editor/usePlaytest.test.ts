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
})
