package game

import "testing"

// fowCellClear reports whether the (gx,gy) cell has the Clear bit (0x02) set
// in the given player's FOW. Caller holds s.mu.
func fowCellClear(f *PlayerFOW, gx, gy int) bool {
	if f == nil || gx < 0 || gy < 0 || gx >= f.Cols || gy >= f.Rows {
		return false
	}
	return f.Cells[gy*f.Cols+gx]&0x02 != 0
}

// Same-team players share vision; cross-team do not; the __enemy__ AI never
// shares sight with players. Built from hand-constructed units + FOW entries
// so it is fully deterministic and independent of map structure.
func TestTeam_FOWSharedWithinTeam(t *testing.T) {
	// scenario builds a state with p1 (team 0), p2 (team p2Team), and an
	// __enemy__ unit, then recomputes FOW. Returns whether p1's FOW has the
	// p2-unit cell and the enemy-unit cell Clear, plus p1's own-cell sanity.
	scenario := func(t *testing.T, p2Team int) (seesP2, seesEnemy, seesOwn bool) {
		t.Helper()
		s := newProjectileTestState(t)
		s.mu.Lock()
		defer s.mu.Unlock()

		cell := s.MapConfig.CellSize
		cols, rows := s.MapConfig.GridCols, s.MapConfig.GridRows
		y := cell * float64(rows/2)
		p1x := cell * float64(cols/4)
		p2x := cell * float64(cols*3/4)
		ex := cell * float64(cols/2)
		vision := cell * 1.5 // ~1 cell — far smaller than the gaps between units

		// Flyer=true so stampCircle ignores terrain LoS (pure circle) ⇒
		// deterministic regardless of the default map's obstacles.
		mkU := func(id int, owner string, x float64) *Unit {
			return &Unit{ID: id, OwnerID: owner, X: x, Y: y, HP: 100, Visible: true, VisionRange: vision, Flyer: true}
		}
		s.Units = append(s.Units, mkU(1, "p1", p1x), mkU(2, "p2", p2x), mkU(3, enemyPlayerID, ex))

		s.Players["p1"] = &Player{ID: "p1", TeamID: 0}
		s.Players["p2"] = &Player{ID: "p2", TeamID: p2Team}
		// __enemy__ deliberately gets NO s.FOW entry (matches production).
		s.FOW = map[string]*PlayerFOW{
			"p1": newPlayerFOW(cols, rows),
			"p2": newPlayerFOW(cols, rows),
		}

		s.recomputeFOWLocked()

		f := s.FOW["p1"]
		gc := func(x float64) (int, int) { return int(x / cell), int(y / cell) }
		gx2, gy2 := gc(p2x)
		gxE, gyE := gc(ex)
		gx1, gy1 := gc(p1x)
		return fowCellClear(f, gx2, gy2), fowCellClear(f, gxE, gyE), fowCellClear(f, gx1, gy1)
	}

	t.Run("same team shares vision", func(t *testing.T) {
		seesP2, seesEnemy, seesOwn := scenario(t, 0)
		if !seesOwn {
			t.Fatal("sanity: p1 must see its own unit's cell")
		}
		if !seesP2 {
			t.Error("same-team ally's vision should be shared into p1's FOW")
		}
		if seesEnemy {
			t.Error("the __enemy__ unit's area must NOT be visible to p1 (enemy never shares sight)")
		}
	})

	t.Run("different team does not share vision", func(t *testing.T) {
		seesP2, _, seesOwn := scenario(t, 1) // p2 on a different team
		if !seesOwn {
			t.Fatal("sanity: p1 must still see its own unit's cell")
		}
		if seesP2 {
			t.Error("cross-team player's vision must NOT be shared into p1's FOW")
		}
	})
}
