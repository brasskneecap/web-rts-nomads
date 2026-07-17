import { describe, it, expect } from 'vitest'
import { parseProgram, serializeProgram, type AbilityProgram } from './abilityProgram'

describe('abilityProgram round-trip', () => {
  it('preserves unknown top-level and config keys', () => {
    const raw = {
      entry: { type: 'unit', relations: ['self'], range: 'match_attack_range' },
      futureKey: { x: 1 },
      triggers: [{ id: 't', type: 'on_cast_complete', actions: [
        { id: 'a', type: 'deal_damage', config: { amount: 10, futureCfgKey: 'keep' } },
      ] }],
    }
    const prog: AbilityProgram = parseProgram(raw)
    const out = serializeProgram(prog) as Record<string, unknown>
    expect(JSON.stringify(out)).toContain('futureKey')
    expect(JSON.stringify(out)).toContain('keep')
  })

  it('does not leak known keys into the remainder bag', () => {
    const prog = parseProgram({ entry: { type: 'self', range: 0 }, unknownX: 1, triggers: [] })
    const out = serializeProgram(prog) as Record<string, unknown>
    // known keys present exactly once; unknown preserved
    expect(out.entry).toBeDefined()
    expect(out.triggers).toBeDefined()
    expect(out.unknownX).toBe(1)
  })

  it('round-trip is idempotent', () => {
    const raw = {
      entry: { type: 'ground_point', range: 400 },
      keepMe: true,
      triggers: [{ id: 't', type: 'on_cast_complete', actions: [
        { id: 'a', type: 'create_zone', config: { name: 'X', extra: 1 } },
      ] }],
    }
    const once = JSON.stringify(serializeProgram(parseProgram(raw)))
    const twice = JSON.stringify(serializeProgram(parseProgram(JSON.parse(once))))
    expect(twice).toBe(once)
  })
})
