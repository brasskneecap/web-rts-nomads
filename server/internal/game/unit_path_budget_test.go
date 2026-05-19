package game

import (
	"testing"
)

// injectPathTracker attaches a live debugPathTracker to s so tests can read the
// fine-path / budget counters (the production one is nil unless
// WEBRTS_DEBUG_PATHING is set at package-init time).
func injectPathTracker(s *GameState) *debugPathTracker {
	tr := &debugPathTracker{
		unitStats:    make(map[int]*unitPathDebugStats),
		callerCounts: make(map[string]int),
	}
	s.debugPathTracker = tr
	return tr
}

// TestUnitPath_BudgetAbortsExhaustiveSearch is the core fix: an unreachable
// goal must NOT cause findUnitPath to explore the entire ~15k-cell sub-grid
// (the ~70ms single-tick freeze). The node-expansion budget must abort the
// search early and report "no route", recording one budget hit.
//
// The goal is fully enclosed by a one-cell blocked ring with the rest of the
// grid left open, so an unbounded A* from the far corner would expand almost
// every cell before failing — far past any sane budget.
func TestUnitPath_BudgetAbortsExhaustiveSearch(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	tr := injectPathTracker(s)

	cols, rows := s.unitPathSubGridDims()
	goal := gridPoint{X: cols / 2, Y: rows / 2}

	blocked := make(map[gridPoint]bool)
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue // goal cell itself stays walkable but isolated
			}
			blocked[gridPoint{X: goal.X + dx, Y: goal.Y + dy}] = true
		}
	}

	start := gridPoint{X: 2, Y: 2}
	result := s.findUnitPath(start, goal, blocked)

	if len(result) != 0 {
		t.Fatalf("expected no route to a fully-enclosed goal; got path len=%d", len(result))
	}
	if tr.unitPathBudgetHits != 1 {
		t.Fatalf("exhaustive search must be aborted by the node budget exactly "+
			"once; unitPathBudgetHits=%d (an unbounded search here explores the "+
			"whole sub-grid — the ~70ms freeze)", tr.unitPathBudgetHits)
	}
}

// TestUnitPath_ReachableShortPathUnaffected guards correctness: a normal
// reachable route on an open grid must still succeed and must NOT trip the
// budget (it expands far fewer nodes than the cap).
func TestUnitPath_ReachableShortPathUnaffected(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	tr := injectPathTracker(s)

	start := gridPoint{X: 2, Y: 2}
	goal := gridPoint{X: 14, Y: 11}
	result := s.findUnitPath(start, goal, map[gridPoint]bool{})

	if len(result) == 0 {
		t.Fatal("reachable open-grid path must still be found after adding the budget")
	}
	if tr.unitPathBudgetHits != 0 {
		t.Fatalf("a short reachable path must not trip the node budget; hits=%d", tr.unitPathBudgetHits)
	}
}

// TestUnitPath_ReachableDetourStillWorks guards the reachability side of the
// budget tradeoff for NORMAL play: routing around a building-sized obstacle
// (a short, localized blocker — not a near-full map partition) must still
// succeed well under the cap. A near-full partition with a single far gap is
// deliberately NOT asserted here: that is the pathological case the budget
// exists to bound, and aborting it is the intended enemy-blocked-objective
// behavior (fall back to engaging the blockers). If a future budget retune
// makes THIS modest detour fail, the cap is too tight for normal play.
func TestUnitPath_ReachableDetourStillWorks(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()
	tr := injectPathTracker(s)

	cols, rows := s.unitPathSubGridDims()
	midX, midY := cols/2, rows/2
	blocked := make(map[gridPoint]bool)
	// A compact ~16-cell vertical obstacle (building-sized), open all around —
	// the unit just bends around it. Expands a few hundred nodes, far below
	// the budget.
	for y := midY - 8; y <= midY+8; y++ {
		blocked[gridPoint{X: midX, Y: y}] = true
	}

	start := gridPoint{X: midX - 10, Y: midY}
	goal := gridPoint{X: midX + 10, Y: midY}
	result := s.findUnitPath(start, goal, blocked)

	if len(result) == 0 {
		t.Fatal("routing around a building-sized obstacle must succeed — budget too tight for normal play")
	}
	if tr.unitPathBudgetHits != 0 {
		t.Fatalf("a normal around-a-building detour must not trip the node budget; hits=%d "+
			"(budget too tight)", tr.unitPathBudgetHits)
	}
}
