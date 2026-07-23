package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// rectRaised returns a `raised` predicate true for every cell in the
// inclusive rectangle [x0,x1] x [y0,y1].
func rectRaised(x0, y0, x1, y1 int) func(x, y int) bool {
	return func(x, y int) bool {
		return x >= x0 && x <= x1 && y >= y0 && y <= y1
	}
}

func TestCliffTileAt_SolidRectangle_PicksExpectedSlots(t *testing.T) {
	// Solid 4x4 raised rectangle: x in [2,5], y in [2,5].
	raised := rectRaised(2, 2, 5, 5)

	tests := []struct {
		name    string
		x, y    int
		wantCol int
		wantRow int
	}{
		{"NW outer corner", 2, 2, cliffOuterNW.Col, cliffOuterNW.Row},
		{"NE outer corner", 5, 2, cliffOuterNE.Col, cliffOuterNE.Row},
		{"SW outer corner", 2, 5, cliffOuterSW.Col, cliffOuterSW.Row},
		{"SE outer corner", 5, 5, cliffOuterSE.Col, cliffOuterSE.Row},
		{"N wall (left)", 3, 2, cliffWallN.Col, cliffWallN.Row},
		{"N wall (right)", 4, 2, cliffWallN.Col, cliffWallN.Row},
		{"S wall (left)", 3, 5, cliffWallS.Col, cliffWallS.Row},
		{"S wall (right)", 4, 5, cliffWallS.Col, cliffWallS.Row},
		{"W wall (top)", 2, 3, cliffWallW.Col, cliffWallW.Row},
		{"W wall (bottom)", 2, 4, cliffWallW.Col, cliffWallW.Row},
		{"E wall (top)", 5, 3, cliffWallE.Col, cliffWallE.Row},
		{"E wall (bottom)", 5, 4, cliffWallE.Col, cliffWallE.Row},
		{"interior flat (3,3)", 3, 3, cliffFlat.Col, cliffFlat.Row},
		{"interior flat (4,3)", 4, 3, cliffFlat.Col, cliffFlat.Row},
		{"interior flat (3,4)", 3, 4, cliffFlat.Col, cliffFlat.Row},
		{"interior flat (4,4)", 4, 4, cliffFlat.Col, cliffFlat.Row},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, row, ok := cliffTileAt(raised, tt.x, tt.y)
			if !ok {
				t.Fatalf("cliffTileAt(%d,%d) ok=false, want true", tt.x, tt.y)
			}
			if col != tt.wantCol || row != tt.wantRow {
				t.Errorf("cliffTileAt(%d,%d) = (%d,%d), want (%d,%d)", tt.x, tt.y, col, row, tt.wantCol, tt.wantRow)
			}
		})
	}
}

func TestCliffCellBlocks_SolidRectangle_WallsAndCornersBlockInteriorDoesNot(t *testing.T) {
	raised := rectRaised(2, 2, 5, 5)

	blocking := []struct{ x, y int }{
		{2, 2}, {5, 2}, {2, 5}, {5, 5}, // outer corners
		{3, 2}, {4, 2}, {3, 5}, {4, 5}, // N/S walls
		{2, 3}, {2, 4}, {5, 3}, {5, 4}, // W/E walls
	}
	for _, c := range blocking {
		if !cliffCellBlocks(raised, c.x, c.y) {
			t.Errorf("cliffCellBlocks(%d,%d) = false, want true (wall/outer corner)", c.x, c.y)
		}
	}

	interior := []struct{ x, y int }{{3, 3}, {4, 3}, {3, 4}, {4, 4}}
	for _, c := range interior {
		if cliffCellBlocks(raised, c.x, c.y) {
			t.Errorf("cliffCellBlocks(%d,%d) = true, want false (flat interior)", c.x, c.y)
		}
	}
}

// TestCliffTileAt_LShape_ConcaveCorner_PicksInnerSlotAndDoesNotBlock builds an
// L-shape (a 4x4 rectangle minus its top-right 2x2 quadrant) and asserts the
// resulting concave corner cell picks the NE inner-corner slot and does not
// block movement.
func TestCliffTileAt_LShape_ConcaveCorner_PicksInnerSlotAndDoesNotBlock(t *testing.T) {
	full := rectRaised(2, 2, 5, 5)
	removedQuadrant := rectRaised(4, 2, 5, 3) // top-right 2x2 quadrant
	raised := func(x, y int) bool {
		return full(x, y) && !removedQuadrant(x, y)
	}

	// Sanity: the removed quadrant is indeed not raised, and the concave
	// cell itself is raised.
	if raised(4, 3) {
		t.Fatalf("setup error: (4,3) expected not raised (removed quadrant)")
	}
	if !raised(3, 4) {
		t.Fatalf("setup error: (3,4) expected raised (concave corner cell)")
	}

	col, row, ok := cliffTileAt(raised, 3, 4)
	if !ok {
		t.Fatalf("cliffTileAt(3,4) ok=false, want true")
	}
	if col != cliffInnerNE.Col || row != cliffInnerNE.Row {
		t.Errorf("cliffTileAt(3,4) = (%d,%d), want NEi (%d,%d)", col, row, cliffInnerNE.Col, cliffInnerNE.Row)
	}

	if cliffCellBlocks(raised, 3, 4) {
		t.Errorf("cliffCellBlocks(3,4) = true, want false (inner corner is walkable plateau top)")
	}
}

func TestCliffTileAt_NonRaisedCell_ReturnsNotOK(t *testing.T) {
	raised := rectRaised(2, 2, 5, 5)

	if _, _, ok := cliffTileAt(raised, 0, 0); ok {
		t.Errorf("cliffTileAt(0,0) ok=true, want false (not raised)")
	}
	if cliffCellBlocks(raised, 0, 0) {
		t.Errorf("cliffCellBlocks(0,0) = true, want false (not raised)")
	}
}

func TestRaisedSetFromElevation_PredicateMatchesCells(t *testing.T) {
	cells := []protocol.GridCoord{{X: 1, Y: 1}, {X: 2, Y: 1}}
	raised := raisedPredicate(raisedSetFromElevation(cells))

	if !raised(1, 1) || !raised(2, 1) {
		t.Errorf("expected authored cells to be raised")
	}
	if raised(0, 0) {
		t.Errorf("expected non-authored cell to not be raised")
	}
}
