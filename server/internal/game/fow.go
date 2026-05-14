package game

import "webrts/server/pkg/protocol"

// CellState encodes the FOW state of a single grid cell. Two bits are used:
//   - bit 0 (EverSeen): set the first time the cell enters a unit's vision cone;
//     never cleared for the lifetime of the match.
//   - bit 1 (Clear): set during recomputeFOWLocked when a live vision source covers
//     the cell; cleared at the start of every recompute pass.
type CellState uint8

const (
	CellDark   CellState = 0x00 // never seen
	CellShroud CellState = 0x01 // ever-seen but not currently visible
	CellClear  CellState = 0x03 // ever-seen and currently in vision
)

// PlayerFOW holds the fog-of-war grid and the ghost-building cache for a single
// player. One instance lives on GameState.FOW per real player ID.
type PlayerFOW struct {
	Cols           int
	Rows           int
	Cells          []uint8
	// KnownBuildings is the last-seen snapshot of each building the player has
	// observed. Keyed by building ID. Each value is a deep-copied
	// *protocol.BuildingTile captured when the building's footprint was in a
	// Clear cell; it is NOT a live pointer into MapConfig.Buildings.
	KnownBuildings map[string]*protocol.BuildingTile
}

func newPlayerFOW(cols, rows int) *PlayerFOW {
	return &PlayerFOW{
		Cols:           cols,
		Rows:           rows,
		Cells:          make([]uint8, cols*rows),
		KnownBuildings: make(map[string]*protocol.BuildingTile),
	}
}

// stampCircle marks every cell within radiusPx of (worldX, worldY) as
// EverSeen|Clear. When blocking is non-nil, a Bresenham line-of-sight check
// gates each candidate cell: cells with no clear ray from the unit's cell are
// left unchanged. The unit's own cell is always stamped. Blocking cells
// themselves are stamped as visible (you see the tree; you just can't see past
// it) but occlude the cells behind them.
func (f *PlayerFOW) stampCircle(worldX, worldY, radiusPx, cellSizePx float64, blocking map[gridPoint]bool) {
	if cellSizePx <= 0 {
		return
	}
	cx := worldX / cellSizePx
	cy := worldY / cellSizePx
	r := radiusPx / cellSizePx

	unitCX := int(cx)
	unitCY := int(cy)

	minGX := int(cx-r) - 1
	maxGX := int(cx+r) + 1
	minGY := int(cy-r) - 1
	maxGY := int(cy+r) + 1

	if minGX < 0 {
		minGX = 0
	}
	if maxGX >= f.Cols {
		maxGX = f.Cols - 1
	}
	if minGY < 0 {
		minGY = 0
	}
	if maxGY >= f.Rows {
		maxGY = f.Rows - 1
	}

	rSq := r * r
	for gy := minGY; gy <= maxGY; gy++ {
		dy := float64(gy) + 0.5 - cy
		for gx := minGX; gx <= maxGX; gx++ {
			dx := float64(gx) + 0.5 - cx
			if dx*dx+dy*dy > rSq {
				continue
			}
			if blocking != nil && !hasLOS(unitCX, unitCY, gx, gy, blocking) {
				continue
			}
			f.Cells[gy*f.Cols+gx] = uint8(CellClear)
		}
	}
}

// hasLOS returns true if there is an unobstructed line of sight between grid
// cells (x0,y0) and (x1,y1) using Bresenham's line algorithm. Intermediate
// cells (exclusive of source and target) are checked against blocking. The
// target cell is never treated as a blocker so that a unit can always see the
// surface of an obstacle even if it cannot see past it.
func hasLOS(x0, y0, x1, y1 int, blocking map[gridPoint]bool) bool {
	dx := x1 - x0
	dy := y1 - y0
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	sx := 1
	if x0 > x1 {
		sx = -1
	}
	sy := 1
	if y0 > y1 {
		sy = -1
	}

	err := dx - dy
	x, y := x0, y0
	for {
		if x == x1 && y == y1 {
			return true
		}
		// Intermediate cells (not source) are checked for blocking.
		if x != x0 || y != y0 {
			if blocking[gridPoint{x, y}] {
				return false
			}
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
	}
}

// clearClearBits strips the Clear bit from every cell, leaving EverSeen intact.
// Called at the start of each FOW recompute before re-stamping live vision.
func (f *PlayerFOW) clearClearBits() {
	for i := range f.Cells {
		f.Cells[i] &= 0x01
	}
}

func (f *PlayerFOW) cellAt(gx, gy int) CellState {
	if gx < 0 || gx >= f.Cols || gy < 0 || gy >= f.Rows {
		return CellDark
	}
	return CellState(f.Cells[gy*f.Cols+gx])
}

func (f *PlayerFOW) isClearAtWorld(worldX, worldY, cellSizePx float64) bool {
	if cellSizePx <= 0 {
		return false
	}
	gx := int(worldX / cellSizePx)
	gy := int(worldY / cellSizePx)
	return f.cellAt(gx, gy) == CellClear
}

func (f *PlayerFOW) isEverSeenAtWorld(worldX, worldY, cellSizePx float64) bool {
	if cellSizePx <= 0 {
		return false
	}
	gx := int(worldX / cellSizePx)
	gy := int(worldY / cellSizePx)
	return f.cellAt(gx, gy) != CellDark
}

// buildingFootprintTouchesCell reports whether the building's grid footprint
// includes the cell (gx, gy).
func (f *PlayerFOW) buildingFootprintTouchesCell(b *protocol.BuildingTile, gx, gy int) bool {
	bx := b.GridCoord.X
	by := b.GridCoord.Y
	return gx >= bx && gx < bx+b.Width && gy >= by && gy < by+b.Height
}

// anyFootprintClear returns true if at least one cell in the building's grid
// footprint has the Clear bit set.
func (f *PlayerFOW) anyFootprintClear(b *protocol.BuildingTile) bool {
	bx := b.GridCoord.X
	by := b.GridCoord.Y
	for gy := by; gy < by+b.Height; gy++ {
		for gx := bx; gx < bx+b.Width; gx++ {
			if f.cellAt(gx, gy) == CellClear {
				return true
			}
		}
	}
	return false
}

// encodeRLE encodes the Cells slice as [state, count, state, count, ...]
// pairs. State values are 0, 1, or 3.
func (f *PlayerFOW) encodeRLE() []int {
	if len(f.Cells) == 0 {
		return nil
	}
	// Pre-allocate a reasonable estimate (2 ints per run; most maps have
	// far fewer distinct runs than total cells).
	out := make([]int, 0, 32)
	cur := int(f.Cells[0])
	count := 1
	for i := 1; i < len(f.Cells); i++ {
		v := int(f.Cells[i])
		if v == cur {
			count++
		} else {
			out = append(out, cur, count)
			cur = v
			count = 1
		}
	}
	out = append(out, cur, count)
	return out
}
