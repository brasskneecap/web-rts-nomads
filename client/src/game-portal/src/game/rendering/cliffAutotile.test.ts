import { describe, it, expect } from 'vitest'
import {
  cliffTileAt,
  cliffCellBlocks,
  cliffWangMask,
  raisedPredicate,
  rampPredicate,
} from './cliffAutotile'

function rectElevation(x0: number, x1: number, y0: number, y1: number) {
  const cells: { x: number; y: number }[] = []
  for (let y = y0; y <= y1; y++) {
    for (let x = x0; x <= x1; x++) cells.push({ x, y })
  }
  return cells
}

const SHEET = 'grass-grass-elevation-25'
const FLAT = { tileset: SHEET, col: 2, row: 1 } // Wang mask 15

describe('cliff Wang auto-tiler', () => {
  // A 4x4 raised plateau spanning cells x∈[2,5], y∈[2,5].
  const raised = raisedPredicate(rectElevation(2, 5, 2, 5))

  it('interior cell → full mask 15 → flat tile, walkable', () => {
    expect(cliffWangMask(raised, 3, 3)).toBe(15)
    expect(cliffTileAt(raised, SHEET, 3, 3)).toEqual(FLAT)
    expect(cliffCellBlocks(raised, 3, 3)).toBe(false)
  })

  it('cell fully outside the plateau → mask 0 → null, walkable', () => {
    expect(cliffWangMask(raised, 0, 0)).toBe(0)
    expect(cliffTileAt(raised, SHEET, 0, 0)).toBeNull()
    expect(cliffCellBlocks(raised, 0, 0)).toBe(false)
  })

  it('transition cell → blocks and picks a non-interior tile', () => {
    const mask = cliffWangMask(raised, 3, 1) // just above the plateau's top edge
    expect(mask).not.toBe(0)
    expect(mask).not.toBe(15)
    expect(cliffCellBlocks(raised, 3, 1)).toBe(true)
    const t = cliffTileAt(raised, SHEET, 3, 1)
    expect(t).not.toBeNull()
    expect(t).not.toEqual(FLAT)
  })

  it('a ramp on a transition cell renders the flat tile and is walkable', () => {
    const isRamp = rampPredicate([{ x: 3, y: 1 }])
    expect(cliffTileAt(raised, SHEET, 3, 1, isRamp)).toEqual(FLAT)
    expect(cliffCellBlocks(raised, 3, 1, isRamp)).toBe(false)
  })

  it('predicates look up cells (and tolerate undefined)', () => {
    expect(raised(2, 2)).toBe(true)
    expect(raised(6, 6)).toBe(false)
    const r = rampPredicate([{ x: 1, y: 1 }])
    expect(r(1, 1)).toBe(true)
    expect(r(2, 2)).toBe(false)
    expect(rampPredicate(undefined)(0, 0)).toBe(false)
    expect(raisedPredicate(undefined)(0, 0)).toBe(false)
  })
})
