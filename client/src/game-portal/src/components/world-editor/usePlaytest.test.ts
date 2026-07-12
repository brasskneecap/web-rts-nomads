import { describe, expect, it } from 'vitest'
import { scratchMapId, resolvePlaytestMapId } from './usePlaytest'

describe('playtest map id resolution', () => {
  it('uses the working map id when saved, else the scratch id', () => {
    expect(resolvePlaytestMapId({ id: 'my_map' } as any)).toBe('my_map')
    expect(resolvePlaytestMapId({ id: 'editor-draft' } as any)).toBe(scratchMapId)
    expect(resolvePlaytestMapId({ id: '' } as any)).toBe(scratchMapId)
  })
})
