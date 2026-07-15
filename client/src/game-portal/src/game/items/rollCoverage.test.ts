import { describe, expect, it } from 'vitest'
import { analyzeCoverage } from './rollCoverage'

const r = (min: number, max: number, label = `${min}-${max}`) => ({ min, max, label })

describe('analyzeCoverage', () => {
  it('reports a complete die with no errors and per-band percentages', () => {
    const rep = analyzeCoverage(100, [r(1, 50, 'a'), r(51, 100, 'b')])
    expect(rep.complete).toBe(true)
    expect(rep.errors).toEqual([])
    expect(rep.bands.map((b) => [b.label, b.percent])).toEqual([['a', 50], ['b', 50]])
  })

  it('flags a gap, and includes the uncovered stretch as a band', () => {
    const rep = analyzeCoverage(100, [r(1, 50, 'a'), r(61, 100, 'b')])
    expect(rep.complete).toBe(false)
    expect(rep.errors.some((e) => e.includes('51') && e.includes('60'))).toBe(true)
    const gap = rep.bands.find((b) => b.uncovered)
    expect(gap).toMatchObject({ min: 51, max: 60, percent: 10 })
  })

  it('flags a trailing gap', () => {
    const rep = analyzeCoverage(100, [r(1, 50, 'a')])
    expect(rep.complete).toBe(false)
    expect(rep.errors.some((e) => e.includes('51') && e.includes('100'))).toBe(true)
  })

  it('flags an overlap', () => {
    const rep = analyzeCoverage(100, [r(1, 60, 'a'), r(50, 100, 'b')])
    expect(rep.complete).toBe(false)
    expect(rep.errors.some((e) => e.toLowerCase().includes('claimed twice'))).toBe(true)
  })

  it('flags a range outside the die', () => {
    const rep = analyzeCoverage(100, [r(1, 120, 'a')])
    expect(rep.complete).toBe(false)
    expect(rep.errors.some((e) => e.includes('outside'))).toBe(true)
  })

  it('reports the small die of a subtable (maxRoll 15)', () => {
    // basic_weapons: broad_sword 1-10, scimitar 11-15.
    const rep = analyzeCoverage(15, [r(1, 10, 'broad_sword'), r(11, 15, 'scimitar')])
    expect(rep.complete).toBe(true)
    // broad_sword owns 10 of 15 rolls ≈ 66.7%.
    expect(rep.bands[0].percent).toBeCloseTo(66.7, 1)
  })

  it('tiling in any authored order still validates', () => {
    const rep = analyzeCoverage(100, [r(51, 100, 'b'), r(1, 50, 'a')])
    expect(rep.complete).toBe(true)
  })
})
