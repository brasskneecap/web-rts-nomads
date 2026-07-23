package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

func rectElevation(x0, x1, y0, y1 int) []protocol.GridCoord {
	var cells []protocol.GridCoord
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			cells = append(cells, protocol.GridCoord{X: x, Y: y})
		}
	}
	return cells
}

// TestCliffWang covers the Wang cliff auto-tiler over a 4x4 raised plateau
// (cells x in [2,5], y in [2,5]). Mirrors the client's cliffAutotile tests.
func TestCliffWang(t *testing.T) {
	raised := raisedPredicate(raisedSetFromElevation(rectElevation(2, 5, 2, 5)))
	noRamp := rampPredicate(rampSetFromRamps(nil))

	// Interior cell: all four corners inside → mask 15 → flat tile (2,1), walkable.
	if m := cliffWangMask(raised, 3, 3); m != 15 {
		t.Fatalf("interior mask = %d, want 15", m)
	}
	if col, row, ok := cliffTileAt(raised, noRamp, 3, 3); !ok || col != 2 || row != 1 {
		t.Fatalf("interior tile = (%d,%d,%v), want (2,1,true)", col, row, ok)
	}
	if cliffCellBlocks(raised, noRamp, 3, 3) {
		t.Fatal("interior plateau top should not block")
	}

	// Fully outside: mask 0 → no cliff tile, walkable ground.
	if m := cliffWangMask(raised, 0, 0); m != 0 {
		t.Fatalf("outside mask = %d, want 0", m)
	}
	if _, _, ok := cliffTileAt(raised, noRamp, 0, 0); ok {
		t.Fatal("outside cell should have no cliff tile")
	}
	if cliffCellBlocks(raised, noRamp, 0, 0) {
		t.Fatal("outside cell should not block")
	}

	// Transition cell just above the plateau's top edge: mask not 0/15 → blocks.
	if m := cliffWangMask(raised, 3, 1); m == 0 || m == 15 {
		t.Fatalf("transition mask = %d, want not 0 or 15", m)
	}
	if !cliffCellBlocks(raised, noRamp, 3, 1) {
		t.Fatal("cliff transition should block movement")
	}

	// A ramp on that transition cell renders the flat tile (2,1) and is walkable.
	rampAt := rampPredicate(rampSetFromRamps([]protocol.GridCoord{{X: 3, Y: 1}}))
	if col, row, ok := cliffTileAt(raised, rampAt, 3, 1); !ok || col != 2 || row != 1 {
		t.Fatalf("ramp tile = (%d,%d,%v), want (2,1,true)", col, row, ok)
	}
	if cliffCellBlocks(raised, rampAt, 3, 1) {
		t.Fatal("ramp cell should not block")
	}
}
