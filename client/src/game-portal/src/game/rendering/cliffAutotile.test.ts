import { describe, expect, it } from 'vitest'
import {
  CLIFF_FLAT,
  CLIFF_INNER_NE,
  CLIFF_INNER_NW,
  CLIFF_INNER_SE,
  CLIFF_INNER_SW,
  CLIFF_OUTER_NE,
  CLIFF_OUTER_NW,
  CLIFF_OUTER_SE,
  CLIFF_OUTER_SW,
  CLIFF_WALL_E,
  CLIFF_WALL_N,
  CLIFF_WALL_S,
  CLIFF_WALL_W,
  cliffCellBlocks,
  cliffTileAt,
  raisedPredicate,
  rampPredicate,
} from './cliffAutotile'

const CLIFF_TILESET = 'grass-cliff'

// Builds a `raised` predicate from a set of "x,y" strings.
function raisedFromCells(cells: Array<[number, number]>): (x: number, y: number) => boolean {
  const set = new Set(cells.map(([x, y]) => `${x},${y}`))
  return (x: number, y: number) => set.has(`${x},${y}`)
}

describe('cliffAutotile', () => {
  describe('solid raised rectangle', () => {
    // 5x5 rectangle from (0,0) to (4,4).
    const cells: Array<[number, number]> = []
    for (let y = 0; y <= 4; y++) {
      for (let x = 0; x <= 4; x++) cells.push([x, y])
    }
    const raised = raisedFromCells(cells)

    it('picks outer corner slots at the four corners and blocks them', () => {
      expect(cliffTileAt(raised, CLIFF_TILESET, 0, 0)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_OUTER_NW,
      })
      expect(cliffCellBlocks(raised, 0, 0)).toBe(true)

      expect(cliffTileAt(raised, CLIFF_TILESET, 4, 0)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_OUTER_NE,
      })
      expect(cliffCellBlocks(raised, 4, 0)).toBe(true)

      expect(cliffTileAt(raised, CLIFF_TILESET, 0, 4)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_OUTER_SW,
      })
      expect(cliffCellBlocks(raised, 0, 4)).toBe(true)

      expect(cliffTileAt(raised, CLIFF_TILESET, 4, 4)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_OUTER_SE,
      })
      expect(cliffCellBlocks(raised, 4, 4)).toBe(true)
    })

    it('picks wall slots along the edges and blocks them', () => {
      // North edge, not a corner: (2,0)
      expect(cliffTileAt(raised, CLIFF_TILESET, 2, 0)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_WALL_N,
      })
      expect(cliffCellBlocks(raised, 2, 0)).toBe(true)

      // South edge: (2,4)
      expect(cliffTileAt(raised, CLIFF_TILESET, 2, 4)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_WALL_S,
      })
      expect(cliffCellBlocks(raised, 2, 4)).toBe(true)

      // West edge: (0,2)
      expect(cliffTileAt(raised, CLIFF_TILESET, 0, 2)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_WALL_W,
      })
      expect(cliffCellBlocks(raised, 0, 2)).toBe(true)

      // East edge: (4,2)
      expect(cliffTileAt(raised, CLIFF_TILESET, 4, 2)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_WALL_E,
      })
      expect(cliffCellBlocks(raised, 4, 2)).toBe(true)
    })

    it('picks FLAT for the fully-interior cell and does not block', () => {
      expect(cliffTileAt(raised, CLIFF_TILESET, 2, 2)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_FLAT,
      })
      expect(cliffCellBlocks(raised, 2, 2)).toBe(false)
    })
  })

  describe('L-shape with one concave corner', () => {
    // L-shape: full 3x3 block minus the (2,2) cell (bottom-right removed),
    // making (1,1) concave — its SE neighbor (2,2) is missing while N/S/W/E
    // and the other three diagonals are all present.
    const cells: Array<[number, number]> = [
      [0, 0], [1, 0], [2, 0],
      [0, 1], [1, 1], [2, 1],
      [0, 2], [1, 2],
    ]
    const raised = raisedFromCells(cells)

    it('picks the SEi inner corner at the concave cell and does not block', () => {
      expect(cliffTileAt(raised, CLIFF_TILESET, 1, 1)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_INNER_SE,
      })
      expect(cliffCellBlocks(raised, 1, 1)).toBe(false)
    })
  })

  describe('inner corner slot picks (isolated diagonal gaps)', () => {
    // A 3x3 solid block with exactly one diagonal neighbor of the center cell
    // missing exercises each inner-corner branch independently.
    function solid3x3ExceptDiagonal(missing: [number, number]) {
      const cells: Array<[number, number]> = []
      for (let y = 0; y <= 2; y++) {
        for (let x = 0; x <= 2; x++) {
          if (x === missing[0] && y === missing[1]) continue
          cells.push([x, y])
        }
      }
      return raisedFromCells(cells)
    }

    it('missing NE diagonal picks NEi at center', () => {
      const raised = solid3x3ExceptDiagonal([2, 0])
      expect(cliffTileAt(raised, CLIFF_TILESET, 1, 1)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_INNER_NE,
      })
      expect(cliffCellBlocks(raised, 1, 1)).toBe(false)
    })

    it('missing NW diagonal picks NWi at center', () => {
      const raised = solid3x3ExceptDiagonal([0, 0])
      expect(cliffTileAt(raised, CLIFF_TILESET, 1, 1)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_INNER_NW,
      })
      expect(cliffCellBlocks(raised, 1, 1)).toBe(false)
    })

    it('missing SW diagonal picks SWi at center', () => {
      const raised = solid3x3ExceptDiagonal([0, 2])
      expect(cliffTileAt(raised, CLIFF_TILESET, 1, 1)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_INNER_SW,
      })
      expect(cliffCellBlocks(raised, 1, 1)).toBe(false)
    })
  })

  describe('ramp cell', () => {
    // Same 5x5 raised rectangle as above; mark the north-wall cell (2,0) as
    // a ramp.
    const cells: Array<[number, number]> = []
    for (let y = 0; y <= 4; y++) {
      for (let x = 0; x <= 4; x++) cells.push([x, y])
    }
    const raised = raisedFromCells(cells)
    const isRamp = (x: number, y: number) => x === 2 && y === 0

    it('renders the ramp cell as FLAT instead of its wall slot', () => {
      expect(cliffTileAt(raised, CLIFF_TILESET, 2, 0, isRamp)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_FLAT,
      })
    })

    it('does not block the ramp cell', () => {
      expect(cliffCellBlocks(raised, 2, 0, isRamp)).toBe(false)
    })

    it('leaves other wall cells unchanged', () => {
      expect(cliffTileAt(raised, CLIFF_TILESET, 2, 4, isRamp)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_WALL_S,
      })
      expect(cliffCellBlocks(raised, 2, 4, isRamp)).toBe(true)

      expect(cliffTileAt(raised, CLIFF_TILESET, 0, 2, isRamp)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_WALL_W,
      })
      expect(cliffCellBlocks(raised, 0, 2, isRamp)).toBe(true)

      expect(cliffTileAt(raised, CLIFF_TILESET, 0, 0, isRamp)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_OUTER_NW,
      })
      expect(cliffCellBlocks(raised, 0, 0, isRamp)).toBe(true)
    })

    it('is a no-op when isRamp is omitted (default false)', () => {
      expect(cliffTileAt(raised, CLIFF_TILESET, 2, 0)).toEqual({
        tileset: CLIFF_TILESET,
        ...CLIFF_WALL_N,
      })
      expect(cliffCellBlocks(raised, 2, 0)).toBe(true)
    })
  })

  describe('rampPredicate', () => {
    it('builds a lookup matching the ramps list', () => {
      const isRamp = rampPredicate([{ x: 1, y: 2 }, { x: 3, y: 4 }])
      expect(isRamp(1, 2)).toBe(true)
      expect(isRamp(3, 4)).toBe(true)
      expect(isRamp(0, 0)).toBe(false)
    })

    it('returns an always-false predicate for undefined ramps', () => {
      const isRamp = rampPredicate(undefined)
      expect(isRamp(0, 0)).toBe(false)
    })
  })

  describe('non-raised cell', () => {
    const raised = raisedFromCells([[0, 0]])

    it('cliffTileAt returns null', () => {
      expect(cliffTileAt(raised, CLIFF_TILESET, 5, 5)).toBeNull()
    })

    it('cliffCellBlocks returns false', () => {
      expect(cliffCellBlocks(raised, 5, 5)).toBe(false)
    })
  })

  describe('raisedPredicate', () => {
    it('builds a lookup matching the elevation list', () => {
      const raised = raisedPredicate([{ x: 1, y: 2 }, { x: 3, y: 4 }])
      expect(raised(1, 2)).toBe(true)
      expect(raised(3, 4)).toBe(true)
      expect(raised(0, 0)).toBe(false)
    })

    it('returns an always-false predicate for undefined elevation', () => {
      const raised = raisedPredicate(undefined)
      expect(raised(0, 0)).toBe(false)
    })
  })
})
