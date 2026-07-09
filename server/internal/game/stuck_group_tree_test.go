package game

import (
	"testing"

	"webrts/server/pkg/protocol"
)

// TestGroupMove_NoStormNearTrees guards the "units stuck walking into trees"
// stall for GROUP moves whose formation slots pack against a tree cluster —
// the production case (two units pinned at one point, moved=0, repath storm,
// staticAdj=true). Before the movement-loop wall-slide fix, units whose
// straight approach to their slot clipped a tree repathed to the same clipping
// path every tick and never moved.
//
// Assertion: after a group move fully settles, no unit is left storming (still
// Moving with a runaway repath count). Sweeps several cluster shapes and
// approach directions that were observed to storm.
func TestGroupMove_NoStormNearTrees(t *testing.T) {
	clusters := map[string][]protocol.GridCoord{
		"single":  {{X: 20, Y: 14}},
		"h-wall3": {{X: 20, Y: 14}, {X: 21, Y: 14}, {X: 22, Y: 14}},
		"v-wall3": {{X: 20, Y: 13}, {X: 20, Y: 14}, {X: 20, Y: 15}},
		"diag":    {{X: 20, Y: 14}, {X: 21, Y: 15}},
		"2x2":     {{X: 20, Y: 14}, {X: 21, Y: 14}, {X: 20, Y: 15}, {X: 21, Y: 15}},
	}
	approaches := map[string]protocol.Vec2{
		"fromW": {X: 1000, Y: 928},
		"fromE": {X: 1600, Y: 928},
		"fromN": {X: 1312, Y: 640},
		"fromS": {X: 1312, Y: 1200},
	}
	dests := []protocol.Vec2{
		{X: 1312, Y: 928}, // centre of the cluster
		{X: 1290, Y: 928}, // west edge
		{X: 1338, Y: 928}, // east edge
		{X: 1312, Y: 905}, // north edge
	}

	for cname, cells := range clusters {
		for aname, base := range approaches {
			for _, d := range dests {
				storming := groupMoveStormingUnits(t, cells, base, d, 6)
				if len(storming) > 0 {
					t.Errorf("cluster=%s approach=%s dest=(%.0f,%.0f): %d unit(s) still storming after settle: %v",
						cname, aname, d.X, d.Y, len(storming), storming)
				}
			}
		}
	}
}

// groupMoveStormingUnits runs a group move and returns a description of any unit
// that is still Moving with a runaway repath count after the move should have
// settled (i.e. repath-storming in place).
func groupMoveStormingUnits(t *testing.T, treeCells []protocol.GridCoord, base, dest protocol.Vec2, n int) []string {
	t.Helper()
	s := newStuckRecoveryTestState(t)

	s.mu.Lock()
	for _, c := range treeCells {
		s.MapConfig.Obstacles = append(s.MapConfig.Obstacles, protocol.ObstacleTile{
			GridCoord: c, Obstacle: "tree",
		})
	}
	s.blockedCellsValid = false

	ids := make([]int, 0, n)
	for i := 0; i < n; i++ {
		px := base.X + float64(i%3)*24
		py := base.Y + float64(i/3)*24
		u := s.spawnPlayerUnitLocked("soldier", "p1", "#3498db", protocol.Vec2{X: px, Y: py})
		u.Visible = true
		u.HP = u.MaxHP
		s.initializeCombatUnitLocked(u)
		ids = append(ids, u.ID)
	}
	s.mu.Unlock()

	s.MoveUnits("p1", ids, dest)

	for i := 0; i < 400; i++ { // 20s — a group move must have settled by now
		s.Update(0.05)
	}

	var storming []string
	s.mu.Lock()
	for _, id := range ids {
		u := s.unitsByID[id]
		// A settled unit is either stopped (Moving=false) or genuinely still
		// travelling with a sane repath count. A high repath count while still
		// Moving is the storm.
		if u.Moving && u.PathDiagnostics.RepathCount >= 50 {
			storming = append(storming, formatStormUnit(u))
		}
	}
	s.mu.Unlock()
	return storming
}

func formatStormUnit(u *Unit) string {
	return "unit at (" + f0(u.X) + "," + f0(u.Y) + ")"
}

func f0(v float64) string {
	n := int(v)
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
