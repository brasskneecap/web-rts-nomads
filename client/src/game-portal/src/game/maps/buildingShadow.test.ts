import { describe, it, expect } from 'vitest'
import { resolveBuildingShadow } from './buildingDefs'

const CELL = 40

describe('resolveBuildingShadow', () => {
  it('derives defaults from the footprint when no config is given', () => {
    // 4x3 building, cell 40px.
    const s = resolveBuildingShadow(undefined, 4, 3, CELL)
    expect(s).not.toBeNull()
    // radiusX = width(4) * 0.42 cells * cell; radiusY = radiusX * 0.3
    expect(s!.radiusX).toBeCloseTo(4 * 0.42 * CELL)
    expect(s!.radiusY).toBeCloseTo(4 * 0.42 * 0.3 * CELL)
    // centered horizontally, near the bottom (0.85 down)
    expect(s!.centerX).toBeCloseTo((4 / 2) * CELL)
    expect(s!.centerY).toBeCloseTo(3 * 0.85 * CELL)
    expect(s!.opacity).toBeCloseTo(0.45)
  })

  it('returns null when explicitly disabled', () => {
    expect(resolveBuildingShadow({ enabled: false }, 4, 3, CELL)).toBeNull()
  })

  it('honors explicit cell-unit overrides (converted to px)', () => {
    const s = resolveBuildingShadow(
      { radiusX: 3, radiusY: 1, offsetX: 2, offsetY: 2.5, opacity: 0.6 },
      4,
      3,
      CELL,
    )
    expect(s!.radiusX).toBeCloseTo(3 * CELL)
    expect(s!.radiusY).toBeCloseTo(1 * CELL)
    expect(s!.centerX).toBeCloseTo(2 * CELL)
    expect(s!.centerY).toBeCloseTo(2.5 * CELL)
    expect(s!.opacity).toBeCloseTo(0.6)
  })

  it('treats zero distance fields as "use default" (codebase convention)', () => {
    const zeroed = resolveBuildingShadow({ radiusX: 0, offsetX: 0 }, 4, 3, CELL)
    const defaults = resolveBuildingShadow(undefined, 4, 3, CELL)
    expect(zeroed).toEqual(defaults)
  })

  it('clamps opacity into [0, 1]', () => {
    expect(resolveBuildingShadow({ opacity: 5 }, 2, 2, CELL)!.opacity).toBe(1)
  })
})
