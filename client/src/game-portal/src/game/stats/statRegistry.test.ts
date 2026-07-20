import { describe, it, expect } from 'vitest'
import { STAT_DEFS, statLabel, isFractionStat, formatModifier } from './statRegistry'
import type { StatModifier } from '../network/protocol'

const mod = (stat: string, operation: 'add' | 'multiply', value: number): StatModifier =>
  ({ stat, operation, value }) as StatModifier

describe('isFractionStat', () => {
  it('is true only for stats whose value is itself a 0-1 fraction', () => {
    // Probability, and the three (1+add)×mul stats measured against a fixed
    // 1.0 baseline — for these an `add` genuinely is a percentage-point amount.
    expect(isFractionStat('critChance')).toBe(true)
    expect(isFractionStat('gatherSpeed')).toBe(true)
    expect(isFractionStat('unitProductionSpeed')).toBe(true)
    expect(isFractionStat('buildingConstructionSpeed')).toBe(true)
  })

  it('is false for raw per-unit-base stats', () => {
    for (const id of ['attackSpeed', 'damage', 'maxHp', 'moveSpeed', 'armor', 'attackRange']) {
      expect(isFractionStat(id)).toBe(false)
    }
  })

  it('is false for critMultiplier, whose baseline varies (bullseye overrides 2.0 to 2.5)', () => {
    expect(isFractionStat('critMultiplier')).toBe(false)
  })

  it('falls back to false for an unknown stat id', () => {
    expect(isFractionStat('not_a_real_stat')).toBe(false)
  })
})

describe('formatModifier', () => {
  it('renders an add on a FRACTION stat as a percentage', () => {
    expect(formatModifier(mod('critChance', 'add', 0.1))).toBe('+10% Crit Chance')
    expect(formatModifier(mod('gatherSpeed', 'add', 0.25))).toBe('+25% Gather Speed')
  })

  it('renders an add on a RAW stat as a bare number, never a percentage', () => {
    // Regression guard for the hawk_spirit bug: +0.3 on an archer's 1.5 base
    // attack speed is really +20%, so a "+30%" rendering is a lie. The delta
    // alone cannot yield a percentage — it must stay a bare number.
    const out = formatModifier(mod('attackSpeed', 'add', 0.3))
    expect(out).toBe('+0.3 Attack Speed')
    expect(out).not.toContain('%')
  })

  it('renders a multiply as a signed percent delta from 1.0 for any stat', () => {
    expect(formatModifier(mod('damage', 'multiply', 1.15))).toBe('+15% Damage')
    expect(formatModifier(mod('moveSpeed', 'multiply', 0.9))).toBe('-10% Move Speed')
  })

  it('signs negative deltas correctly in both fraction and raw forms', () => {
    expect(formatModifier(mod('critChance', 'add', -0.05))).toBe('-5% Crit Chance')
    expect(formatModifier(mod('armor', 'add', -2))).toBe('-2 Armor')
  })

  it('falls back to the raw id as the label for an unknown stat', () => {
    expect(formatModifier(mod('mystery_stat', 'add', 3))).toBe('+3 mystery_stat')
  })
})

describe('STAT_DEFS', () => {
  it('has unique ids and a non-empty label for every entry', () => {
    const ids = STAT_DEFS.map((d) => d.id)
    expect(new Set(ids).size).toBe(ids.length)
    for (const d of STAT_DEFS) {
      expect(d.label.length).toBeGreaterThan(0)
      expect(statLabel(d.id)).toBe(d.label)
    }
  })
})
