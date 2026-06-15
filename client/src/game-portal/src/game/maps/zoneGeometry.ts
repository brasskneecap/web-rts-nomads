/**
 * zoneGeometry.ts
 *
 * Stateless helpers for zone geometry used by both the map editor and the
 * in-match renderer. Perimeter/interior are always DERIVED from the cell set —
 * never stored — matching the server-side invariant.
 */
import type { Zone } from '../network/protocol'

/** Stable string key for a grid cell. Mirrors Go's gridPoint{X,Y} in lookup maps. */
export function cellKey(x: number, y: number): string {
  return `${x}:${y}`
}

/**
 * Build a cellKey → zoneId index from a list of zones.
 * Last zone wins on conflict (mirrors server single-owner invariant; the editor
 * enforces non-overlap at authoring time, but this handles any residual races).
 */
export function buildZoneCellIndex(zones: Zone[]): Map<string, string> {
  const index = new Map<string, string>()
  for (const zone of zones) {
    for (const [x, y] of zone.cells) {
      index.set(cellKey(x, y), zone.id)
    }
  }
  return index
}

/**
 * Returns true when the cell at (x, y) is a member of the zone AND has at
 * least one 4-neighbour that is NOT a member of the same zone.
 * `members` should be a Set<string> of cellKey values for the zone's cells.
 */
export function isPerimeterCell(
  _zone: Zone,
  x: number,
  y: number,
  members: Set<string>,
): boolean {
  if (!members.has(cellKey(x, y))) return false
  const neighbours = [
    cellKey(x - 1, y),
    cellKey(x + 1, y),
    cellKey(x, y - 1),
    cellKey(x, y + 1),
  ]
  return neighbours.some((k) => !members.has(k))
}

/**
 * Returns the cell list with any fully-enclosed empty cells filled in.
 *
 * An empty cell is "enclosed" when it cannot reach outside the zone's bounding
 * box through other empty cells (4-connectivity) — i.e. the drawn member cells
 * form a closed loop around it. Used to auto-fill the interior once the author
 * has drawn a perimeter around a region: a flood-fill seeded from the border of
 * the (expanded) bounding box marks every empty cell that escapes to the
 * outside; whatever empty cell it can't reach is trapped inside the loop and is
 * added as a member. Internal divider lines then reclassify from perimeter to
 * interior automatically (they end up surrounded by members).
 *
 * Purely additive — it never removes cells. Returns the original list unchanged
 * when nothing is enclosed (e.g. the loop isn't closed yet).
 */
export function fillEnclosedZoneCells(cells: [number, number][]): [number, number][] {
  if (cells.length === 0) return cells

  const members = new Set<string>(cells.map(([x, y]) => cellKey(x, y)))

  // Bounding box expanded by one so there is a guaranteed "outside" ring to
  // seed the flood-fill from.
  let minX = Infinity
  let minY = Infinity
  let maxX = -Infinity
  let maxY = -Infinity
  for (const [x, y] of cells) {
    if (x < minX) minX = x
    if (x > maxX) maxX = x
    if (y < minY) minY = y
    if (y > maxY) maxY = y
  }
  minX -= 1
  minY -= 1
  maxX += 1
  maxY += 1

  // Flood-fill the empty cells reachable from the box border = "outside".
  const outside = new Set<string>()
  const stack: Array<[number, number]> = []
  for (let x = minX; x <= maxX; x++) {
    stack.push([x, minY], [x, maxY])
  }
  for (let y = minY; y <= maxY; y++) {
    stack.push([minX, y], [maxX, y])
  }
  while (stack.length) {
    const [x, y] = stack.pop() as [number, number]
    if (x < minX || x > maxX || y < minY || y > maxY) continue
    const k = cellKey(x, y)
    if (outside.has(k) || members.has(k)) continue
    outside.add(k)
    stack.push([x + 1, y], [x - 1, y], [x, y + 1], [x, y - 1])
  }

  // Any in-box empty cell the flood never reached is enclosed → fill it.
  const filled: [number, number][] = [...cells]
  for (let y = minY + 1; y <= maxY - 1; y++) {
    for (let x = minX + 1; x <= maxX - 1; x++) {
      const k = cellKey(x, y)
      if (!members.has(k) && !outside.has(k)) {
        filled.push([x, y])
        members.add(k)
      }
    }
  }
  return filled
}

