import { describe, expect, it } from 'vitest'
import {
  cellKey,
  classifyZoneCells,
  fillEnclosedZoneCells,
  zoneBoundaryEdges,
} from './zoneGeometry'

/** Build the perimeter ring (outline only) of the rectangle [x0..x1]×[y0..y1]. */
function ringCells(x0: number, y0: number, x1: number, y1: number): [number, number][] {
  const out: [number, number][] = []
  for (let x = x0; x <= x1; x++) {
    out.push([x, y0], [x, y1])
  }
  for (let y = y0 + 1; y < y1; y++) {
    out.push([x0, y], [x1, y])
  }
  return out
}

function has(cells: [number, number][], x: number, y: number): boolean {
  const k = cellKey(x, y)
  return cells.some(([cx, cy]) => cellKey(cx, cy) === k)
}

describe('fillEnclosedZoneCells', () => {
  it('leaves an open (unclosed) outline untouched', () => {
    // A C-shape: a ring missing one edge cell can escape to the outside.
    const ring = ringCells(0, 0, 4, 4).filter(([x, y]) => !(x === 2 && y === 0))
    const filled = fillEnclosedZoneCells(ring)
    expect(filled.length).toBe(ring.length) // nothing enclosed
  })

  it('fills the hollow interior of a closed rectangle', () => {
    const ring = ringCells(0, 0, 4, 4) // 5x5 outline, 16 cells, 9-cell hole
    const filled = fillEnclosedZoneCells(ring)
    // Every interior cell (1..3 × 1..3) is now a member.
    for (let y = 1; y <= 3; y++) {
      for (let x = 1; x <= 3; x++) {
        expect(has(filled, x, y)).toBe(true)
      }
    }
    expect(filled.length).toBe(25) // full 5x5 block
  })

  it('dissolves an internal divider line once an outer loop encloses it', () => {
    // The reported case: a tall outline (0..4 × 0..6) with a horizontal divider
    // line across the middle (y = 3). Both sub-regions are enclosed, so the
    // whole rectangle fills and the divider becomes interior.
    const ring = ringCells(0, 0, 4, 6)
    const divider: [number, number][] = [
      [1, 3], [2, 3], [3, 3], // interior span of the divider (ends already on the ring)
    ]
    const cells = [...ring, ...divider]
    const filled = fillEnclosedZoneCells(cells)
    expect(filled.length).toBe(5 * 7) // solid 5×7 block

    // The divider cells, now surrounded by members, classify as INTERIOR
    // (they were perimeter before the fill) — i.e. the dark line "goes away".
    const dividerZone = { id: 'z', anchor: { x: 2, y: 3 }, cells: filled, capture: { type: 'clear' } }
    const { interior } = classifyZoneCells(dividerZone as never)
    for (const [x, y] of divider) {
      expect(interior.some(([ix, iy]) => ix === x && iy === y)).toBe(true)
    }
  })

  it('is a no-op for an already-solid block (returns same count)', () => {
    const block: [number, number][] = []
    for (let y = 0; y < 3; y++) for (let x = 0; x < 3; x++) block.push([x, y])
    expect(fillEnclosedZoneCells(block).length).toBe(block.length)
  })

  it('returns empty input unchanged', () => {
    expect(fillEnclosedZoneCells([])).toEqual([])
  })
})

describe('zoneBoundaryEdges', () => {
  const edgeKey = (e: { x1: number; y1: number; x2: number; y2: number }) =>
    `${e.x1},${e.y1}-${e.x2},${e.y2}`

  it('outlines a single cell with exactly its 4 sides', () => {
    const zone = { id: 'z', anchor: { x: 0, y: 0 }, cells: [[0, 0]], capture: { type: 'clear' } }
    const edges = zoneBoundaryEdges(zone as never)
    expect(edges).toHaveLength(4)
    const keys = new Set(edges.map(edgeKey))
    expect(keys).toEqual(
      new Set(['0,0-1,0', '0,1-1,1', '0,0-0,1', '1,0-1,1']), // top, bottom, left, right
    )
    // Inward normals point toward the cell centre (canvas y-down): top→down,
    // bottom→up, left→right, right→left.
    const byKey = new Map(edges.map((e) => [edgeKey(e), { nx: e.nx, ny: e.ny }]))
    expect(byKey.get('0,0-1,0')).toEqual({ nx: 0, ny: 1 }) // top
    expect(byKey.get('0,1-1,1')).toEqual({ nx: 0, ny: -1 }) // bottom
    expect(byKey.get('0,0-0,1')).toEqual({ nx: 1, ny: 0 }) // left
    expect(byKey.get('1,0-1,1')).toEqual({ nx: -1, ny: 0 }) // right
  })

  it('emits only the outer ring of a solid block (no internal edges)', () => {
    // Solid 3x3 block. Boundary = 12 unit edges (3 per side), zero interior edges.
    const block: [number, number][] = []
    for (let y = 0; y < 3; y++) for (let x = 0; x < 3; x++) block.push([x, y])
    const zone = { id: 'z', anchor: { x: 1, y: 1 }, cells: block, capture: { type: 'clear' } }
    const edges = zoneBoundaryEdges(zone as never)
    expect(edges).toHaveLength(12)
    // The shared side between (0,0) and (1,0) must NOT appear.
    expect(edges.map(edgeKey)).not.toContain('1,0-1,1')
  })

  it('traces interior holes as well as the outer boundary', () => {
    // 5x5 outline with a hollow center: outer ring (20 edges) + inner hole (12 edges).
    const ring: [number, number][] = []
    for (let x = 0; x <= 4; x++) ring.push([x, 0], [x, 4])
    for (let y = 1; y < 4; y++) ring.push([0, y], [4, y])
    const zone = { id: 'z', anchor: { x: 2, y: 2 }, cells: ring, capture: { type: 'clear' } }
    const edges = zoneBoundaryEdges(zone as never)
    // Outer perimeter of the 5x5 box = 20 unit edges; inner 3x3 hole boundary = 12.
    expect(edges).toHaveLength(20 + 12)
  })
})
