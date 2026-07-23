import type { GridCoord, TileCoord } from '../network/protocol'

// Cliff auto-tiling (ELEVATION plateaus). A raised cell picks a slot out of a
// 4x4 cliff atlas (e.g. `grass-cliff`) based on which of its 8 neighbors are
// also raised — walls/outer-corners on the plateau boundary block movement,
// the flat top and inner corners are walkable. The server implements the
// identical pick/block logic; keep the two in exact sync.
//
// Y increases DOWNWARD (screen coords): N = y-1, S = y+1.

export type CliffSlot = { col: number; row: number }

export const CLIFF_FLAT: CliffSlot = { col: 1, row: 1 }

export const CLIFF_WALL_N: CliffSlot = { col: 1, row: 0 }
export const CLIFF_WALL_S: CliffSlot = { col: 1, row: 2 }
export const CLIFF_WALL_W: CliffSlot = { col: 0, row: 1 }
export const CLIFF_WALL_E: CliffSlot = { col: 2, row: 1 }

export const CLIFF_OUTER_NW: CliffSlot = { col: 0, row: 0 }
export const CLIFF_OUTER_NE: CliffSlot = { col: 2, row: 0 }
export const CLIFF_OUTER_SW: CliffSlot = { col: 0, row: 2 }
export const CLIFF_OUTER_SE: CliffSlot = { col: 2, row: 2 }

export const CLIFF_INNER_NE: CliffSlot = { col: 3, row: 1 }
export const CLIFF_INNER_NW: CliffSlot = { col: 3, row: 2 }
export const CLIFF_INNER_SW: CliffSlot = { col: 2, row: 3 }
export const CLIFF_INNER_SE: CliffSlot = { col: 3, row: 3 }

// Slots whose picked tile blocks movement: walls (boundary faces) and outer
// corners. FLAT and the four inner corners are walkable plateau top.
const BLOCKING_SLOTS: ReadonlySet<CliffSlot> = new Set([
  CLIFF_WALL_N,
  CLIFF_WALL_S,
  CLIFF_WALL_W,
  CLIFF_WALL_E,
  CLIFF_OUTER_NW,
  CLIFF_OUTER_NE,
  CLIFF_OUTER_SW,
  CLIFF_OUTER_SE,
])

// Picks the cliff slot for a raised cell per the canonical pick order (first
// match wins). `raised` must return false for any coordinate outside the
// map's bounds — out-of-bounds is "not raised", same as an unraised cell.
function pickCliffSlot(
  raised: (x: number, y: number) => boolean,
  x: number,
  y: number,
): CliffSlot {
  const n = raised(x, y - 1)
  const s = raised(x, y + 1)
  const w = raised(x - 1, y)
  const e = raised(x + 1, y)
  const ne = raised(x + 1, y - 1)
  const nw = raised(x - 1, y - 1)
  const se = raised(x + 1, y + 1)
  const sw = raised(x - 1, y + 1)

  if (!w && !n) return CLIFF_OUTER_NW
  if (!e && !n) return CLIFF_OUTER_NE
  if (!w && !s) return CLIFF_OUTER_SW
  if (!e && !s) return CLIFF_OUTER_SE
  if (!n) return CLIFF_WALL_N
  if (!s) return CLIFF_WALL_S
  if (!w) return CLIFF_WALL_W
  if (!e) return CLIFF_WALL_E
  if (!ne) return CLIFF_INNER_NE
  if (!nw) return CLIFF_INNER_NW
  if (!se) return CLIFF_INNER_SE
  if (!sw) return CLIFF_INNER_SW
  return CLIFF_FLAT
}

// Returns the cliff atlas tile for (x,y), or null when the cell isn't
// raised (a non-raised cell has no cliff tile).
export function cliffTileAt(
  raised: (x: number, y: number) => boolean,
  cliffTileset: string,
  x: number,
  y: number,
): TileCoord | null {
  if (!raised(x, y)) return null
  const slot = pickCliffSlot(raised, x, y)
  return { tileset: cliffTileset, col: slot.col, row: slot.row }
}

// True iff (x,y) is raised AND its picked slot is a wall or outer corner.
export function cliffCellBlocks(
  raised: (x: number, y: number) => boolean,
  x: number,
  y: number,
): boolean {
  if (!raised(x, y)) return false
  const slot = pickCliffSlot(raised, x, y)
  return BLOCKING_SLOTS.has(slot)
}

// Builds an O(1) raised-cell lookup from a sparse elevation list.
export function raisedPredicate(
  elevation: ReadonlyArray<GridCoord> | undefined,
): (x: number, y: number) => boolean {
  const set = new Set<string>()
  for (const cell of elevation ?? []) {
    set.add(`${cell.x},${cell.y}`)
  }
  return (x: number, y: number) => set.has(`${x},${y}`)
}
