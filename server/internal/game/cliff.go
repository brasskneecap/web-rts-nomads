package game

import "webrts/server/pkg/protocol"

// Cliff auto-tiling. The *-elevation-25 sheets are Wang cliff atlases sharing
// the base grass/dirt sheet's 16-tile corner-based (marching-squares) layout,
// so a cliff is a Wang overlay over the map's Elevation grid: each cell's tile
// is chosen by which of its 4 corners lie inside the raised plateau. Mirrors
// the client's cliffAutotile.ts — keep cliffWangLayout + the mask rule identical.
//
// mask bits are corners: bit0=TL, bit1=TR, bit2=BL, bit3=BR. The -25 cliff art
// reads vertically inverted from the base sheet (tall rock face is south-
// facing), so this is WANG_GRASS_DIRT_COORDS with the vertical corners swapped
// — matches the client's WANG_LAYOUT. Index = mask.
var cliffWangLayout = [16][2]int{
	{0, 3}, {0, 0}, {1, 3}, {3, 0}, {3, 3}, {3, 2}, {0, 1}, {2, 0},
	{0, 2}, {2, 3}, {1, 0}, {1, 1}, {1, 2}, {3, 1}, {2, 2}, {2, 1},
}

// cliffWangMask returns the Wang mask for cell (x, y): a corner is "inside"
// when ANY of the 4 cells touching it is raised — the raised region expands
// half a cell into its border, matching the client and computeWangMask.
func cliffWangMask(raised func(x, y int) bool, x, y int) int {
	any := func(cells [4][2]int) bool {
		for _, c := range cells {
			if raised(c[0], c[1]) {
				return true
			}
		}
		return false
	}
	mask := 0
	if any([4][2]int{{x - 1, y - 1}, {x, y - 1}, {x - 1, y}, {x, y}}) {
		mask |= 1 // TL
	}
	if any([4][2]int{{x, y - 1}, {x + 1, y - 1}, {x, y}, {x + 1, y}}) {
		mask |= 2 // TR
	}
	if any([4][2]int{{x - 1, y}, {x, y}, {x - 1, y + 1}, {x, y + 1}}) {
		mask |= 4 // BL
	}
	if any([4][2]int{{x, y}, {x + 1, y}, {x, y + 1}, {x + 1, y + 1}}) {
		mask |= 8 // BR
	}
	return mask
}

// cliffTileAt returns the cliff atlas (col, row) for cell (x, y). ok=false when
// the cell is entirely outside the plateau (mask 0 → ground shows through). A
// ramp renders the flat interior tile (mask 15). Only used by tests here;
// walkability goes through cliffCellBlocks.
func cliffTileAt(raised func(x, y int) bool, isRamp func(x, y int) bool, x, y int) (col, row int, ok bool) {
	mask := cliffWangMask(raised, x, y)
	if mask == 0 {
		return 0, 0, false
	}
	slot := cliffWangLayout[mask]
	if isRamp(x, y) {
		slot = cliffWangLayout[15]
	}
	return slot[0], slot[1], true
}

// cliffCellBlocks reports whether (x, y) renders a cliff TRANSITION (a face /
// edge / corner) and so blocks movement: mask is neither 0 (ground) nor 15
// (flat plateau top). Ramps are always walkable.
func cliffCellBlocks(raised func(x, y int) bool, isRamp func(x, y int) bool, x, y int) bool {
	if isRamp(x, y) {
		return false
	}
	mask := cliffWangMask(raised, x, y)
	return mask != 0 && mask != 15
}

// raisedSetFromElevation builds a lookup set from a map's authored Elevation
// cells, for use as the backing store of a `raised` predicate closure.
func raisedSetFromElevation(cells []protocol.GridCoord) map[[2]int]bool {
	set := make(map[[2]int]bool, len(cells))
	for _, c := range cells {
		set[[2]int{c.X, c.Y}] = true
	}
	return set
}

// raisedPredicate returns a `raised(x, y) bool` closure over a set built with
// raisedSetFromElevation.
func raisedPredicate(set map[[2]int]bool) func(x, y int) bool {
	return func(x, y int) bool {
		return set[[2]int{x, y}]
	}
}

// rampSetFromRamps builds a lookup set from a map's authored Ramps cells.
func rampSetFromRamps(cells []protocol.GridCoord) map[[2]int]bool {
	set := make(map[[2]int]bool, len(cells))
	for _, c := range cells {
		set[[2]int{c.X, c.Y}] = true
	}
	return set
}

// rampPredicate returns an `isRamp(x, y) bool` closure over a set built with
// rampSetFromRamps.
func rampPredicate(set map[[2]int]bool) func(x, y int) bool {
	return func(x, y int) bool {
		return set[[2]int{x, y}]
	}
}
