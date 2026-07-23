import type { GridCoord, TileCoord } from '@/game/network/protocol'

// Cliff auto-tiling. The *-elevation-25 sheets are proper Wang cliff atlases:
// their 16 tiles use the SAME corner-based (marching-squares) layout as the
// base grass/dirt sheet (see WANG_GRASS_DIRT_COORDS in terrainTileset.ts). So a
// cliff is just a Wang overlay over an "elevation" grid — each cell's tile is
// chosen by which of its 4 corners lie inside the raised plateau, which makes
// faces, lips and corners connect seamlessly and recompute across neighbours.
//
// Mask bits are corners: bit0=TL, bit1=TR, bit2=BL, bit3=BR. The -25 cliff
// sheets share the base grass/dirt sheet's Wang arrangement, so this table is
// identical to WANG_GRASS_DIRT_COORDS (col/row per mask). The tall rock face
// lands on the plateau's north/top edge, a subtle lip on the south.
const WANG_LAYOUT: ReadonlyArray<{ col: number; row: number }> = [
  { col: 0, row: 3 }, // 0  ----
  { col: 3, row: 3 }, // 1  T---
  { col: 0, row: 2 }, // 2  -T--
  { col: 1, row: 2 }, // 3  TT--
  { col: 0, row: 0 }, // 4  --B-
  { col: 3, row: 2 }, // 5  T-B-
  { col: 2, row: 3 }, // 6  -TB-
  { col: 3, row: 1 }, // 7  TTB-
  { col: 1, row: 3 }, // 8  ---B
  { col: 0, row: 1 }, // 9  T--B
  { col: 1, row: 0 }, // 10 -T-B
  { col: 2, row: 2 }, // 11 TT-B
  { col: 3, row: 0 }, // 12 --BB
  { col: 2, row: 0 }, // 13 T-BB
  { col: 1, row: 1 }, // 14 -TBB
  { col: 2, row: 1 }, // 15 TTBB (full interior)
]

const NO_RAMPS = () => false

// The Wang mask for cell (x,y): a corner is "inside" when ANY of the 4 cells
// touching it is raised — the raised region expands half a cell into its
// border, exactly matching the grass/dirt overlay auto-tiler (computeWangMask).
export function cliffWangMask(
  raised: (x: number, y: number) => boolean,
  x: number,
  y: number,
): number {
  const any = (cells: ReadonlyArray<readonly [number, number]>): boolean =>
    cells.some(([cx, cy]) => raised(cx, cy))
  let mask = 0
  if (any([[x - 1, y - 1], [x, y - 1], [x - 1, y], [x, y]])) mask |= 1 // TL
  if (any([[x, y - 1], [x + 1, y - 1], [x, y], [x + 1, y]])) mask |= 2 // TR
  if (any([[x - 1, y], [x, y], [x - 1, y + 1], [x, y + 1]])) mask |= 4 // BL
  if (any([[x, y], [x + 1, y], [x, y + 1], [x + 1, y + 1]])) mask |= 8 // BR
  return mask
}

// The cliff tile at (x,y), or null when the cell is entirely outside the
// plateau (mask 0 → ground shows through). A ramp renders the full-interior
// tile (mask 15) — a flat, walkable opening in the wall.
export function cliffTileAt(
  raised: (x: number, y: number) => boolean,
  cliffTileset: string,
  x: number,
  y: number,
  isRamp: (x: number, y: number) => boolean = NO_RAMPS,
): TileCoord | null {
  const mask = cliffWangMask(raised, x, y)
  if (mask === 0) return null
  const slot = isRamp(x, y) ? WANG_LAYOUT[15] : WANG_LAYOUT[mask]
  return { tileset: cliffTileset, col: slot.col, row: slot.row }
}

// A cell blocks movement when it renders a cliff TRANSITION (a face/edge/
// corner): mask is neither 0 (ground) nor 15 (flat plateau top). Ramps are
// always walkable.
export function cliffCellBlocks(
  raised: (x: number, y: number) => boolean,
  x: number,
  y: number,
  isRamp: (x: number, y: number) => boolean = NO_RAMPS,
): boolean {
  if (isRamp(x, y)) return false
  const mask = cliffWangMask(raised, x, y)
  return mask !== 0 && mask !== 15
}

export function raisedPredicate(
  elevation: ReadonlyArray<GridCoord> | undefined,
): (x: number, y: number) => boolean {
  const set = new Set<string>()
  for (const c of elevation ?? []) set.add(`${c.x},${c.y}`)
  return (x, y) => set.has(`${x},${y}`)
}

export function rampPredicate(
  ramps: ReadonlyArray<GridCoord> | undefined,
): (x: number, y: number) => boolean {
  const set = new Set<string>()
  for (const c of ramps ?? []) set.add(`${c.x},${c.y}`)
  return (x, y) => set.has(`${x},${y}`)
}
