package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestStuckUnit_ReachesPastTreeBufferEmbeddedStart reproduces the production
// "unit stuck walking into a tree" stall and asserts the unit escapes it.
//
// Real capture: unit=44 order=Move moved=0px pathLen=2 pos=(2178,1397)
// allyCrowd=0 staticAdj=true — a lone move-ordered unit pinned next to a tree,
// repathing every tick with zero net movement.
//
// Root cause (see assignUnitPathWithSubBlocked): when the unit stands inside a
// tree's sub-cell buffer, the sub-A* start is relocated off the unit's real
// position, and the leading-waypoint cull then strips the obstacle-avoidance
// waypoints (it assumes the path starts at the unit). What remains is a
// straight shot whose first step clips the tree's coarse cell, so the movement
// loop's walkability check rejects it and repaths — forever.
//
// Each case: a lone unit standing in a tree's buffer, move-ordered to a
// reachable point past the tree. It must arrive, not storm in place.
func TestStuckUnit_ReachesPastTreeBufferEmbeddedStart(t *testing.T) {
	cases := []struct {
		name  string
		trees []protocol.GridCoord
		start protocol.Vec2
		dest  protocol.Vec2
	}{
		{
			name:  "diagonal tree gap",
			trees: []protocol.GridCoord{{X: 10, Y: 12}, {X: 11, Y: 13}},
			start: protocol.Vec2{X: 639, Y: 800},
			dest:  protocol.Vec2{X: 706, Y: 800},
		},
		{
			name:  "single tree, buffer-embedded start",
			trees: []protocol.GridCoord{{X: 10, Y: 12}},
			start: protocol.Vec2{X: 639, Y: 800},
			dest:  protocol.Vec2{X: 714, Y: 800},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStuckRecoveryTestState(t)

			s.mu.Lock()
			for _, c := range tc.trees {
				s.MapConfig.Obstacles = append(s.MapConfig.Obstacles, protocol.ObstacleTile{
					GridCoord: c, Obstacle: "tree",
				})
			}
			s.blockedCellsValid = false
			u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", tc.start)
			u.Visible = true
			u.HP = u.MaxHP
			s.initializeCombatUnitLocked(u)
			id := u.ID
			s.mu.Unlock()

			s.MoveUnits("p1", []int{id}, tc.dest)

			reached := false
			for i := 0; i < 400; i++ { // 20s of sim
				s.Update(0.05)
				s.mu.Lock()
				fu := s.unitsByID[id]
				dx, dy := fu.X-tc.dest.X, fu.Y-tc.dest.Y
				s.mu.Unlock()
				if dx*dx+dy*dy <= 40*40 {
					reached = true
					break
				}
			}

			if !reached {
				s.mu.Lock()
				fu := s.unitsByID[id]
				x, y, moving, pathLen, repaths := fu.X, fu.Y, fu.Moving, len(fu.Path), fu.PathDiagnostics.RepathCount
				s.mu.Unlock()
				t.Fatalf("unit never reached dest past tree: pos=(%.0f,%.0f) moving=%v pathLen=%d repaths=%d (wanted within 40px of (%.0f,%.0f))",
					x, y, moving, pathLen, repaths, tc.dest.X, tc.dest.Y)
			}
		})
	}
}

// TestStuckUnit_EscapesWhenEmbeddedInObstacle covers the follow-up failure mode:
// a unit standing fully INSIDE a blocked cell (shoved in by knockback / pull /
// separation, or a building/tree dropped on it). Normal path advancement can't
// move it — every step's coarse walkability check fails because the unit's own
// cell is blocked — so it repath-storms in place. The sim must eject it toward
// the nearest walkable ground, after which it reaches its destination.
func TestStuckUnit_EscapesWhenEmbeddedInObstacle(t *testing.T) {
	cases := []struct {
		name  string
		trees []protocol.GridCoord
		start protocol.Vec2 // inside a tree cell
		dest  protocol.Vec2
	}{
		{
			name:  "centre of a single tree",
			trees: []protocol.GridCoord{{X: 10, Y: 12}},
			start: protocol.Vec2{X: 672, Y: 800}, // cell (10,12)
			dest:  protocol.Vec2{X: 900, Y: 800},
		},
		{
			name:  "inside U pocket wall",
			trees: []protocol.GridCoord{{X: 10, Y: 11}, {X: 10, Y: 12}, {X: 10, Y: 13}, {X: 11, Y: 11}, {X: 11, Y: 13}},
			start: protocol.Vec2{X: 672, Y: 758}, // cell (10,11)
			dest:  protocol.Vec2{X: 900, Y: 800},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStuckRecoveryTestState(t)

			s.mu.Lock()
			for _, c := range tc.trees {
				s.MapConfig.Obstacles = append(s.MapConfig.Obstacles, protocol.ObstacleTile{
					GridCoord: c, Obstacle: "tree",
				})
			}
			s.blockedCellsValid = false
			u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", tc.start)
			u.Visible = true
			u.HP = u.MaxHP
			s.initializeCombatUnitLocked(u)
			id := u.ID
			s.mu.Unlock()

			s.MoveUnits("p1", []int{id}, tc.dest)

			reached := false
			for i := 0; i < 400; i++ {
				s.Update(0.05)
				s.mu.Lock()
				fu := s.unitsByID[id]
				dx, dy := fu.X-tc.dest.X, fu.Y-tc.dest.Y
				s.mu.Unlock()
				if dx*dx+dy*dy <= 40*40 {
					reached = true
					break
				}
			}

			if !reached {
				s.mu.Lock()
				fu := s.unitsByID[id]
				cell := s.worldToGrid(fu.X, fu.Y)
				embedded := !s.isWalkable(cell, s.getBlockedCellsLocked())
				x, y, moving, pathLen := fu.X, fu.Y, fu.Moving, len(fu.Path)
				s.mu.Unlock()
				t.Fatalf("embedded unit never escaped/reached: pos=(%.0f,%.0f) cell=(%d,%d) stillInObstacle=%v moving=%v pathLen=%d (wanted within 40px of (%.0f,%.0f))",
					x, y, cell.X, cell.Y, embedded, moving, pathLen, tc.dest.X, tc.dest.Y)
			}
		})
	}
}
