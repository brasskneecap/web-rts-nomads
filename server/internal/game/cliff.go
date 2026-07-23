package game

import "webrts/server/pkg/protocol"

// cliffSlot identifies one (col, row) slot in the 4x4 cliff atlas.
type cliffSlot struct {
	Col int
	Row int
}

// Named slots in the 4x4 cliff atlas. Mirrors the client's cliff auto-tile
// table exactly — if this changes, update the client too.
var (
	cliffFlat = cliffSlot{Col: 1, Row: 1}

	cliffWallN = cliffSlot{Col: 1, Row: 0}
	cliffWallS = cliffSlot{Col: 1, Row: 2}
	cliffWallW = cliffSlot{Col: 0, Row: 1}
	cliffWallE = cliffSlot{Col: 2, Row: 1}

	cliffOuterNW = cliffSlot{Col: 0, Row: 0}
	cliffOuterNE = cliffSlot{Col: 2, Row: 0}
	cliffOuterSW = cliffSlot{Col: 0, Row: 2}
	cliffOuterSE = cliffSlot{Col: 2, Row: 2}

	cliffInnerNE = cliffSlot{Col: 3, Row: 1}
	cliffInnerNW = cliffSlot{Col: 3, Row: 2}
	cliffInnerSW = cliffSlot{Col: 2, Row: 3}
	cliffInnerSE = cliffSlot{Col: 3, Row: 3}
)

// cliffSlotBlocks reports whether a picked cliff slot blocks movement — true
// for wall and outer-corner slots, false for the flat top and inner corners.
func cliffSlotBlocks(slot cliffSlot) bool {
	switch slot {
	case cliffWallN, cliffWallS, cliffWallW, cliffWallE,
		cliffOuterNW, cliffOuterNE, cliffOuterSW, cliffOuterSE:
		return true
	default:
		return false
	}
}

// cliffTileAt derives the cliff atlas slot for cell (x, y) given a `raised`
// predicate and an `isRamp` predicate. Returns ok=false when (x, y) is not
// itself raised — non-raised cells have no cliff tile (a ramp mark on a
// non-raised cell is inert). A raised cell marked as a ramp always picks the
// FLAT slot, overriding the normal wall/corner derivation, so it renders as
// plateau-top and (via cliffCellBlocks) never blocks.
//
// Y increases downward (screen coords): N = y-1, S = y+1. See the package
// doc comment on the CANONICAL CLIFF AUTO-TILE SPEC for the derivation this
// mirrors 1:1; the client mirrors the same rule set.
func cliffTileAt(raised func(x, y int) bool, isRamp func(x, y int) bool, x, y int) (col int, row int, ok bool) {
	if !raised(x, y) {
		return 0, 0, false
	}

	if isRamp(x, y) {
		return cliffFlat.Col, cliffFlat.Row, true
	}

	n := raised(x, y-1)
	s := raised(x, y+1)
	w := raised(x-1, y)
	e := raised(x+1, y)
	ne := raised(x+1, y-1)
	nw := raised(x-1, y-1)
	se := raised(x+1, y+1)
	sw := raised(x-1, y+1)

	var slot cliffSlot
	switch {
	case !w && !n:
		slot = cliffOuterNW
	case !e && !n:
		slot = cliffOuterNE
	case !w && !s:
		slot = cliffOuterSW
	case !e && !s:
		slot = cliffOuterSE
	case !n:
		slot = cliffWallN
	case !s:
		slot = cliffWallS
	case !w:
		slot = cliffWallW
	case !e:
		slot = cliffWallE
	case !ne:
		slot = cliffInnerNE
	case !nw:
		slot = cliffInnerNW
	case !se:
		slot = cliffInnerSE
	case !sw:
		slot = cliffInnerSW
	default:
		slot = cliffFlat
	}

	return slot.Col, slot.Row, true
}

// cliffCellBlocks reports whether (x, y) is raised and its picked cliff slot
// blocks movement (a wall or outer corner). The flat plateau top, the four
// inner corners, and ramp cells are walkable.
func cliffCellBlocks(raised func(x, y int) bool, isRamp func(x, y int) bool, x, y int) bool {
	if !raised(x, y) {
		return false
	}
	if isRamp(x, y) {
		return false
	}
	col, row, ok := cliffTileAt(raised, isRamp, x, y)
	if !ok {
		return false
	}
	return cliffSlotBlocks(cliffSlot{Col: col, Row: row})
}

// raisedSetFromElevation builds a lookup set from a map's authored
// Elevation cells, for use as the backing store of a `raised` predicate
// closure passed to cliffTileAt / cliffCellBlocks.
func raisedSetFromElevation(cells []protocol.GridCoord) map[[2]int]bool {
	set := make(map[[2]int]bool, len(cells))
	for _, c := range cells {
		set[[2]int{c.X, c.Y}] = true
	}
	return set
}

// raisedPredicate returns a `raised(x, y) bool` closure backed by a set
// built with raisedSetFromElevation.
func raisedPredicate(set map[[2]int]bool) func(x, y int) bool {
	return func(x, y int) bool {
		return set[[2]int{x, y}]
	}
}

// rampSetFromRamps builds a lookup set from a map's authored Ramps cells,
// for use as the backing store of an `isRamp` predicate closure passed to
// cliffTileAt / cliffCellBlocks. A ramp cell only has an effect where the
// same cell is also present in the raised set — see cliffTileAt.
func rampSetFromRamps(cells []protocol.GridCoord) map[[2]int]bool {
	set := make(map[[2]int]bool, len(cells))
	for _, c := range cells {
		set[[2]int{c.X, c.Y}] = true
	}
	return set
}

// rampPredicate returns an `isRamp(x, y) bool` closure backed by a set built
// with rampSetFromRamps. Shares the raisedPredicate closure style.
func rampPredicate(set map[[2]int]bool) func(x, y int) bool {
	return func(x, y int) bool {
		return set[[2]int{x, y}]
	}
}
