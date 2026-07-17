import { describe, it, expect } from 'vitest'
import { parsePreviewResult, type PreviewResult, type AbilityExecutionTraceEvent } from './programPreview'

describe('programPreview', () => {
  it('parses a preview result, preserving trace order', () => {
    const raw = {
      trace: [
        { t: 0, type: 'cast_started' },
        { t: 1.4, type: 'damage_applied', path: 'triggers[0].actions[1]', payload: { unit: 3, amount: 140 } },
      ],
      units: [{ index: 0, team: 'enemy', hpBefore: 200, hpAfter: 60 }],
      casterManaSpent: 40,
      runnable: true,
      warnings: [],
    }
    const res: PreviewResult = parsePreviewResult(raw)
    expect(res.trace).toHaveLength(2)
    expect(res.trace[0].type).toBe('cast_started') // order preserved
    expect(res.trace[1].t).toBe(1.4)
    expect(res.trace[1].path).toBe('triggers[0].actions[1]')
    expect(res.units[0].hpAfter).toBe(60)
    expect(res.runnable).toBe(true)
    const firstEvent: AbilityExecutionTraceEvent = res.trace[0]
    expect(firstEvent.t).toBe(0)
  })

  it('defensively coerces missing fields', () => {
    const res = parsePreviewResult({})
    expect(res.trace).toEqual([])
    expect(res.units).toEqual([])
    expect(res.warnings).toEqual([])
    expect(res.runnable).toBe(false)
    expect(res.frames).toEqual([])
  })

  it('parses per-tick frames, passing the snapshot through unmodified', () => {
    const raw = {
      trace: [],
      units: [],
      casterManaSpent: 0,
      runnable: true,
      warnings: [],
      frames: [
        {
          tick: 0,
          t: 0,
          snapshot: {
            units: [{ id: 3, hp: 60 }],
            effects: [{ id: 'e1', type: 'burn' }],
          },
        },
      ],
    }
    const res: PreviewResult = parsePreviewResult(raw)
    expect(res.frames).toHaveLength(1)
    expect(res.frames[0].tick).toBe(0)
    expect(res.frames[0].t).toBe(0)
    expect(res.frames[0].snapshot.units).toEqual([{ id: 3, hp: 60 }])
    expect(res.frames[0].snapshot.effects).toEqual([{ id: 'e1', type: 'burn' }])
  })
})
