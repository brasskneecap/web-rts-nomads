import { describe, expect, it } from 'vitest'
import type { AbilityExecutionTraceEvent } from '@/game/abilities/program/programPreview'
import type { UnitSnapshot } from '@/game/network/protocol'
import { damageNumbersForFrameIndex, frameIndexForTraceEvent } from './previewDamageNumbers'

// makeUnit builds a minimal UnitSnapshot stub — only the fields
// damageNumbersForFrameIndex/findCasterOwnerId actually read (id, ownerId,
// unitType, x, y). Cast to UnitSnapshot since the wire type carries many
// more fields this module never touches.
function makeUnit(over: Partial<UnitSnapshot> & { id: number; ownerId: string; x: number; y: number }): UnitSnapshot {
  return {
    unitType: 'soldier',
    color: '#fff',
    name: 'unit',
    visible: true,
    hp: 100,
    maxHp: 100,
    moving: false,
    ...over,
  } as UnitSnapshot
}

function damageEvent(t: number, unit: number, amount: number, type?: string): AbilityExecutionTraceEvent {
  return { t, type: 'damage_applied', payload: { unit, amount, ...(type ? { type } : {}) } }
}

function healEvent(t: number, unit: number, amount: number): AbilityExecutionTraceEvent {
  return { t, type: 'healing_applied', payload: { unit, amount } }
}

const CASTER_X = 0
const CASTER_Y = 0

describe('frameIndexForTraceEvent', () => {
  it('floors t/DT onto the tick grid', () => {
    expect(frameIndexForTraceEvent(0)).toBe(0)
    expect(frameIndexForTraceEvent(0.05)).toBe(1)
    expect(frameIndexForTraceEvent(0.09)).toBe(1)
  })

  it('is not knocked down a tick by float error at an exact multiple of DT', () => {
    // 7 * 0.05 === 0.35000000000000003 in IEEE-754 — without the EPS fudge
    // this would floor to 6, not 7.
    expect(frameIndexForTraceEvent(7 * 0.05)).toBe(7)
  })
})

describe('damageNumbersForFrameIndex', () => {
  const caster = makeUnit({ id: 1, ownerId: 'preview_caster', x: CASTER_X, y: CASTER_Y })
  const ally = makeUnit({ id: 2, ownerId: 'preview_caster', x: 10, y: 20 })
  const enemy = makeUnit({ id: 3, ownerId: 'preview_enemy', x: 120, y: 0, unitType: 'raider' })
  const units = [caster, ally, enemy]

  it('spawns a normal damage popup for a damage_applied event in this frame window', () => {
    const trace = [damageEvent(0.1, 3, 25, 'fire')]
    const specs = damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)
    expect(specs).toEqual([
      {
        unitId: 3,
        unitType: 'raider',
        x: 120,
        y: 0,
        amount: 25,
        isFriendly: false,
        kind: 'normal',
        damageType: 'fire',
      },
    ])
  })

  it('spawns a heal popup (no damageType) for a healing_applied event', () => {
    const trace = [healEvent(0.1, 2, 15)]
    const specs = damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)
    expect(specs).toEqual([
      {
        unitId: 2,
        unitType: 'soldier',
        x: 10,
        y: 20,
        amount: 15,
        isFriendly: true,
        kind: 'heal',
        damageType: undefined,
      },
    ])
  })

  it('marks the victim friendly when it shares the caster unit ownerId', () => {
    const trace = [damageEvent(0.1, 2, 5)]
    const specs = damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)
    expect(specs[0]?.isFriendly).toBe(true)
  })

  it('marks the victim non-friendly when it does not share the caster unit ownerId', () => {
    const trace = [damageEvent(0.1, 3, 5)]
    const specs = damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)
    expect(specs[0]?.isFriendly).toBe(false)
  })

  it('treats every victim as non-friendly when no unit sits at the caster position', () => {
    const noCasterUnits = [ally, enemy]
    const trace = [damageEvent(0.1, 2, 5)]
    const specs = damageNumbersForFrameIndex(trace, 2, noCasterUnits, CASTER_X, CASTER_Y)
    expect(specs[0]?.isFriendly).toBe(false)
  })

  it('excludes events outside this frame index window', () => {
    // frame 2 covers t in [0.10, 0.15). t=0.16 belongs to frame 3.
    const trace = [damageEvent(0.16, 3, 25)]
    expect(damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)).toEqual([])
  })

  it('only returns events belonging to the requested frame out of a mixed trace', () => {
    const trace = [damageEvent(0.0, 3, 10), damageEvent(0.1, 3, 20), healEvent(0.1, 2, 5), damageEvent(0.2, 3, 30)]
    const specs = damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)
    expect(specs.map((s) => s.amount).sort((a, b) => a - b)).toEqual([5, 20])
  })

  it('ignores trace event types other than damage_applied/healing_applied', () => {
    const trace: AbilityExecutionTraceEvent[] = [
      { t: 0.1, type: 'targets_selected', payload: { count: 1 } },
      { t: 0.1, type: 'trigger_fired' },
    ]
    expect(damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)).toEqual([])
  })

  it('skips a damage_applied event with amount <= 0', () => {
    const trace = [damageEvent(0.1, 3, 0)]
    expect(damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)).toEqual([])
  })

  it('skips a damage_applied event whose payload is missing unit/amount', () => {
    const trace: AbilityExecutionTraceEvent[] = [{ t: 0.1, type: 'damage_applied', payload: {} }]
    expect(damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)).toEqual([])
  })

  it('skips a damage_applied event whose victim id has no matching unit (already removed this frame)', () => {
    const trace = [damageEvent(0.1, 999, 25)]
    expect(damageNumbersForFrameIndex(trace, 2, units, CASTER_X, CASTER_Y)).toEqual([])
  })

  it('returns [] when the trace is empty', () => {
    expect(damageNumbersForFrameIndex([], 2, units, CASTER_X, CASTER_Y)).toEqual([])
  })
})