/**
 * A single boundary edge of a zone, expressed in GRID units (multiply by
 * cellSize to get pixels). It is the shared side between a member cell and a
 * non-member neighbour. `nx`/`ny` is the unit normal pointing INWARD (toward
 * the member cell's centre, canvas y-down); offset the segment along it to draw
 * the outline just inside the zone so two adjacent zones that share an edge each
 * render their own line instead of overpainting one another.
 *
 * `nbx`/`nby` is the grid cell on the OUTSIDE of this edge (the non-member
 * neighbour). Callers can look that cell up in a cross-zone ownership index to
 * decide whether to drop the edge — e.g. hide the shared border between two
 * zones owned by the same player so they read as one continuous region.
 */
export interface ZoneEdge {
  x1: number
  y1: number
  x2: number
  y2: number
  nx: number
  ny: number
  nbx: number
  nby: number
}

/**
 * Returns the OUTLINE of a zone as a list of edges: for every member cell, each
 * of its 4 sides whose neighbour is not a member of the same zone. This traces
 * the outer boundary (and any interior holes) as line segments rather than
 * filling whole perimeter cells, so the zone reads as a thin outline instead of
 * a band of highlighted squares. Derived from the cell set each call — never
 * cached, matching the perimeter-not-stored invariant.
 */
export function zoneBoundaryEdges(zone: Zone): ZoneEdge[] {
  const members = new Set<string>(zone.cells.map(([x, y]) => cellKey(x, y)))
  const edges: ZoneEdge[] = []
  for (const [x, y] of zone.cells) {
    // top — inward normal points down (+y), neighbour above
    if (!members.has(cellKey(x, y - 1))) edges.push({ x1: x, y1: y, x2: x + 1, y2: y, nx: 0, ny: 1, nbx: x, nby: y - 1 })
    // bottom — inward normal points up (-y), neighbour below
    if (!members.has(cellKey(x, y + 1))) edges.push({ x1: x, y1: y + 1, x2: x + 1, y2: y + 1, nx: 0, ny: -1, nbx: x, nby: y + 1 })
    // left — inward normal points right (+x), neighbour to the left
    if (!members.has(cellKey(x - 1, y))) edges.push({ x1: x, y1: y, x2: x, y2: y + 1, nx: 1, ny: 0, nbx: x - 1, nby: y })
    // right — inward normal points left (-x), neighbour to the right
    if (!members.has(cellKey(x + 1, y))) edges.push({ x1: x + 1, y1: y, x2: x + 1, y2: y + 1, nx: -1, ny: 0, nbx: x + 1, nby: y })
  }
  return edges
}

/**
 * Classifies all cells in a zone into perimeter and interior arrays.
 * A cell is a perimeter cell iff at least one of its 4-neighbours is not a
 * member of the same zone (including out-of-bounds neighbours, which are
 * never members). Derived from the cell set each call — never cached.
 */
export function classifyZoneCells(zone: Zone): {
  perimeter: [number, number][]
  interior: [number, number][]
} {
  const members = new Set<string>(zone.cells.map(([x, y]) => cellKey(x, y)))
  const perimeter: [number, number][] = []
  const interior: [number, number][] = []

  for (const [x, y] of zone.cells) {
    if (isPerimeterCell(zone, x, y, members)) {
      perimeter.push([x, y])
    } else {
      interior.push([x, y])
    }
  }

  return { perimeter, interior }
}
