package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestSpawnBatchPath_EquivalentToPerUnit guards the spawn-loop batching
// refactor. Its correctness rests on one claim: for a set of units that all
// share an OrderID, the per-plane sub-cell blocked map built once via
// buildGroupSubBlockedLocked is identical to the map each unit's own
// assignUnitPath would have built (buildUnitPathBlockedLocked excludes self +
// same-OrderID peers, so the excluded set is the same for all). If that holds,
// batching changes cost only — not the resulting paths.
//
// This paths the same units both ways and asserts identical Moving / Target /
// full Path. If a future change breaks the equivalence (e.g. wrong plane map,
// OrderID not shared), this fails.
func TestSpawnBatchPath_EquivalentToPerUnit(t *testing.T) {
	s := newObjectiveTestState(t)
	defer s.mu.Unlock()

	orderID := s.nextMovementOrderIDLocked()
	target := protocol.Vec2{X: 192, Y: 704} // townhall-1 center (reachable, obstacle-free map)

	var units []*Unit
	for i := 0; i < 5; i++ {
		u := s.spawnPlayerUnitLocked("soldier", enemyPlayerID, "#e74c3c",
			protocol.Vec2{X: 1600 + float64(i)*24, Y: 700})
		u.Visible = true
		u.MaxHP, u.HP = 800, 800
		u.MoveSpeed = 150
		u.OrderID = orderID // every spawned unit shares the spawn's orderID
		s.initializeCombatUnitLocked(u)
		units = append(units, u)
	}

	blocked := s.getBlockedCellsLocked()

	// Per-unit pathing (the old behavior): each call builds its own blocked map.
	type result struct {
		moving         bool
		tx, ty         float64
		path           []protocol.Vec2
	}
	perUnit := make([]result, len(units))
	for i, u := range units {
		s.assignUnitPath(u, target, blocked, nil)
		perUnit[i] = result{u.Moving, u.TargetX, u.TargetY, append([]protocol.Vec2(nil), u.Path...)}
	}

	// Reset movement outputs (positions are untouched by pathing, so the
	// blocked map is identical on the second pass).
	for _, u := range units {
		u.Path = nil
		u.Moving = false
		u.TargetX, u.TargetY = u.X, u.Y
	}

	// Batched pathing (the new behavior): one shared map for the group.
	groundSub, flyerSub := s.buildGroupSubBlockedLocked(units, blocked)
	subFor := func(u *Unit) map[gridPoint]bool {
		if u != nil && u.Flyer {
			return flyerSub
		}
		return groundSub
	}
	for _, u := range units {
		s.assignUnitPathWithSubBlocked(u, target, blocked, subFor(u), nil)
	}

	for i, u := range units {
		want := perUnit[i]
		if u.Moving != want.moving || u.TargetX != want.tx || u.TargetY != want.ty {
			t.Fatalf("unit %d: batched result diverged: Moving=%v Target=(%.2f,%.2f) want Moving=%v Target=(%.2f,%.2f)",
				i, u.Moving, u.TargetX, u.TargetY, want.moving, want.tx, want.ty)
		}
		if len(u.Path) != len(want.path) {
			t.Fatalf("unit %d: batched path length %d != per-unit %d", i, len(u.Path), len(want.path))
		}
		for j := range u.Path {
			if u.Path[j] != want.path[j] {
				t.Fatalf("unit %d waypoint %d: batched %v != per-unit %v", i, j, u.Path[j], want.path[j])
			}
		}
	}

	// Sanity: this fixture must actually exercise real paths, not the
	// degenerate "no route" case (which would make the equivalence vacuous).
	if !units[0].Moving || len(units[0].Path) == 0 {
		t.Fatal("fixture invalid: units must have a real reachable path to the townhall")
	}
}
