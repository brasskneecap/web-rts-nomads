import { describe, it, expect } from 'vitest'
import type { AbilityActionDef, LoopVar } from '@/game/abilities/program/abilityProgram'
import { readLoop, nextVarName, withVarAdded, withVarField, withVarRemoved, withVarStepMode } from './loopEditor'

function loopAction(): AbilityActionDef {
  return {
    id: 'chain',
    type: 'loop',
    config: {
      iterations: 3,
      vars: [{ name: 'a', start: 65, step: -5 }],
      body: [
        { id: 'dmg', type: 'deal_damage', config: { amount: 'a', type: 'lightning' } },
        { id: 'gap', type: 'wait', config: { seconds: 0.12 } },
      ],
    },
  }
}

describe('readLoop', () => {
  it('reads iterations, vars, and body from a loop action', () => {
    const v = readLoop(loopAction())
    expect(v).not.toBeNull()
    expect(v!.iterations).toBe(3)
    expect(v!.vars).toEqual([{ name: 'a', start: 65, step: -5 }])
    expect(v!.body.map((b) => b.type)).toEqual(['deal_damage', 'wait'])
  })

  it('returns null for a non-loop action', () => {
    expect(readLoop({ id: 'x', type: 'deal_damage', config: { amount: 5 } })).toBeNull()
  })

  it('tolerates a loop with no config', () => {
    const v = readLoop({ id: 'x', type: 'loop' })
    expect(v).toEqual({ iterations: 0, vars: [], body: [], stepFirst: false })
  })

  it('reads stepFirst (default false, true when set)', () => {
    expect(readLoop(loopAction())!.stepFirst).toBe(false)
    const a = loopAction()
    ;(a.config as Record<string, unknown>).stepFirst = true
    expect(readLoop(a)!.stepFirst).toBe(true)
  })
})

describe('nextVarName', () => {
  it('assigns letters alphabetically, skipping used ones', () => {
    expect(nextVarName([])).toBe('a')
    expect(nextVarName([{ name: 'a', start: 0, step: 0 }])).toBe('b')
    expect(nextVarName([{ name: 'a', start: 0, step: 0 }, { name: 'c', start: 0, step: 0 }])).toBe('b')
  })
  it('returns null once all 26 are used', () => {
    const all = Array.from({ length: 26 }, (_, i) => ({ name: String.fromCharCode(97 + i), start: 0, step: 0 }))
    expect(nextVarName(all)).toBeNull()
  })
})

describe('immutable var edits', () => {
  it('withVarAdded appends the next letter with zeroed start/step', () => {
    expect(withVarAdded([{ name: 'a', start: 65, step: -5 }])).toEqual([
      { name: 'a', start: 65, step: -5 },
      { name: 'b', start: 0, step: 0 },
    ])
  })
  it('withVarRemoved drops the named variable', () => {
    expect(withVarRemoved([{ name: 'a', start: 1, step: 0 }, { name: 'b', start: 2, step: 0 }], 'a')).toEqual([
      { name: 'b', start: 2, step: 0 },
    ])
  })
  it('withVarField edits start/step (negatives allowed) without mutating input', () => {
    const vars = [{ name: 'a', start: 65, step: -5 }]
    expect(withVarField(vars, 'a', 'step', -8)).toEqual([{ name: 'a', start: 65, step: -8 }])
    expect(vars[0]).toEqual({ name: 'a', start: 65, step: -5 }) // original untouched
  })

  it('withVarStepMode sets the step mode (number/percent) immutably', () => {
    const vars: LoopVar[] = [{ name: 'a', start: 100, step: -10 }]
    expect(withVarStepMode(vars, 'a', 'percent')).toEqual([{ name: 'a', start: 100, step: -10, stepMode: 'percent' }])
    expect(vars[0].stepMode).toBeUndefined() // original untouched
  })
})
